---
name: agent-memory-context
description: This skill should be used at the start of every Agent-Memory project conversation or user prompt to retrieve deduplicated project context, and at the end of every turn/session to incrementally sync changed content back into Agent-Memory. It also installs CodeBuddy/Codex hooks, binaries, Milvus, and embedding guidance for Agent-Memory.
allowed-tools: Read, Grep, Bash, Edit, Write
---

# Agent-Memory Context Loop

Enforce a closed context loop between AI assistants and Agent-Memory.

## Installation First

For a new machine or project, install this skill package before relying on automatic hooks:

```bash
python3 ${CODEBUDDY_SKILL_DIR}/install.py --project-root <repo>
```

The installer keeps all skill assets in one directory, installs project hooks/templates, downloads Agent-Memory binaries into an install `bin` directory, adds that directory to `PATH`, installs Milvus through Docker Compose or local Milvus Lite, and guides the user through embedding provider selection. Go is only required when building Agent-Memory from source.

## Mandatory Runtime Contract

Treat the following workflow as non-optional whenever working in a repository using this skill.

1. Before answering or editing, derive a search query from the user's latest prompt.
2. Search Agent-Memory for relevant context before using local assumptions.
3. Preserve and reuse the returned `session_id` for every subsequent search in the same assistant conversation.
4. Use that `session_id` on later searches so Agent-Memory can deduplicate results already returned.
5. At the end of the assistant turn/session, persist the session history into Agent-Memory as `conversation` memory.
6. At the same end step, extract stable `experience`, `preference`, `tool_history`, and `fact` records from the session and import them under the matching source types.
7. Run incremental indexing so changed files are searchable in the next turn.
8. If the index is missing, run a full `index` before searching or syncing.
9. If hooks are available, rely on hooks for start/search and end/sync; otherwise execute the workflow manually and keep the rule in persistent project instructions.
10. Treat `knowledge` as manually curated content only: import it when the user explicitly asks to add a document/path/link to the knowledge base.

## Search Scope Selection

Choose the narrowest useful search scope from the prompt intent:

- Use `code` for implementation, debugging, refactoring, tests, functions, packages, CLI behavior, MCP tools, or compile errors.
- Use `knowledge` for README, design docs, module docs, project rules, usage guides, architecture, or skill instructions.
- Use `experience,conversation,preference,tool_history,fact` for prior decisions, lessons learned, user preferences, historical fixes, and operational context.
- Use `all` for mixed tasks, unclear prompts, broad planning, or repository onboarding.

Prefer one strong query plus one narrowed follow-up query over many low-signal searches. Reuse the same `session_id` for all of them.

## CLI Workflow

Use the repository root as `<repo>`.

```bash
code-context search <repo> "<query>" 8 all
code-context search <repo> "<query>" 8 code --session-id=<session_id>
code-context sync <repo>
```

If `code-context` is unavailable, set `AGENT_MEMORY_CODE_CONTEXT_BIN` to the installed binary path or use a local development binary.

## MCP Workflow

If an MCP client exposes Agent-Memory tools, prefer them only when they satisfy the same contract:

- `index_codebase` can perform full or incremental refresh for a path.
- `search_code` can search indexed code.
- `get_indexing_status` can check whether a path is indexed.

When MCP search does not support `session_id` or non-code source types, use the CLI `search` command.

## Hook Integration

All hook assets live in `${CODEBUDDY_SKILL_DIR}`:

- Installer: `${CODEBUDDY_SKILL_DIR}/install.py`
- Hook script: `${CODEBUDDY_SKILL_DIR}/hooks/agent_memory_hook.py`
- Templates: `${CODEBUDDY_SKILL_DIR}/templates/`

Installed hook behavior:

- `SessionStart`: ensure the repository is indexed or incrementally synced.
- `UserPromptSubmit`: record the user prompt, optionally import explicitly requested documents/links as `knowledge`, search with the user's prompt, inject results, and persist the Agent-Memory `session_id` mapping.
- `Stop` / `SessionEnd`: write session history as `conversation`, auto-extract `experience`, `preference`, `tool_history`, and `fact`, then run incremental sync for the next turn.

## Response Discipline

After retrieving context, ground answers in returned results and cite repository files when discussing code or docs. If search fails, state the failure briefly, fix the index when possible, and retry once before proceeding. Do not fabricate project behavior that is discoverable from Agent-Memory.
