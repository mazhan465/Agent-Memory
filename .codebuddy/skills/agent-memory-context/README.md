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
8. Installs Milvus standalone by downloading the official Docker Compose file and starting it with Docker Compose.
9. Guides the user to choose `hash`, `ollama`, or `openai-compatible` embeddings and writes `<install-root>/agent-memory.env`.

## Platform Notes

- macOS: install script uses shell profile updates for `PATH`; optional Ollama install uses Homebrew when available.
- Windows: install script uses the user `Path` environment variable; Docker Desktop is required for Milvus; optional Ollama install uses `winget` when available.
- Linux: Docker Compose is used for Milvus; binary download and PATH setup follow the same Unix flow.

## Required External Components

- Docker / Docker Desktop for Milvus.
- Python 3 for hooks and installer.
- Git for repository root discovery.
- Optional: Ollama for local embedding models.
- Optional: OpenAI-compatible embedding API credentials.

## Manual Runtime Commands

If hooks are disabled, run manually:

```bash
code-context search <repo> "<query>" 8 all
code-context search <repo> "<query>" 8 code --session-id=<session_id>
code-context sync <repo>
```
