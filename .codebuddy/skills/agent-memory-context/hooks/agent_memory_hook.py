#!/usr/bin/env python3
"""Agent-Memory hook bridge for CodeBuddy and Codex."""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
import html
import json
import os
from pathlib import Path
import platform
import re
import shlex
import shutil
import subprocess
import sys
from typing import Any
from urllib.error import URLError
from urllib.request import Request, urlopen

DEFAULT_LIMIT = 8
DEFAULT_MAX_CONTEXT_CHARS = 12000
DEFAULT_MEMORY_MAX_CHARS = 20000
DEFAULT_MEMORY_ITEMS_PER_TYPE = 12
DEFAULT_TIMEOUT_SECONDS = 120
DEFAULT_URL_IMPORT_MAX_BYTES = 2 * 1024 * 1024
MEMORY_SOURCE_TYPES = ("conversation", "experience", "preference", "tool_history", "fact")
KNOWLEDGE_TRIGGER_TERMS = (
    "导入知识库", "加入知识库", "放入知识库", "写入知识库", "收录到知识库", "当作知识库", "作为知识库",
    "import knowledge", "ingest knowledge", "add to knowledge base", "import into knowledge base",
)


def main() -> int:
    parser = argparse.ArgumentParser(description="Agent-Memory lifecycle hook bridge")
    parser.add_argument("mode", choices=["session-start", "prompt", "sync"], help="hook behavior to run")
    args = parser.parse_args()

    payload = read_payload()
    root = resolve_repo_root(payload)
    event_name = str(payload.get("hook_event_name") or event_name_from_mode(args.mode))

    try:
        if args.mode == "prompt":
            prompt = str(payload.get("prompt") or "").strip()
            if not prompt:
                return emit_json(event_name, additional_context="Agent-Memory hook skipped: empty prompt")
            ensure_indexed(root)
            remember_prompt(root, payload, prompt)
            import_summary = import_prompt_knowledge(root, payload, prompt)
            context = search_prompt_context(root, payload, prompt)
            if import_summary:
                context = import_summary + "\n\n" + context
            return emit_json(event_name, additional_context=context)
        if args.mode == "session-start":
            summary = ensure_indexed(root, sync_when_indexed=True)
            return emit_json(event_name, additional_context=f"Agent-Memory context loop active. {summary}")
        memory_summary = persist_session_memory(root, payload)
        summary = ensure_indexed(root, sync_when_indexed=True)
        additional_context = f"{memory_summary} {summary}".strip()
        return emit_json(event_name, additional_context=additional_context, suppress=True)
    except Exception as exc:  # pylint: disable=broad-exception-caught
        return emit_json(event_name, additional_context=f"Agent-Memory hook warning: {exc}")


def read_payload() -> dict[str, Any]:
    raw = sys.stdin.read().strip()
    if not raw:
        return {}
    try:
        value = json.loads(raw)
    except json.JSONDecodeError:
        return {"raw_input": raw}
    if isinstance(value, dict):
        return value
    return {"raw_input": raw}


def event_name_from_mode(mode: str) -> str:
    if mode == "prompt":
        return "UserPromptSubmit"
    if mode == "session-start":
        return "SessionStart"
    return "Stop"


def resolve_repo_root(payload: dict[str, Any]) -> Path:
    explicit_root = os.environ.get("AGENT_MEMORY_TARGET_REPO")
    if explicit_root:
        return Path(explicit_root).expanduser().resolve()

    cwd = (
        payload.get("cwd")
        or os.environ.get("CODEBUDDY_PROJECT_DIR")
        or os.environ.get("PWD")
        or os.getcwd()
    )
    start = Path(str(cwd)).expanduser().resolve()
    result = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        cwd=str(start),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        check=False,
    )
    if result.returncode == 0 and result.stdout.strip():
        return Path(result.stdout.strip()).resolve()
    return start


def code_context_prefix(root: Path) -> list[str]:
    configured = os.environ.get("AGENT_MEMORY_CODE_CONTEXT_BIN")
    if configured:
        return shlex.split(configured)

    exe = ".exe" if platform.system().lower() == "windows" else ""
    candidate_paths = [
        root / "bin" / f"code-context{exe}",
        default_install_root() / "bin" / f"code-context{exe}",
    ]
    for candidate in candidate_paths:
        if candidate.exists():
            return [str(candidate)]

    found = shutil.which(f"code-context{exe}") or shutil.which("code-context")
    if found:
        return [found]

    if (root / "cmd" / "code-context").is_dir() and (root / "go.mod").exists():
        return ["go", "run", "./cmd/code-context"]

    return [f"code-context{exe}"]


def default_install_root() -> Path:
    if platform.system().lower() == "windows":
        local_app_data = os.environ.get("LOCALAPPDATA")
        if local_app_data:
            return Path(local_app_data) / "AgentMemory"
    return Path.home() / ".agent-memory"


def run_code_context(root: Path, args: list[str], timeout: int | None = None) -> subprocess.CompletedProcess[str]:
    command = code_context_prefix(root) + args
    return subprocess.run(
        command,
        cwd=str(root),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=timeout or hook_timeout_seconds(),
        check=False,
    )


def hook_timeout_seconds() -> int:
    raw = os.environ.get("AGENT_MEMORY_HOOK_TIMEOUT_SEC", "")
    if raw.isdigit() and int(raw) > 0:
        return int(raw)
    return DEFAULT_TIMEOUT_SECONDS


def ensure_indexed(root: Path, sync_when_indexed: bool = False) -> str:
    status = run_code_context(root, ["status", str(root)])
    status_text = (status.stdout + status.stderr).strip()
    if status.returncode != 0:
        raise RuntimeError(compact_text(status_text) or "status command failed")

    if "status=not_indexed" in status.stdout:
        indexed = run_code_context(root, ["index", str(root)])
        if indexed.returncode != 0:
            raise RuntimeError(compact_text(indexed.stderr or indexed.stdout) or "index command failed")
        return compact_text(indexed.stdout)

    if sync_when_indexed:
        synced = run_code_context(root, ["sync", str(root)])
        if synced.returncode != 0:
            raise RuntimeError(compact_text(synced.stderr or synced.stdout) or "sync command failed")
        return compact_text(synced.stdout)

    return compact_text(status.stdout)


def remember_prompt(root: Path, payload: dict[str, Any], prompt: str) -> None:
    state = load_state(root, payload)
    messages = list(state.get("session_messages") or [])
    message = {
        "role": "user",
        "content": redact_sensitive(prompt),
        "created_at": utc_now(),
    }
    if not messages or messages[-1].get("content") != message["content"]:
        messages.append(message)
    state["session_messages"] = messages[-60:]
    save_state(root, payload, state)


def persist_session_memory(root: Path, payload: dict[str, Any]) -> str:
    messages = extract_session_messages(root, payload)
    if not messages:
        return "Agent-Memory session memory skipped: no session history."

    records_by_type = build_session_memory_records(root, payload, messages)
    imported: list[str] = []
    for source_type in MEMORY_SOURCE_TYPES:
        records = records_by_type.get(source_type, [])
        if not records:
            continue
        path = write_memory_import_file(root, payload, source_type, records)
        source_id = session_source_id(root, payload, source_type)
        result = run_code_context(root, ["import", "memory", source_type, str(path), source_id])
        if result.returncode != 0:
            message = compact_text(result.stderr or result.stdout) or f"import {source_type} failed"
            raise RuntimeError(message)
        imported.append(f"{source_type}={len(records)}")
    if not imported:
        return "Agent-Memory session memory skipped: no extractable records."
    return "Agent-Memory session memory imported: " + ", ".join(imported) + "."


def build_session_memory_records(
    root: Path,
    payload: dict[str, Any],
    messages: list[dict[str, str]],
) -> dict[str, list[dict[str, Any]]]:
    session_key = session_state_key(root, payload)
    records: dict[str, list[dict[str, Any]]] = {source_type: [] for source_type in MEMORY_SOURCE_TYPES}
    conversation_text = session_history_text(messages)
    if conversation_text:
        records["conversation"].append(memory_record("conversation", session_key, conversation_text, 0, role="conversation"))

    records["preference"] = extracted_sentence_records(
        "preference",
        session_key,
        messages,
        roles={"user"},
        terms=(
            "用户要求", "用户偏好", "偏好", "以后", "每次", "必须", "不要", "默认", "需要", "应该",
            "prefer", "preference", "always", "never", "must", "should",
        ),
    )
    records["experience"] = extracted_sentence_records(
        "experience",
        session_key,
        messages,
        roles=None,
        terms=(
            "经验", "教训", "解决", "修复", "完成", "实现", "验证通过", "原因", "根因", "方案", "排查",
            "lesson", "fixed", "implemented", "verified", "resolved",
        ),
    )
    records["fact"] = extracted_sentence_records(
        "fact",
        session_key,
        messages,
        roles=None,
        terms=(
            "当前项目", "仓库", "路径", "工作目录", "已实现", "已提交", "commit", "环境", "端口", "分支",
            "module", "repository", "workspace", "branch",
        ),
    )
    records["tool_history"] = tool_history_records(session_key, messages, payload)
    return records


def memory_record(source_type: str, session_key: str, content: str, index: int, role: str = "") -> dict[str, Any]:
    clean_content = compact_memory_text(redact_sensitive(content), memory_max_chars())
    digest = hashlib.sha256(f"{session_key}\0{source_type}\0{index}\0{clean_content}".encode("utf-8")).hexdigest()[:16]
    return {
        "id": f"{source_type}-{digest}",
        "content": clean_content,
        "summary": compact_text(clean_content, 240),
        "role": role,
        "conversation_id": session_key,
        "tags": ["auto_extracted", source_type, "agent_memory_hook"],
        "metadata": {
            "extractor": "agent_memory_hook",
            "session_key": session_key,
        },
        "created_at": utc_now(),
        "updated_at": utc_now(),
    }


def extracted_sentence_records(
    source_type: str,
    session_key: str,
    messages: list[dict[str, str]],
    roles: set[str] | None,
    terms: tuple[str, ...],
) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    seen: set[str] = set()
    max_items = positive_int_env("AGENT_MEMORY_HOOK_MAX_MEMORY_ITEMS", DEFAULT_MEMORY_ITEMS_PER_TYPE)
    for message in messages:
        role = normalize_role(message.get("role", ""))
        if roles is not None and role not in roles:
            continue
        for sentence in split_memory_sentences(message.get("content", "")):
            if not contains_any(sentence.lower(), terms):
                continue
            key = hashlib.sha256(sentence.encode("utf-8")).hexdigest()
            if key in seen:
                continue
            seen.add(key)
            content = f"{role}: {sentence}" if role else sentence
            records.append(memory_record(source_type, session_key, content, len(records), role=role))
            if len(records) >= max_items:
                return records
    return records


def tool_history_records(
    session_key: str,
    messages: list[dict[str, str]],
    payload: dict[str, Any],
) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    seen: set[str] = set()
    entries = []
    for message in messages:
        tool_name = str(message.get("tool_name") or "").strip()
        if tool_name:
            entries.append({"tool_name": tool_name, "content": message.get("content", "")})
    entries.extend(collect_payload_tool_entries(payload))

    max_items = positive_int_env("AGENT_MEMORY_HOOK_MAX_MEMORY_ITEMS", DEFAULT_MEMORY_ITEMS_PER_TYPE)
    for entry in entries:
        tool_name = str(entry.get("tool_name") or "tool").strip() or "tool"
        content = compact_memory_text(str(entry.get("content") or ""), 3000)
        text = f"Tool `{tool_name}` was used during the session.\n{content}".strip()
        key = hashlib.sha256(text.encode("utf-8")).hexdigest()
        if key in seen:
            continue
        seen.add(key)
        record = memory_record("tool_history", session_key, text, len(records), role="tool")
        record["tool_name"] = tool_name
        records.append(record)
        if len(records) >= max_items:
            return records
    return records


def extract_session_messages(root: Path, payload: dict[str, Any]) -> list[dict[str, str]]:
    messages: list[dict[str, str]] = []
    for key in ("messages", "conversation", "conversation_history", "session_history", "history", "transcript", "turns"):
        messages.extend(collect_messages_from_value(payload.get(key)))
    for key in ("transcript_path", "conversation_path", "history_path", "session_history_path"):
        path_value = payload.get(key)
        if path_value:
            messages.extend(read_session_messages_from_path(Path(str(path_value)).expanduser()))

    state_messages = load_state(root, payload).get("session_messages") or []
    messages.extend(collect_messages_from_value(state_messages))
    return dedupe_messages(messages)


def read_session_messages_from_path(path: Path) -> list[dict[str, str]]:
    if not path.exists() or not path.is_file():
        return []
    try:
        data = path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return []
    trimmed = data.strip()
    if not trimmed:
        return []
    try:
        return collect_messages_from_value(json.loads(trimmed))
    except json.JSONDecodeError:
        pass

    messages: list[dict[str, str]] = []
    for line in trimmed.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            messages.extend(collect_messages_from_value(json.loads(line)))
        except json.JSONDecodeError:
            messages.append({"role": "transcript", "content": redact_sensitive(line)})
    return messages


def collect_messages_from_value(value: Any) -> list[dict[str, str]]:
    if value is None:
        return []
    if isinstance(value, str):
        return [{"role": "transcript", "content": redact_sensitive(value)}]
    if isinstance(value, list):
        messages: list[dict[str, str]] = []
        for item in value:
            messages.extend(collect_messages_from_value(item))
        return messages
    if not isinstance(value, dict):
        return []

    messages: list[dict[str, str]] = []
    direct = message_from_mapping(value)
    if direct:
        messages.append(direct)
    for key in ("message", "messages", "conversation", "history", "turns", "children"):
        nested = value.get(key)
        if nested is not None and nested is not value:
            messages.extend(collect_messages_from_value(nested))
    return messages


def message_from_mapping(value: dict[str, Any]) -> dict[str, str] | None:
    nested_message = value.get("message") if isinstance(value.get("message"), dict) else {}
    role = first_string(value, ("role", "speaker", "author", "type")) or first_string(nested_message, ("role", "type"))
    tool_name = first_string(value, ("tool_name", "tool", "name")) or first_string(nested_message, ("tool_name", "name"))
    content = content_from_mapping(nested_message) or content_from_mapping(value)
    if not content and tool_name:
        content = json.dumps(value, ensure_ascii=False, sort_keys=True)
    content = redact_sensitive(compact_memory_text(content, memory_max_chars()))
    if not content:
        return None
    message = {"role": normalize_role(role), "content": content}
    if tool_name:
        message["tool_name"] = tool_name
    created_at = first_string(value, ("created_at", "timestamp", "time"))
    if created_at:
        message["created_at"] = created_at
    return message


def content_from_mapping(value: Any) -> str:
    if not isinstance(value, dict):
        return ""
    for key in ("content", "text", "prompt", "response", "summary", "result", "output"):
        if key in value:
            text = text_from_value(value[key])
            if text.strip():
                return text.strip()
    return ""


def text_from_value(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, str):
        return value
    if isinstance(value, list):
        return "\n".join(filter(None, (text_from_value(item).strip() for item in value)))
    if isinstance(value, dict):
        for key in ("text", "content", "message", "input", "output"):
            if key in value:
                text = text_from_value(value[key])
                if text.strip():
                    return text
        return json.dumps(value, ensure_ascii=False, sort_keys=True)
    return str(value)


def collect_payload_tool_entries(payload: dict[str, Any]) -> list[dict[str, str]]:
    entries: list[dict[str, str]] = []
    for key in ("tool_calls", "tool_results", "tool_history", "tools"):
        value = payload.get(key)
        if value is None:
            continue
        values = value if isinstance(value, list) else [value]
        for item in values:
            if not isinstance(item, dict):
                continue
            tool_name = first_string(item, ("tool_name", "name", "tool")) or "tool"
            content = content_from_mapping(item) or json.dumps(item, ensure_ascii=False, sort_keys=True)
            entries.append({"tool_name": tool_name, "content": redact_sensitive(content)})
    return entries


def dedupe_messages(messages: list[dict[str, str]]) -> list[dict[str, str]]:
    deduped: list[dict[str, str]] = []
    seen: set[str] = set()
    for message in messages:
        content = str(message.get("content") or "").strip()
        if not content:
            continue
        role = normalize_role(message.get("role", ""))
        key = hashlib.sha256(f"{role}\0{content}".encode("utf-8")).hexdigest()
        if key in seen:
            continue
        seen.add(key)
        clean = dict(message)
        clean["role"] = role
        clean["content"] = content
        deduped.append(clean)
    return deduped


def session_history_text(messages: list[dict[str, str]]) -> str:
    lines = []
    for message in messages:
        role = normalize_role(message.get("role", "")) or "unknown"
        tool_name = str(message.get("tool_name") or "").strip()
        prefix = f"{role}({tool_name})" if tool_name else role
        lines.append(f"{prefix}: {message.get('content', '').strip()}")
    return compact_memory_text("\n".join(lines), memory_max_chars())


def split_memory_sentences(text: str) -> list[str]:
    normalized = re.sub(r"[\r\n]+", "\n", text.strip())
    parts = re.split(r"(?:\n+|(?<=[。！？!?；;])\s*)", normalized)
    sentences: list[str] = []
    for part in parts:
        sentence = compact_text(part, 600)
        if len(sentence) >= 12:
            sentences.append(sentence)
    return sentences


def write_memory_import_file(
    root: Path,
    payload: dict[str, Any],
    source_type: str,
    records: list[dict[str, Any]],
) -> Path:
    directory = state_base_dir() / "memory-imports" / session_state_key(root, payload)
    directory.mkdir(parents=True, exist_ok=True)
    path = directory / f"{source_type}.jsonl"
    content = "\n".join(json.dumps(record, ensure_ascii=False, sort_keys=True) for record in records)
    path.write_text(content + "\n", encoding="utf-8")
    return path


def import_prompt_knowledge(root: Path, payload: dict[str, Any], prompt: str) -> str:
    if not should_import_knowledge(prompt):
        return ""
    targets = knowledge_import_targets(root, payload, prompt)
    if not targets:
        return "Agent-Memory knowledge import trigger detected, but no readable Markdown path or URL was found."

    messages: list[str] = []
    for target in targets:
        source_id = knowledge_source_id(target)
        result = run_code_context(root, ["import", "knowledge", str(target), source_id])
        if result.returncode != 0:
            messages.append(f"failed {target}: {compact_text(result.stderr or result.stdout)}")
            continue
        messages.append(compact_text(result.stdout) or f"imported {target}")
    return "Agent-Memory manual knowledge import: " + "; ".join(messages)


def should_import_knowledge(prompt: str) -> bool:
    text = prompt.lower()
    if contains_any(text, ("不要导入", "无需导入", "不要加入知识库", "do not import")):
        return False
    if contains_any(text, KNOWLEDGE_TRIGGER_TERMS):
        return True
    return "知识库" in text and contains_any(text, ("导入", "加入", "放入", "写入", "收录", "当作", "作为"))


def knowledge_import_targets(root: Path, payload: dict[str, Any], prompt: str) -> list[Path]:
    targets: list[Path] = []
    for url in extract_urls(prompt):
        imported = materialize_url_knowledge(payload, url)
        if imported:
            targets.append(imported)
    for raw_path in extract_candidate_paths(prompt):
        path = resolve_prompt_path(root, raw_path)
        if path and is_supported_knowledge_path(path):
            targets.append(path)
    return dedupe_paths(targets)


def extract_urls(text: str) -> list[str]:
    return re.findall(r"https?://[^\s，。；;）)\]}>\"']+", text)


def extract_candidate_paths(text: str) -> list[str]:
    candidates: list[str] = []
    for pattern in (r"`([^`]+)`", r"[\"“”']([^\"“”']+)[\"“”']"):
        candidates.extend(match.strip() for match in re.findall(pattern, text) if match.strip())
    candidates.extend(re.findall(r"(?<!\w)(?:\.{1,2}/|/|~)[^\s，。；;）)\]}>\"']+", text))
    candidates.extend(re.findall(r"(?<!\w)[\w./@-]+\.(?:md|markdown)(?!\w)", text, flags=re.IGNORECASE))
    candidates.extend(re.findall(r"(?:把|将)\s*([^\s，。；;]+)\s*(?:文档|目录|路径|当作|作为|导入|加入)", text))
    return [candidate for candidate in candidates if not candidate.startswith(("http://", "https://"))]


def resolve_prompt_path(root: Path, raw_path: str) -> Path | None:
    value = raw_path.strip().rstrip("，。；;,.。")
    if not value:
        return None
    path = Path(value).expanduser()
    if not path.is_absolute():
        path = root / path
    try:
        return path.resolve()
    except OSError:
        return None


def is_supported_knowledge_path(path: Path) -> bool:
    if path.is_dir():
        return True
    return path.is_file() and path.suffix.lower() in {".md", ".markdown"}


def materialize_url_knowledge(payload: dict[str, Any], url: str) -> Path | None:
    try:
        request = Request(url, headers={"User-Agent": "Agent-Memory hook"})
        with urlopen(request, timeout=hook_timeout_seconds()) as response:  # nosec B310: explicit user-triggered import.
            data = response.read(DEFAULT_URL_IMPORT_MAX_BYTES + 1)
            content_type = response.headers.get("content-type", "")
    except (OSError, URLError):
        return None
    if len(data) > DEFAULT_URL_IMPORT_MAX_BYTES:
        data = data[:DEFAULT_URL_IMPORT_MAX_BYTES]
    text = data.decode("utf-8", errors="replace")
    markdown = html_to_markdown_text(text) if "html" in content_type.lower() or "<html" in text.lower() else text
    digest = hashlib.sha256(url.encode("utf-8")).hexdigest()[:16]
    directory = state_base_dir() / "knowledge-imports" / session_state_key_from_payload(payload)
    directory.mkdir(parents=True, exist_ok=True)
    path = directory / f"url-{digest}.md"
    title = extract_html_title(text) or url
    content = f"# {title}\n\nsource_url: {url}\nimported_at: {utc_now()}\n\n{markdown.strip()}\n"
    path.write_text(content, encoding="utf-8")
    return path


def html_to_markdown_text(text: str) -> str:
    value = re.sub(r"(?is)<(script|style).*?>.*?</\1>", "", text)
    value = re.sub(r"(?i)<br\s*/?>", "\n", value)
    value = re.sub(r"(?i)</(p|div|section|article|h[1-6]|li|tr)>", "\n", value)
    value = re.sub(r"<[^>]+>", " ", value)
    value = html.unescape(value)
    value = re.sub(r"[ \t]+", " ", value)
    return re.sub(r"\n{3,}", "\n\n", value).strip()


def extract_html_title(text: str) -> str:
    match = re.search(r"(?is)<title[^>]*>(.*?)</title>", text)
    if not match:
        return ""
    return compact_text(html.unescape(re.sub(r"<[^>]+>", "", match.group(1))), 120)


def dedupe_paths(paths: list[Path]) -> list[Path]:
    deduped: list[Path] = []
    seen: set[str] = set()
    for path in paths:
        key = str(path)
        if key in seen:
            continue
        seen.add(key)
        deduped.append(path)
    return deduped


def knowledge_source_id(path: Path) -> str:
    return "manual-" + hashlib.sha256(str(path).encode("utf-8")).hexdigest()[:16]


def session_source_id(root: Path, payload: dict[str, Any], source_type: str) -> str:
    return f"session-{session_state_key(root, payload)[:16]}-{source_type}"


def session_state_key(root: Path, payload: dict[str, Any]) -> str:
    return state_path(root, payload).stem


def session_state_key_from_payload(payload: dict[str, Any]) -> str:
    external_session = str(payload.get("session_id") or payload.get("thread-id") or payload.get("thread_id") or "default")
    return hashlib.sha256(external_session.encode("utf-8")).hexdigest()


def first_string(value: dict[str, Any], keys: tuple[str, ...]) -> str:
    for key in keys:
        item = value.get(key)
        if isinstance(item, str) and item.strip():
            return item.strip()
    return ""


def normalize_role(role: str) -> str:
    value = str(role or "").strip().lower()
    if value in {"human", "user_message"}:
        return "user"
    if value in {"assistant_message", "ai"}:
        return "assistant"
    if value in {"tool_use", "tool_result", "function", "function_call"}:
        return "tool"
    return value


def redact_sensitive(text: str) -> str:
    value = re.sub(r"(?i)\b(bearer)\s+[A-Za-z0-9._~+/=-]+", r"\1 <redacted>", text)
    return re.sub(
        r"(?i)\b(api[_-]?key|token|password|passwd|secret)\b\s*[:=]\s*['\"]?[^\s'\"]+",
        r"\1=<redacted>",
        value,
    )


def compact_memory_text(text: str, max_chars: int) -> str:
    value = text.strip()
    if len(value) <= max_chars:
        return value
    keep = max_chars // 2
    return value[:keep].rstrip() + "\n[Agent-Memory memory truncated]\n" + value[-keep:].lstrip()


def memory_max_chars() -> int:
    return positive_int_env("AGENT_MEMORY_HOOK_MEMORY_MAX_CHARS", DEFAULT_MEMORY_MAX_CHARS)


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat()


def search_prompt_context(root: Path, payload: dict[str, Any], prompt: str) -> str:
    state = load_state(root, payload)
    session_id = str(state.get("agent_memory_session_id") or "").strip()
    search_types = choose_search_types(prompt)
    limit = positive_int_env("AGENT_MEMORY_HOOK_SEARCH_LIMIT", DEFAULT_LIMIT)

    args = ["search", str(root), prompt, str(limit), search_types]
    if session_id:
        args.append(f"--session-id={session_id}")

    result = run_code_context(root, args)
    if result.returncode != 0 and "not indexed" in (result.stderr + result.stdout).lower():
        ensure_indexed(root)
        result = run_code_context(root, args)
    if result.returncode != 0:
        raise RuntimeError(compact_text(result.stderr or result.stdout) or "search command failed")

    response = json.loads(result.stdout)
    returned_session_id = str(response.get("session_id") or "").strip()
    if returned_session_id:
        state["agent_memory_session_id"] = returned_session_id
        save_state(root, payload, state)

    return format_search_context(root, prompt, search_types, response)


def choose_search_types(prompt: str) -> str:
    text = prompt.lower()
    knowledge_terms = (
        "readme", "doc", "docs", "design", "architecture", "rule", "rules", "skill", "usage",
        "guide", "文档", "设计", "规范", "规则", "说明", "使用", "架构", "方案", "技能",
    )
    memory_terms = (
        "previous", "history", "memory", "preference", "conversation", "tool history",
        "之前", "历史", "经验", "偏好", "记忆", "上次",
    )
    code_terms = (
        "bug", "fix", "function", "method", "class", "test", "compile", "golang", ".go",
        "代码", "函数", "方法", "单测", "测试", "编译", "报错", "实现", "重构", "接口", "命令",
    )
    broad_terms = ("project", "repo", "repository", "context", "codex", "codebuddy", "项目", "上下文", "闭环", "适配")

    has_knowledge = contains_any(text, knowledge_terms)
    has_memory = contains_any(text, memory_terms)
    has_code = contains_any(text, code_terms) or bool(re.search(r"\b(cmd|internal|pkg|api|cli|mcp)\b", text))
    has_broad = contains_any(text, broad_terms)

    if has_broad or sum([has_knowledge, has_memory, has_code]) != 1:
        return "all"
    if has_code:
        return "code"
    if has_knowledge:
        return "knowledge"
    return "conversation,experience,preference,tool_history,fact"


def contains_any(text: str, terms: tuple[str, ...]) -> bool:
    return any(term in text for term in terms)


def positive_int_env(name: str, default: int) -> int:
    raw = os.environ.get(name, "")
    if raw.isdigit() and int(raw) > 0:
        return int(raw)
    return default


def state_base_dir() -> Path:
    configured = os.environ.get("AGENT_MEMORY_HOOK_STATE_DIR")
    if configured:
        return Path(configured).expanduser()
    return default_install_root() / "hook-state"


def state_path(root: Path, payload: dict[str, Any]) -> Path:
    external_session = str(
        payload.get("session_id")
        or payload.get("thread-id")
        or payload.get("thread_id")
        or "default"
    )
    key = hashlib.sha256(f"{root}\0{external_session}".encode("utf-8")).hexdigest()
    return state_base_dir() / "sessions" / f"{key}.json"


def load_state(root: Path, payload: dict[str, Any]) -> dict[str, Any]:
    path = state_path(root, payload)
    if not path.exists():
        return {}
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {}
    if isinstance(value, dict):
        return value
    return {}


def save_state(root: Path, payload: dict[str, Any], state: dict[str, Any]) -> None:
    path = state_path(root, payload)
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary_path = path.with_suffix(".tmp")
    temporary_path.write_text(json.dumps(state, ensure_ascii=False, indent=2), encoding="utf-8")
    temporary_path.replace(path)


def format_search_context(root: Path, prompt: str, search_types: str, response: dict[str, Any]) -> str:
    max_chars = positive_int_env("AGENT_MEMORY_HOOK_MAX_CONTEXT_CHARS", DEFAULT_MAX_CONTEXT_CHARS)
    lines = [
        "# Agent-Memory Retrieved Context",
        f"repo: {root}",
        f"query: {prompt}",
        f"search_types: {search_types}",
        f"session_id: {response.get('session_id', '')}",
        f"result_count: {response.get('result_count', 0)}",
        f"deduped_count: {response.get('deduped_count', 0)}",
        "",
    ]
    for item in response.get("results", []):
        location = item.get("location") or {}
        relative_path = location.get("relative_path") or item.get("source_id") or "unknown"
        start_line = location.get("start_line") or 0
        end_line = location.get("end_line") or 0
        heading = (
            f"## {item.get('rank', '?')}. [{item.get('category', '')}] "
            f"{relative_path}:{start_line}-{end_line} score={item.get('score', 0)}"
        )
        lines.append(heading)
        content = compact_multiline(str(item.get("content") or ""), 2200)
        if content:
            lines.append(content)
        lines.append("")
        if len("\n".join(lines)) >= max_chars:
            lines.append("[Agent-Memory context truncated by hook budget]")
            break
    return "\n".join(lines)[:max_chars]


def compact_text(text: str, max_chars: int = 2000) -> str:
    return compact_multiline(text, max_chars).replace("\n", " ").strip()


def compact_multiline(text: str, max_chars: int) -> str:
    value = text.strip()
    if len(value) <= max_chars:
        return value
    return value[: max_chars - 3].rstrip() + "..."


def emit_json(event_name: str, additional_context: str = "", suppress: bool = False) -> int:
    output: dict[str, Any] = {"continue": True}
    if suppress:
        output["suppressOutput"] = True
    if additional_context:
        output["hookSpecificOutput"] = {
            "hookEventName": event_name,
            "additionalContext": additional_context,
        }
    print(json.dumps(output, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
