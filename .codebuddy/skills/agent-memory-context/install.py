#!/usr/bin/env python3
"""Install Agent-Memory skill assets, binaries, Milvus, and runtime guidance."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import platform
import re
import shlex
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import time
from typing import Any
from urllib.error import URLError
from urllib.request import Request, urlopen, urlretrieve
import zipfile

SKILL_NAME = "agent-memory-context"
DEFAULT_GITHUB_REPO = "mazhan465/Agent-Memory"
DEFAULT_MILVUS_VERSION = "v3.0-beta"
BEGIN_MARKER = "# BEGIN Agent-Memory managed block"
END_MARKER = "# END Agent-Memory managed block"


def main() -> int:
    args = parse_args()
    skill_dir = Path(__file__).resolve().parent
    project_root = Path(args.project_root).expanduser().resolve() if args.project_root else discover_project_root(skill_dir)
    install_root = Path(args.install_root).expanduser().resolve() if args.install_root else default_install_root()
    python_cmd = args.python_cmd or default_python_cmd()

    print(f"Agent-Memory skill dir: {skill_dir}")
    print(f"Target project root: {project_root}")
    print(f"Install root: {install_root}")

    if not args.skip_hooks:
        install_project_assets(skill_dir, project_root, python_cmd)
    if not args.skip_binary:
        install_binaries(args, install_root)
        ensure_path(install_root / "bin", args.yes)
    if not args.skip_milvus:
        install_milvus(args, install_root)

    embedding = choose_embedding(args)
    write_env_file(install_root, embedding, args)
    maybe_install_ollama(args, install_root, embedding)
    print_next_steps(project_root, install_root, embedding)
    return 0


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Install Agent-Memory context-loop skill")
    parser.add_argument("--project-root", help="Project root to install CodeBuddy/Codex hooks into")
    parser.add_argument(
        "--install-root",
        help="Directory for Agent-Memory binaries, env file, and Milvus runtime files",
    )
    parser.add_argument("--github-repo", default=DEFAULT_GITHUB_REPO, help="GitHub repo that publishes binaries")
    parser.add_argument("--version", default="latest", help="GitHub release tag or latest")
    parser.add_argument("--python-cmd", help="Python command used inside generated hook configs")
    parser.add_argument("--skip-hooks", action="store_true", help="Do not install CodeBuddy/Codex hook configs")
    parser.add_argument("--skip-binary", action="store_true", help="Do not download code-context binaries")
    parser.add_argument("--skip-milvus", action="store_true", help="Do not install a Milvus runtime")
    parser.add_argument(
        "--milvus-mode",
        default="docker",
        choices=["docker", "lite", "binary-guide"],
        help="Milvus runtime mode: docker compose, local Milvus Lite, or Linux binary guide",
    )
    parser.add_argument("--no-start-milvus", action="store_true", help="Install Milvus files but do not start Milvus")
    parser.add_argument(
        "--milvus-version",
        default=DEFAULT_MILVUS_VERSION,
        help="Milvus release tag for Docker compose or binary guide",
    )
    parser.add_argument("--milvus-port", default="19530", help="Milvus listen port for local modes")
    parser.add_argument(
        "--milvus-data-dir",
        help="Milvus Lite data directory; defaults to <install-root>/milvus-lite/data",
    )
    parser.add_argument(
        "--milvus-lite-package",
        default="pymilvus[milvus-lite]",
        help="Python package spec used for local Milvus Lite installation",
    )
    parser.add_argument(
        "--embedding",
        default="prompt",
        choices=["prompt", "hash", "ollama", "openai", "openai-compatible"],
        help="Embedding provider to configure",
    )
    parser.add_argument("--install-ollama", action="store_true", help="Try to install Ollama and pull an embedding model")
    parser.add_argument(
        "--ollama-mode",
        default="auto",
        choices=["auto", "host", "docker", "skip"],
        help="Ollama runtime mode: host binary, Docker container, skip, or auto",
    )
    parser.add_argument(
        "--ollama-accelerator",
        default="auto",
        choices=["auto", "cpu", "nvidia", "amd-rocm"],
        help="Ollama Docker acceleration profile",
    )
    parser.add_argument("--no-start-ollama", action="store_true", help="Install Ollama files but do not start Ollama")
    parser.add_argument("--ollama-model", default="embeddinggemma", help="Ollama embedding model to pull/configure")
    parser.add_argument(
        "--ollama-dimensions",
        type=positive_int,
        default=0,
        help="Optional Ollama /api/embed dimensions value; 0 uses the model default",
    )
    parser.add_argument("--openai-base-url", default="https://api.openai.com/v1", help="OpenAI-compatible base URL")
    parser.add_argument("--openai-model", default="text-embedding-3-small", help="OpenAI-compatible embedding model")
    parser.add_argument("--yes", "-y", action="store_true", help="Accept safe installer prompts")
    return parser.parse_args()


def discover_project_root(skill_dir: Path) -> Path:
    result = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        cwd=str(skill_dir),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        check=False,
    )
    if result.returncode == 0 and result.stdout.strip():
        return Path(result.stdout.strip()).resolve()

    parts = list(skill_dir.parts)
    if ".codebuddy" in parts:
        index = parts.index(".codebuddy")
        return Path(*parts[:index]).resolve()
    return Path.cwd().resolve()


def default_install_root() -> Path:
    if platform.system().lower() == "windows":
        local_app_data = os.environ.get("LOCALAPPDATA")
        if local_app_data:
            return Path(local_app_data) / "AgentMemory"
    return Path.home() / ".agent-memory"


def default_python_cmd() -> str:
    if platform.system().lower() == "windows":
        return "python"
    return "python3"


def positive_int(value: str) -> int:
    parsed = int(value)
    if parsed < 0:
        raise argparse.ArgumentTypeError("value must be >= 0")
    return parsed


def install_project_assets(skill_dir: Path, project_root: Path, python_cmd: str) -> None:
    target_skill_dir = project_root / ".codebuddy" / "skills" / SKILL_NAME
    if skill_dir != target_skill_dir.resolve():
        copy_skill_dir(skill_dir, target_skill_dir)
        print(f"Installed skill directory: {target_skill_dir}")
    else:
        print("Skill directory already lives in the target project")

    install_codebuddy_settings(target_skill_dir, project_root, python_cmd)
    install_codebuddy_rule(target_skill_dir, project_root)
    install_codex_config(target_skill_dir, project_root, python_cmd)
    install_agents_md(target_skill_dir, project_root)


def copy_skill_dir(source: Path, target: Path) -> None:
    def ignore(_: str, names: list[str]) -> set[str]:
        return {name for name in names if name in {"__pycache__", ".DS_Store"} or name.endswith(".pyc")}

    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copytree(source, target, dirs_exist_ok=True, ignore=ignore)


def render_template(path: Path, values: dict[str, str]) -> str:
    content = path.read_text(encoding="utf-8")
    for key, value in values.items():
        content = content.replace("{{" + key + "}}", value)
    return content


def backup_file(path: Path) -> None:
    if path.exists():
        backup = path.with_name(path.name + ".agent-memory.bak")
        shutil.copy2(path, backup)


def install_codebuddy_settings(skill_dir: Path, project_root: Path, python_cmd: str) -> None:
    template_path = skill_dir / "templates" / "codebuddy" / "settings.json"
    rendered = render_template(template_path, {"PYTHON_CMD": python_cmd})
    desired = json.loads(rendered)
    settings_path = project_root / ".codebuddy" / "settings.json"
    settings_path.parent.mkdir(parents=True, exist_ok=True)

    current: dict[str, Any] = {}
    if settings_path.exists():
        current = json.loads(settings_path.read_text(encoding="utf-8"))
    merged = merge_codebuddy_hooks(current, desired)
    backup_file(settings_path)
    settings_path.write_text(json.dumps(merged, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"Installed CodeBuddy hooks: {settings_path}")


def merge_codebuddy_hooks(current: dict[str, Any], desired: dict[str, Any]) -> dict[str, Any]:
    result = dict(current)
    result_hooks = dict(result.get("hooks") or {})
    desired_hooks = desired.get("hooks") or {}
    for event, groups in desired_hooks.items():
        existing_groups = list(result_hooks.get(event) or [])
        existing_keys = {json.dumps(group, sort_keys=True, ensure_ascii=False) for group in existing_groups}
        for group in groups:
            key = json.dumps(group, sort_keys=True, ensure_ascii=False)
            if key not in existing_keys:
                existing_groups.append(group)
                existing_keys.add(key)
        result_hooks[event] = existing_groups
    result["hooks"] = result_hooks
    return result


def install_codebuddy_rule(skill_dir: Path, project_root: Path) -> None:
    source = skill_dir / "templates" / "codebuddy" / "rules" / "agent-memory-context.md"
    target = project_root / ".codebuddy" / "rules" / "agent-memory-context.md"
    target.parent.mkdir(parents=True, exist_ok=True)
    backup_file(target)
    shutil.copy2(source, target)
    print(f"Installed CodeBuddy rule: {target}")


def install_codex_config(skill_dir: Path, project_root: Path, python_cmd: str) -> None:
    template_path = skill_dir / "templates" / "codex" / "config.toml"
    full_template = render_template(template_path, {"PYTHON_CMD": python_cmd})
    hooks_block = codex_hooks_only_block(full_template)
    config_path = project_root / ".codex" / "config.toml"
    config_path.parent.mkdir(parents=True, exist_ok=True)
    existing = config_path.read_text(encoding="utf-8") if config_path.exists() else ""

    updated = ensure_codex_hooks_feature(existing)
    updated = replace_marked_block(updated, hooks_block)
    backup_file(config_path)
    config_path.write_text(updated.rstrip() + "\n", encoding="utf-8")
    print(f"Installed Codex hooks: {config_path}")


def codex_hooks_only_block(full_template: str) -> str:
    lines = full_template.splitlines()
    output: list[str] = []
    in_features = False
    for line in lines:
        if line.strip() == "[features]":
            in_features = True
            continue
        if in_features and line.startswith("["):
            in_features = False
        if in_features:
            continue
        output.append(line)
    return "\n".join(line for line in output if line.strip())


def ensure_codex_hooks_feature(text: str) -> str:
    if not text.strip():
        return "[features]\ncodex_hooks = true\n"
    if re.search(r"(?m)^\s*codex_hooks\s*=", text):
        return re.sub(r"(?m)^\s*codex_hooks\s*=\s*[^\n]+", "codex_hooks = true", text)
    match = re.search(r"(?m)^\[features\]\s*$", text)
    if match:
        insert_at = match.end()
        return text[:insert_at] + "\ncodex_hooks = true" + text[insert_at:]
    return "[features]\ncodex_hooks = true\n\n" + text


def replace_marked_block(text: str, block: str) -> str:
    managed = f"{BEGIN_MARKER}\n{block.rstrip()}\n{END_MARKER}\n"
    pattern = re.compile(re.escape(BEGIN_MARKER) + r".*?" + re.escape(END_MARKER) + r"\n?", re.S)
    if pattern.search(text):
        return pattern.sub(managed, text)
    separator = "\n" if text.endswith("\n") else "\n\n"
    return text + separator + managed


def install_agents_md(skill_dir: Path, project_root: Path) -> None:
    template = (skill_dir / "templates" / "codex" / "AGENTS.md").read_text(encoding="utf-8")
    agents_path = project_root / "AGENTS.md"
    existing = agents_path.read_text(encoding="utf-8") if agents_path.exists() else ""
    updated = replace_marked_block(existing, template)
    backup_file(agents_path)
    agents_path.write_text(updated.rstrip() + "\n", encoding="utf-8")
    print(f"Installed Codex AGENTS.md block: {agents_path}")


def install_binaries(args: argparse.Namespace, install_root: Path) -> None:
    bin_dir = install_root / "bin"
    bin_dir.mkdir(parents=True, exist_ok=True)
    release = fetch_release(args.github_repo, args.version)
    if not release:
        print("WARNING: Could not fetch GitHub release metadata; skipped binary download")
        return
    for binary in ["code-context", "code-context-mcp"]:
        if download_binary_from_release(release, binary, bin_dir):
            continue
        print(f"WARNING: Could not find a release asset for {binary}; install it manually into {bin_dir}")


def fetch_release(repo: str, version: str) -> dict[str, Any] | None:
    endpoint = "latest" if version == "latest" else f"tags/{version}"
    url = f"https://api.github.com/repos/{repo}/releases/{endpoint}"
    try:
        request = Request(url, headers={"Accept": "application/vnd.github+json", "User-Agent": "agent-memory-installer"})
        with urlopen(request, timeout=30) as response:
            return json.loads(response.read().decode("utf-8"))
    except (OSError, URLError, json.JSONDecodeError) as exc:
        print(f"WARNING: failed to query {url}: {exc}")
        return None


def download_binary_from_release(release: dict[str, Any], binary: str, bin_dir: Path) -> bool:
    os_name = normalized_os()
    arch = normalized_arch()
    assets = release.get("assets") or []
    for asset in assets:
        name = str(asset.get("name") or "")
        if not matches_binary_asset(name, binary, os_name, arch):
            continue
        url = str(asset.get("browser_download_url") or "")
        if not url:
            continue
        print(f"Downloading {name}")
        return install_asset(url, name, binary, bin_dir)
    return download_binary_by_convention(release, binary, bin_dir)


def download_binary_by_convention(release: dict[str, Any], binary: str, bin_dir: Path) -> bool:
    tag = str(release.get("tag_name") or "").strip()
    html_url = str(release.get("html_url") or "")
    match = re.match(r"https://github.com/([^/]+/[^/]+)/releases/", html_url)
    if not tag or not match:
        return False
    repo = match.group(1)
    os_name = normalized_os()
    arch = normalized_arch()
    suffix = ".exe" if os_name == "windows" else ""
    base_names = [
        f"{binary}-{os_name}-{arch}{suffix}",
        f"{binary}_{os_name}_{arch}{suffix}",
        f"{binary}-{tag}-{os_name}-{arch}{suffix}",
        f"{binary}_{tag}_{os_name}_{arch}{suffix}",
    ]
    candidates = base_names + [name + ext for name in base_names for ext in (".tar.gz", ".tgz", ".zip")]
    for name in candidates:
        url = f"https://github.com/{repo}/releases/download/{tag}/{name}"
        if install_asset(url, name, binary, bin_dir, quiet=True):
            return True
    return False


def normalized_os() -> str:
    system = platform.system().lower()
    if system == "darwin":
        return "darwin"
    if system == "windows":
        return "windows"
    return "linux"


def normalized_arch() -> str:
    machine = platform.machine().lower()
    if machine in {"x86_64", "amd64"}:
        return "amd64"
    if machine in {"arm64", "aarch64"}:
        return "arm64"
    return machine


def matches_binary_asset(name: str, binary: str, os_name: str, arch: str) -> bool:
    lower = name.lower()
    if binary not in lower or "sha256" in lower or "checksum" in lower:
        return False
    arch_aliases = {"amd64": ["amd64", "x86_64"], "arm64": ["arm64", "aarch64"]}.get(arch, [arch])
    return os_name in lower and any(alias in lower for alias in arch_aliases)


def install_asset(url: str, asset_name: str, binary: str, bin_dir: Path, quiet: bool = False) -> bool:
    suffix = ".exe" if normalized_os() == "windows" else ""
    target = bin_dir / f"{binary}{suffix}"
    with tempfile.TemporaryDirectory() as tmp:
        download_path = Path(tmp) / asset_name
        try:
            urlretrieve(url, download_path)
        except (OSError, URLError) as exc:
            if not quiet:
                print(f"WARNING: failed to download {url}: {exc}")
            return False
        if asset_name.endswith(".zip"):
            with zipfile.ZipFile(download_path) as archive:
                return extract_binary_from_zip(archive, binary, target)
        if asset_name.endswith((".tar.gz", ".tgz")):
            with tarfile.open(download_path) as archive:
                return extract_binary_from_tar(archive, binary, target)
        shutil.copy2(download_path, target)
        make_executable(target)
        print(f"Installed binary: {target}")
        return True


def extract_binary_from_zip(archive: zipfile.ZipFile, binary: str, target: Path) -> bool:
    for member in archive.namelist():
        if Path(member).name in {binary, binary + ".exe"}:
            target.write_bytes(archive.read(member))
            make_executable(target)
            print(f"Installed binary: {target}")
            return True
    return False


def extract_binary_from_tar(archive: tarfile.TarFile, binary: str, target: Path) -> bool:
    for member in archive.getmembers():
        if Path(member.name).name not in {binary, binary + ".exe"} or not member.isfile():
            continue
        extracted = archive.extractfile(member)
        if extracted is None:
            continue
        target.write_bytes(extracted.read())
        make_executable(target)
        print(f"Installed binary: {target}")
        return True
    return False


def make_executable(path: Path) -> None:
    if normalized_os() != "windows":
        mode = path.stat().st_mode
        path.chmod(mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)


def ensure_path(bin_dir: Path, yes: bool) -> None:
    current_paths = os.environ.get("PATH", "").split(os.pathsep)
    if str(bin_dir) in current_paths:
        return
    if not yes and not confirm(f"Add {bin_dir} to your user PATH?"):
        print(f"Add this directory to PATH manually: {bin_dir}")
        return
    if normalized_os() == "windows":
        update_windows_path(bin_dir)
    else:
        update_unix_path(bin_dir)


def confirm(prompt: str) -> bool:
    if not sys.stdin.isatty():
        return False
    answer = input(f"{prompt} [y/N] ").strip().lower()
    return answer in {"y", "yes"}


def update_unix_path(bin_dir: Path) -> None:
    shell = Path(os.environ.get("SHELL", "")).name
    profile = Path.home() / (".zshrc" if shell == "zsh" else ".bashrc")
    line = f'export PATH="{bin_dir}:$PATH"'
    marker = "# Agent-Memory PATH"
    content = profile.read_text(encoding="utf-8") if profile.exists() else ""
    if str(bin_dir) not in content:
        profile.write_text(content.rstrip() + f"\n\n{marker}\n{line}\n", encoding="utf-8")
    print(f"Updated PATH in {profile}. Restart the shell or run: {line}")


def update_windows_path(bin_dir: Path) -> None:
    command = [
        "powershell",
        "-NoProfile",
        "-Command",
        (
            "$p=[Environment]::GetEnvironmentVariable('Path','User');"
            f"$b='{str(bin_dir)}';"
            "if(($p -split ';') -notcontains $b){"
            "[Environment]::SetEnvironmentVariable('Path', ($p.TrimEnd(';')+';'+$b), 'User')}"
        ),
    ]
    result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, check=False)
    if result.returncode == 0:
        print("Updated user PATH. Restart terminal/IDE to apply it.")
    else:
        print(f"WARNING: failed to update PATH automatically: {result.stderr.strip()}")
        print(f"Add this directory to PATH manually: {bin_dir}")


def install_milvus(args: argparse.Namespace, install_root: Path) -> None:
    if args.milvus_mode == "lite":
        install_milvus_lite(args, install_root)
        return
    if args.milvus_mode == "binary-guide":
        write_milvus_binary_guide(args, install_root)
        return
    install_milvus_docker(args, install_root)


def install_milvus_docker(args: argparse.Namespace, install_root: Path) -> None:
    milvus_dir = install_root / "milvus"
    milvus_dir.mkdir(parents=True, exist_ok=True)
    compose_path = milvus_dir / "docker-compose.yml"
    url = (
        "https://github.com/milvus-io/milvus/releases/download/"
        f"{args.milvus_version}/milvus-standalone-docker-compose.yml"
    )
    try:
        print(f"Downloading Milvus compose: {url}")
        urlretrieve(url, compose_path)
    except (OSError, URLError) as exc:
        print(f"WARNING: failed to download Milvus compose: {exc}")
        print("Install Docker Desktop and download the compose file from Milvus docs manually.")
        return

    if args.no_start_milvus:
        print(f"Milvus compose saved: {compose_path}")
        return
    if not shutil.which("docker"):
        print("Docker was not found. Install Docker Desktop, then run:")
        print(f"  docker compose -f {compose_path} up -d")
        return
    result = subprocess.run(
        ["docker", "compose", "-f", str(compose_path), "up", "-d"],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        check=False,
    )
    if result.returncode == 0:
        print("Milvus started at localhost:19530; Web UI: http://127.0.0.1:9091/webui/")
    else:
        print(f"WARNING: docker compose failed: {result.stderr.strip()}")
        print(f"Retry manually: docker compose -f {compose_path} up -d")


def install_milvus_lite(args: argparse.Namespace, install_root: Path) -> None:
    milvus_dir = install_root / "milvus-lite"
    venv_dir = milvus_dir / "venv"
    data_dir = Path(args.milvus_data_dir).expanduser().resolve() if args.milvus_data_dir else milvus_dir / "data"
    log_dir = milvus_dir / "logs"
    milvus_dir.mkdir(parents=True, exist_ok=True)
    data_dir.mkdir(parents=True, exist_ok=True)
    log_dir.mkdir(parents=True, exist_ok=True)

    if not ensure_venv(venv_dir):
        print("WARNING: failed to create Python venv for Milvus Lite")
        print("Install manually: python -m venv <venv> && pip install -U 'pymilvus[milvus-lite]'")
        return
    pip = venv_bin(venv_dir, "pip")
    result = subprocess.run(
        [str(pip), "install", "-U", args.milvus_lite_package],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        print(f"WARNING: failed to install Milvus Lite: {result.stderr.strip()}")
        return

    server = venv_bin(venv_dir, "milvus-lite")
    if not server.exists():
        print(f"WARNING: Milvus Lite command was not found after install: {server}")
        print(f"Try manually: {pip} install -U {args.milvus_lite_package}")
        return
    write_milvus_lite_start_scripts(milvus_dir, server, data_dir, args.milvus_port)
    if args.no_start_milvus:
        print(f"Milvus Lite installed. Start it with: {milvus_dir / start_script_name()}")
        return
    start_milvus_lite(server, data_dir, args.milvus_port, log_dir)


def ensure_venv(venv_dir: Path) -> bool:
    if venv_bin(venv_dir, "python").exists():
        return True
    result = subprocess.run([sys.executable, "-m", "venv", str(venv_dir)], check=False)
    return result.returncode == 0


def venv_bin(venv_dir: Path, name: str) -> Path:
    if normalized_os() == "windows":
        suffix = ".exe" if name in {"python", "pip", "milvus-lite"} else ""
        return venv_dir / "Scripts" / f"{name}{suffix}"
    return venv_dir / "bin" / name


def write_milvus_lite_start_scripts(milvus_dir: Path, server: Path, data_dir: Path, port: str) -> None:
    if normalized_os() == "windows":
        script = milvus_dir / "start-milvus-lite.ps1"
        script.write_text(
            f"& '{server}' server --data-dir '{data_dir}' --port {port}\n",
            encoding="utf-8",
        )
        print(f"Wrote Milvus Lite start script: {script}")
        return
    script = milvus_dir / "start-milvus-lite.sh"
    script.write_text(
        "#!/usr/bin/env bash\n"
        "set -euo pipefail\n"
        f"exec {shlex.quote(str(server))} server --data-dir {shlex.quote(str(data_dir))} --port {port}\n",
        encoding="utf-8",
    )
    make_executable(script)
    print(f"Wrote Milvus Lite start script: {script}")


def start_script_name() -> str:
    if normalized_os() == "windows":
        return "start-milvus-lite.ps1"
    return "start-milvus-lite.sh"


def start_milvus_lite(server: Path, data_dir: Path, port: str, log_dir: Path) -> None:
    stdout_path = log_dir / "milvus-lite.out.log"
    stderr_path = log_dir / "milvus-lite.err.log"
    stdout_file = stdout_path.open("ab")
    stderr_file = stderr_path.open("ab")
    command = [str(server), "server", "--data-dir", str(data_dir), "--port", str(port)]
    try:
        if normalized_os() == "windows":
            process = subprocess.Popen(
                command,
                stdout=stdout_file,
                stderr=stderr_file,
                creationflags=subprocess.CREATE_NEW_PROCESS_GROUP,
            )
        else:
            process = subprocess.Popen(command, stdout=stdout_file, stderr=stderr_file, start_new_session=True)
    except OSError as exc:
        stdout_file.close()
        stderr_file.close()
        print(f"WARNING: failed to start Milvus Lite: {exc}")
        print(f"Start manually: {' '.join(command)}")
        return
    time.sleep(2)
    if process.poll() is None:
        print(f"Milvus Lite started at localhost:{port} pid={process.pid}")
        print(f"Milvus Lite logs: {stdout_path}, {stderr_path}")
    else:
        print(f"WARNING: Milvus Lite exited early with code {process.returncode}; check logs: {stderr_path}")
    stdout_file.close()
    stderr_file.close()


def write_milvus_binary_guide(args: argparse.Namespace, install_root: Path) -> None:
    guide_path = install_root / "milvus-binary" / "README.md"
    guide_path.parent.mkdir(parents=True, exist_ok=True)
    guide = f"""# Milvus binary standalone guide

Milvus upstream documents a non-Docker runtime path for Linux amd64 by running etcd, MinIO,
and the Milvus standalone binary directly.

This mode is not fully automated here because upstream binary setup is Linux-focused and may
still require Docker once to extract the Milvus binary from `milvusdb/milvus:{args.milvus_version}`.

Reference: https://github.com/milvus-io/milvus/blob/master/deployments/binary/README.md

Runtime address expected by Agent-Memory:

```bash
export AGENT_MEMORY_VECTOR_STORE=milvus
export AGENT_MEMORY_MILVUS_ADDRESS=localhost:{args.milvus_port}
```

For local non-Docker development on macOS, Windows, or Linux, prefer:

```bash
python3 install.py --milvus-mode lite
```
"""
    guide_path.write_text(guide, encoding="utf-8")
    print(f"Wrote Milvus binary standalone guide: {guide_path}")


def choose_embedding(args: argparse.Namespace) -> str:
    if args.embedding != "prompt":
        return args.embedding
    if not sys.stdin.isatty():
        print("Embedding provider not selected; defaulting to hash. Re-run with --embedding ollama/openai if needed.")
        return "hash"
    print("Choose embedding provider:")
    print("  1) hash: zero dependency, weak semantics")
    print("  2) ollama: local embedding model, recommended for private local setup")
    print("  3) openai-compatible: remote embedding API")
    answer = input("Selection [1/2/3, default 1]: ").strip()
    if answer == "2":
        return "ollama"
    if answer == "3":
        return "openai-compatible"
    return "hash"


def write_env_file(install_root: Path, embedding: str, args: argparse.Namespace) -> None:
    env_path = install_root / "agent-memory.env"
    bin_name = "code-context.exe" if normalized_os() == "windows" else "code-context"
    lines = [
        "# Agent-Memory runtime environment",
        f"AGENT_MEMORY_CODE_CONTEXT_BIN={install_root / 'bin' / bin_name}",
        "AGENT_MEMORY_VECTOR_STORE=milvus",
        "AGENT_MEMORY_MILVUS_ADDRESS=localhost:19530",
        "AGENT_MEMORY_MILVUS_COLLECTION=agent_memory_chunks",
        f"AGENT_MEMORY_EMBEDDING_PROVIDER={embedding}",
    ]
    if embedding == "ollama":
        lines.extend([
            "AGENT_MEMORY_OLLAMA_HOST=http://127.0.0.1:11434",
            f"AGENT_MEMORY_OLLAMA_EMBEDDING_MODEL={args.ollama_model}",
        ])
        if args.ollama_dimensions > 0:
            lines.append(f"AGENT_MEMORY_OLLAMA_EMBEDDING_DIMENSIONS={args.ollama_dimensions}")
    if embedding in {"openai", "openai-compatible"}:
        lines.extend([
            f"AGENT_MEMORY_OPENAI_BASE_URL={args.openai_base_url}",
            "AGENT_MEMORY_OPENAI_API_KEY=replace-with-your-api-key",
            f"AGENT_MEMORY_OPENAI_EMBEDDING_MODEL={args.openai_model}",
        ])
    install_root.mkdir(parents=True, exist_ok=True)
    env_path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(f"Wrote environment guide: {env_path}")


def maybe_install_ollama(args: argparse.Namespace, install_root: Path, embedding: str) -> None:
    if embedding != "ollama" or args.ollama_mode == "skip":
        return
    mode = resolve_ollama_mode(args)
    if mode == "docker":
        install_ollama_docker(args, install_root)
        return
    if args.install_ollama:
        install_ollama_runtime()
    if shutil.which("ollama"):
        subprocess.run(["ollama", "pull", args.ollama_model], check=False)
    else:
        print("Ollama is not installed. Install it from https://ollama.com and run:")
        print(f"  ollama pull {args.ollama_model}")


def resolve_ollama_mode(args: argparse.Namespace) -> str:
    if args.ollama_mode != "auto":
        return args.ollama_mode
    if args.install_ollama and normalized_os() == "linux":
        return "docker"
    return "host"


def install_ollama_docker(args: argparse.Namespace, install_root: Path) -> None:
    ollama_dir = install_root / "ollama"
    ollama_dir.mkdir(parents=True, exist_ok=True)
    compose_path = ollama_dir / "docker-compose.yml"
    accelerator = resolve_ollama_accelerator(args.ollama_accelerator)
    compose_path.write_text(render_ollama_compose(accelerator), encoding="utf-8")
    print(f"Wrote Ollama compose: {compose_path}")
    print_ollama_accelerator_note(accelerator)
    if args.no_start_ollama:
        print(f"Ollama compose saved. Start it with: docker compose -f {compose_path} up -d")
        return
    if not shutil.which("docker"):
        print("Docker was not found. Install Docker Desktop or Docker Engine, then run:")
        print(f"  docker compose -f {compose_path} up -d")
        return
    result = subprocess.run(
        ["docker", "compose", "-f", str(compose_path), "up", "-d"],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        print(f"WARNING: Ollama docker compose failed: {result.stderr.strip()}")
        print(f"Retry manually: docker compose -f {compose_path} up -d")
        return
    print("Ollama started at http://127.0.0.1:11434")
    wait_for_ollama("http://127.0.0.1:11434")
    pull_ollama_model_docker(args.ollama_model)


def resolve_ollama_accelerator(requested: str) -> str:
    if requested != "auto":
        return requested
    if normalized_os() == "linux":
        if shutil.which("nvidia-smi"):
            return "nvidia"
        if Path("/dev/kfd").exists() and Path("/dev/dri").exists():
            return "amd-rocm"
    return "cpu"


def render_ollama_compose(accelerator: str) -> str:
    image = "ollama/ollama:rocm" if accelerator == "amd-rocm" else "ollama/ollama"
    lines = [
        "services:",
        "  ollama:",
        f"    image: {image}",
        "    container_name: agent-memory-ollama",
        "    restart: unless-stopped",
        "    ports:",
        "      - \"11434:11434\"",
        "    volumes:",
        "      - ollama:/root/.ollama",
    ]
    if accelerator == "nvidia":
        lines.append("    gpus: all")
    if accelerator == "amd-rocm":
        lines.extend([
            "    devices:",
            "      - /dev/kfd:/dev/kfd",
            "      - /dev/dri:/dev/dri",
        ])
    lines.extend(["volumes:", "  ollama:", ""])
    return "\n".join(lines)


def print_ollama_accelerator_note(accelerator: str) -> None:
    if accelerator == "nvidia":
        print("Ollama Docker accelerator: NVIDIA. Ensure NVIDIA Container Toolkit is installed.")
        return
    if accelerator == "amd-rocm":
        print("Ollama Docker accelerator: AMD ROCm. Ensure /dev/kfd and /dev/dri are available on Linux.")
        return
    if normalized_os() == "darwin":
        print("Ollama Docker accelerator: CPU. Use host Ollama on macOS if you need Apple Silicon GPU acceleration.")
    else:
        print("Ollama Docker accelerator: CPU.")


def wait_for_ollama(host: str) -> None:
    deadline = time.time() + 30
    endpoint = host.rstrip("/") + "/api/version"
    while time.time() < deadline:
        try:
            with urlopen(endpoint, timeout=2):
                return
        except (OSError, URLError):
            time.sleep(1)
    print(f"WARNING: Ollama health check timed out: {endpoint}")


def pull_ollama_model_docker(model: str) -> None:
    model = model.strip()
    if not model:
        return
    result = subprocess.run(
        ["docker", "exec", "agent-memory-ollama", "ollama", "pull", model],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        check=False,
    )
    if result.returncode == 0:
        print(f"Pulled Ollama model: {model}")
    else:
        print(f"WARNING: failed to pull Ollama model {model}: {result.stderr.strip()}")
        print(f"Retry manually: docker exec agent-memory-ollama ollama pull {model}")


def install_ollama_runtime() -> None:
    system = normalized_os()
    if system == "darwin" and shutil.which("brew"):
        subprocess.run(["brew", "install", "ollama"], check=False)
        return
    if system == "windows" and shutil.which("winget"):
        subprocess.run(["winget", "install", "-e", "--id", "Ollama.Ollama"], check=False)
        return
    print("Automatic host Ollama install is not available on this platform. Install from https://ollama.com")


def print_next_steps(project_root: Path, install_root: Path, embedding: str) -> None:
    env_path = install_root / "agent-memory.env"
    print("\nNext steps:")
    print("1. Restart terminal/IDE so PATH changes are visible.")
    if normalized_os() == "windows":
        print(f"2. Configure environment variables from: {env_path}")
    else:
        print(f"2. Load environment variables when needed: set -a; source {env_path}; set +a")
    print("3. In CodeBuddy, open /hooks and approve the project hooks if prompted.")
    print("4. In Codex, mark the project trusted so .codex/config.toml hooks are loaded.")
    print(f"5. Initialize and index: code-context config init && code-context index {project_root}")
    if embedding == "hash":
        print("6. hash embedding is only for quick trial; choose Ollama or OpenAI-compatible embeddings for better recall.")
    elif embedding == "ollama":
        print("6. Ensure Ollama is running before indexing; Docker mode listens on http://127.0.0.1:11434.")
    else:
        print("6. Replace AGENT_MEMORY_OPENAI_API_KEY in the env file before indexing.")


if __name__ == "__main__":
    raise SystemExit(main())
