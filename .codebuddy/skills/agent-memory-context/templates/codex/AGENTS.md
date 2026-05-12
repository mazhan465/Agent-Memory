# Agent-Memory Agent Instructions

These instructions apply to the whole repository.

## Mandatory Context Loop

- Before answering or editing for any user prompt, search Agent-Memory with a query derived from the prompt.
- Choose search scope by intent: `code` for implementation, `knowledge` for docs/design/rules, memory source types for history/preferences, and `all` for mixed or unclear tasks.
- Preserve the returned `session_id` and reuse it for all subsequent searches in the same conversation.
- At the end of each assistant turn/session, persist session history as `conversation`, extract `experience`, `preference`, `tool_history`, and `fact`, then incrementally sync the repository so changed files are indexed for future searches.
- Import `knowledge` only when the user explicitly asks to add a document, path, or link to the knowledge base.
- If the index is missing, run full indexing before searching or syncing.

## Commands

```bash
code-context search <repo> "<query>" 8 all
code-context search <repo> "<query>" 8 code --session-id=<session_id>
code-context sync <repo>
```

If `code-context` is not on `PATH`, use the binary under the Agent-Memory install `bin` directory or set `AGENT_MEMORY_CODE_CONTEXT_BIN`.

## Hooks

Codex project hooks are configured in `.codex/config.toml` and implemented by `.codebuddy/skills/agent-memory-context/hooks/agent_memory_hook.py`.

- `SessionStart` ensures the repository is indexed or synced.
- `UserPromptSubmit` records the prompt, imports explicitly requested knowledge documents/links, searches Agent-Memory, and injects retrieved context.
- `Stop` writes session memory, extracts structured memory types, and runs incremental sync.

If hooks are not active, manually follow the commands above.
