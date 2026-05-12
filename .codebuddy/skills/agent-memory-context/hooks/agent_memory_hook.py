#!/usr/bin/env python3
"""Agent-Memory hook bridge for CodeBuddy and Codex."""

from __future__ import annotations

import argparse
import hashlib
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

DEFAULT_LIMIT = 8
DEFAULT_MAX_CONTEXT_CHARS = 12000
DEFAULT_TIMEOUT_SECONDS = 120


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
            context = search_prompt_context(root, payload, prompt)
            return emit_json(event_name, additional_context=context)
        if args.mode == "session-start":
            summary = ensure_indexed(root, sync_when_indexed=True)
            return emit_json(event_name, additional_context=f"Agent-Memory context loop active. {summary}")
        summary = ensure_indexed(root, sync_when_indexed=True)
        return emit_json(event_name, additional_context=f"Agent-Memory incremental sync finished. {summary}", suppress=True)
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
