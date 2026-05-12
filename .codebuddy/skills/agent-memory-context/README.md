# Agent-Memory Context Skill

This directory is the complete Agent-Memory skill package. Keep skill instructions, hooks, templates, installer, and install guidance here so CodeBuddy and Codex integrations can be installed from one place.

## Contents

```text
SKILL.md                         # Runtime contract for AI assistants
install.py                       # Cross-platform installer
hooks/agent_memory_hook.py        # CodeBuddy/Codex lifecycle hook bridge
templates/codebuddy/settings.json # CodeBuddy hooks template
templates/codebuddy/rules/        # CodeBuddy rule template
templates/codex/config.toml       # Codex hooks template
templates/codex/AGENTS.md         # Codex fallback instructions
```

## Quick Install

From this repository:

```bash
python3 .codebuddy/skills/agent-memory-context/install.py --project-root .
```

Non-interactive example:

```bash
python3 .codebuddy/skills/agent-memory-context/install.py \
  --project-root . \
  --yes \
  --milvus-mode lite \
  --embedding ollama \
  --install-ollama
```

## What the Installer Does

1. Installs/copies this skill directory into `<project>/.codebuddy/skills/agent-memory-context`.
2. Merges CodeBuddy hooks into `<project>/.codebuddy/settings.json`.
3. Installs CodeBuddy project rules into `<project>/.codebuddy/rules/agent-memory-context.md`.
4. Installs Codex hooks into `<project>/.codex/config.toml`.
5. Appends the Agent-Memory instruction block into `<project>/AGENTS.md`.
6. Downloads `code-context` and `code-context-mcp` release assets from GitHub into `<install-root>/bin`.
7. Adds `<install-root>/bin` to the user `PATH` when allowed.
8. Installs Milvus with the selected mode: Docker Compose by default, local non-Docker Milvus Lite with `--milvus-mode lite`, or a Linux binary guide with `--milvus-mode binary-guide`.
9. Guides the user to choose `hash`, `ollama`, or `openai-compatible` embeddings and writes `<install-root>/agent-memory.env`.

## Platform Notes

- macOS: install script uses shell profile updates for `PATH`; optional Ollama install uses Homebrew when available; Milvus Lite is the local non-Docker option.
- Windows: install script uses the user `Path` environment variable; optional Ollama install uses `winget` when available; Milvus Lite avoids requiring Docker Desktop for local development.
- Linux: Docker Compose is the default Milvus mode; Milvus Lite and the binary guide are available for non-Docker local setups.

## Required External Components

- Python 3 for hooks, installer, and Milvus Lite mode.
- Git for repository root discovery.
- Optional: Docker / Docker Desktop for Milvus Docker mode.
- Optional: Go 1.25+ only when building from source; release binaries do not require Go.
- Optional: Ollama for local embedding models.
- Optional: OpenAI-compatible embedding API credentials.

## Runtime Memory Flow

- Prompt time: cache the user prompt, import `knowledge` only when the user explicitly asks to add a document, path, or link to the knowledge base, then search Agent-Memory.
- Turn/session end: persist session history as `conversation`, extract `experience`, `preference`, `tool_history`, and `fact`, then run incremental sync.

## Manual Runtime Commands

If hooks are disabled, run manually:

```bash
code-context search <repo> "<query>" 8 all
code-context import memory experience <json-or-jsonl-path> <source-id>
code-context sync <repo>
```
