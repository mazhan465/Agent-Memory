# Agent-Memory Context Rule

This project must use Agent-Memory as the first source of repository context.

Mandatory workflow:

1. At the beginning of every user prompt, search Agent-Memory with a query derived from the prompt.
2. Keep the returned `session_id` and pass it to all later searches in the same conversation to deduplicate already returned context.
3. Choose search types from `code`, `doc`, `knowledge`, `conversation`, `experience`, `preference`, `tool_history`, `fact`, or `all` based on the prompt intent. `doc` means all non-code sources; for mixed or unclear prompts, run separate `code` and `doc` searches by default.
4. Automatically index project directories explicitly requested by the user or configured in `AGENT_MEMORY_PROJECT_DIRS` / `AGENT_MEMORY_EXTRA_PROJECT_DIRS`.
5. When indexing or syncing a project, import changed `README` and other Markdown files as `conversation` or `experience` memory according to content.
6. At the end of every assistant turn or session, write session history as `conversation`, extract `experience`, `preference`, `tool_history`, and `fact`, then run incremental sync for the current repository and configured extra project directories.
7. Import `knowledge` only when the user explicitly asks to add a document, path, or link to the knowledge base.
8. If hooks are unavailable, manually run the equivalent `code-context search`, `code-context import memory`, and `code-context sync` commands.

Primary commands:

```bash
code-context search <repo> "<query>" 8 code
code-context search <repo> "<query>" 8 doc --session-id=<session_id>
code-context sync <repo>
```

If the binary is not on `PATH`, set `AGENT_MEMORY_CODE_CONTEXT_BIN` to the installed binary path.
