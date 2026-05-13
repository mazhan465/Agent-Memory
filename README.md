# Agent-Memory

English | [简体中文](README.zh-CN.md)

> Documentation maintenance note: whenever the default English README changes, update the Chinese README at the same time; whenever the Chinese README changes, sync the English README as well.

Agent-Memory is a context memory and knowledge delivery engine for AI coding assistants, IDE agents, and automation toolchains. It indexes codebases, project documentation, historical conversations, tool logs, user preferences, and accumulated experience into searchable context, then provides accurate, traceable, and incrementally updated project knowledge for agents during every conversation or task.

Agent-Memory is not intended to be another note-taking system. Its goal is to become the context layer of agent workflows: retrieve relevant code, documentation, and memory before the agent starts working, then incrementally write new or changed content back to the index when the task ends so the next retrieval sees the latest context.

## Why Agent-Memory

AI coding assistants often face several recurring problems:

- Context windows are limited and cannot permanently hold an entire codebase or long-term decisions.
- Code, documentation, team rules, and historical fixes are scattered across different places.
- Every conversation has to rediscover project background, which is repetitive and easy to miss.
- After code changes, later agents may still work from stale context unless the project is re-indexed.

Agent-Memory solves these problems with a closed loop: index → retrieve → session-level deduplication → incremental sync.

## Core Features

- **Codebase indexing**: scans local repositories, respects `.gitignore` / `.xxxignore`, and supports custom extensions and ignore rules.
- **Incremental updates**: detects created, modified, and deleted files with file size, modification time, and hash snapshots to avoid full rebuilds every time.
- **Syntax-aware chunking**: uses tree-sitter to split Go / C++ code by package, imports, functions, classes, namespaces, and related structures; falls back to line-based chunks when parsing fails.
- **Documentation knowledge base**: imports Markdown documents while preserving metadata such as heading paths, document IDs, section IDs, and knowledge types.
- **Long-term memory import**: imports historical conversations, experience, preferences, tool history, and facts from JSON/JSONL sources.
- **Hybrid retrieval**: combines vector similarity, keywords, paths, and symbol metadata, with source-specific weighting.
- **Session-level deduplication**: search returns a `session_id`; later searches in the same session can pass that ID to avoid repeated results.
- **Multiple embedding backends**: includes a dependency-free hash embedder for local trials and supports OpenAI-compatible APIs and local Ollama models.
- **Multiple vector stores**: uses a local JSON vector store by default and can switch to Milvus.
- **CLI and MCP**: provides the `code-context` CLI and the `code-context-mcp` stdio MCP server.
- **Agent integration loop**: provides CodeBuddy / Codex skills, hooks, and installer scripts so agents can retrieve context at conversation start and sync changes at the end.
- **Recall evaluation**: supports JSON/JSONL evaluation sets, computes hit rate, precision, recall, F1, MRR, and nDCG, and is compatible with SWE-bench-style oracle files.

## Current Status

Agent-Memory is currently a runnable MVP / early preview. It is suitable for:

- Semantic retrieval over local codebases.
- Providing project context to IDE agents and CLI agents.
- Importing project documentation, team rules, and accumulated experience.
- Evaluating how different retrieval strategies or embedding/vector store choices affect recall quality.
- Exploring context loops driven by CodeBuddy / Codex hooks.

APIs and data formats may still evolve. Feedback and improvement suggestions based on real agent workflows are welcome.

## Quick Start

Install Agent-Memory first, then let your AI assistant use it automatically. Pick one of the following installation paths.

### 1. Manual Installation with the Python Installer

This is the recommended path for most users. The installer downloads GitHub Release binaries, installs `code-context` / `code-context-mcp` into an install `bin` directory, adds that directory to `PATH`, installs the `agent-memory-context` skill and hooks into the target project, and guides embedding and Milvus setup.

```bash
git clone https://github.com/mazhan465/Agent-Memory.git
cd Agent-Memory

python3 .codebuddy/skills/agent-memory-context/install.py \
  --project-root /path/to/your/project \
  --milvus-mode lite \
  --embedding ollama \
  --yes
```

For a CLI-only trial without hooks or Milvus:

```bash
python3 .codebuddy/skills/agent-memory-context/install.py \
  --project-root /path/to/your/project \
  --skip-hooks \
  --skip-milvus \
  --embedding hash \
  --yes
```

Useful installer options:

- `--install-root <dir>`: where binaries, `agent-memory.env`, and Milvus runtime files are installed.
- `--version <tag|latest>`: which GitHub Release to download.
- `--milvus-mode docker|lite|binary-guide`: choose Docker Compose, local Milvus Lite, or a manual Linux binary guide.
- `--embedding hash|ollama|openai-compatible`: choose the embedding provider.
- `--install-ollama`: try to install Ollama and pull the configured embedding model.

Base requirements:

- Git.
- Python 3, required for the installer, skill/hooks installation, and Milvus Lite.
- Docker / Docker Desktop, required only for `--milvus-mode docker`.
- Ollama, required only for `--embedding ollama` unless `--install-ollama` succeeds.
- An OpenAI-compatible API key, required only for `--embedding openai-compatible`.
- Go 1.25+, required only for source builds or development.

If you need to build from source, install Go 1.25+ and run:

```bash
make build

# Optional: build the MCP server
mkdir -p bin
go build -o bin/code-context-mcp ./cmd/code-context-mcp
```

### 2. AI-Assisted Installation Guide

If you want an AI coding assistant to install Agent-Memory for you, give it these instructions:

1. Download the GitHub Release archive matching the current platform from `https://github.com/mazhan465/Agent-Memory/releases`.
2. Extract `code-context` and `code-context-mcp`, place them in a directory on `PATH` such as `~/.agent-memory/bin`, and make them executable on Unix-like systems with `chmod +x`.
3. Verify `code-context config path` works. If `PATH` cannot be changed, set `AGENT_MEMORY_CODE_CONTEXT_BIN=/absolute/path/to/code-context` for hooks or agent commands.
4. Install the `agent-memory-context` skill into the target project and connect it to the assistant's hook system. If the Python installer is available, the AI may run it with `--skip-binary` after the binaries are already on `PATH`; otherwise it should copy the skill directory and adapt the hook templates for the target assistant.
5. For hook-capable assistants, wire equivalent events:
   - session start: ensure the repository is indexed or incrementally synced;
   - user prompt: search `code` plus `doc` context with a shared `session_id`;
   - session stop/end: write `conversation`, extract `experience` / `preference` / `tool_history` / `fact`, then run incremental `sync`.
6. If the assistant does not support hooks, use MCP configuration, startup instructions, wrapper commands, or scheduled automations as substitutes. The substitute must still automate `code` search, `knowledge` import when explicitly requested, `experience` / conversation memory writes, and final `sync`.

Minimum hook-equivalent CLI flow:

```bash
code-context index /path/to/repo
code-context search /path/to/repo "<user task>" 8 code
code-context search /path/to/repo "<user task>" 8 doc --session-id=<session_id>
code-context import knowledge /path/to/docs project-docs   # only when the user explicitly asks
code-context import memory experience /path/to/experience.jsonl project-experience
code-context sync /path/to/repo
```

### 3. Choose Embedding and Vector Store

Choose the embedding provider and vector store before the first large index:

| Scenario | Embedding | Vector store | Dependencies |
| --- | --- | --- | --- |
| Quick offline trial | `hash` | `local` | none beyond the binary |
| Private local development | `ollama` | `local` or `milvus` Lite | Ollama and the selected model; Python 3 for Milvus Lite |
| Larger local or team setup | `openai-compatible` or `ollama` | `milvus` Docker / remote Milvus | API key or Ollama; Docker / Docker Desktop or a reachable Milvus service |

Hash embedding is convenient but has limited semantic quality. For real agent workflows, prefer Ollama local embeddings or an OpenAI-compatible embedding API, and use Milvus when the indexed corpus is large or shared across tools.

Example Ollama setup:

```bash
export AGENT_MEMORY_EMBEDDING_PROVIDER=ollama
export AGENT_MEMORY_OLLAMA_HOST=http://127.0.0.1:11434
export AGENT_MEMORY_OLLAMA_EMBEDDING_MODEL=embeddinggemma

ollama pull embeddinggemma
```

Example OpenAI-compatible setup:

```bash
export AGENT_MEMORY_EMBEDDING_PROVIDER=openai-compatible
export AGENT_MEMORY_OPENAI_BASE_URL=https://api.openai.com/v1
export AGENT_MEMORY_OPENAI_API_KEY=your-api-key
export AGENT_MEMORY_OPENAI_EMBEDDING_MODEL=text-embedding-3-small
export AGENT_MEMORY_OPENAI_EMBEDDING_DIMENSIONS=1024
```

Example Milvus setup:

```bash
export AGENT_MEMORY_VECTOR_STORE=milvus
export AGENT_MEMORY_MILVUS_ADDRESS=localhost:19530
export AGENT_MEMORY_MILVUS_COLLECTION=agent_memory_chunks
```

Use `--milvus-mode lite` for a small local non-Docker setup, `--milvus-mode docker` for local standalone Milvus, or configure `AGENT_MEMORY_MILVUS_ADDRESS` to point to an existing Milvus service.

### 4. Initialize Configuration

```bash
code-context config init
code-context config path
```

The default config file is stored under `.AgentMemory/config.yaml` in the user home directory. You can also set `AGENT_MEMORY_CONFIG` to point to another path. Environment variables have higher priority than config files.

### 5. Index a Codebase

```bash
code-context index /path/to/repo
```

Check indexing status:

```bash
code-context status /path/to/repo
```

Run incremental sync later:

```bash
code-context sync /path/to/repo
# Or sync all indexed paths
code-context sync --all
```

### 6. Search Context

```bash
code-context search /path/to/repo "where is authentication handled"
```

Specify result count and search type:

```bash
code-context search /path/to/repo "Milvus vector store" 5 knowledge
```

Reuse a `session_id` for session-level deduplication:

```bash
code-context search /path/to/repo "Milvus vector store" 5 knowledge --session-id=session-dev
```

Available search types:

- `all`
- `code`
- `doc`: all non-code sources, including `knowledge`, `conversation`, `experience`, `preference`, `tool_history`, and `fact`
- `knowledge`
- `conversation`
- `experience`
- `preference`
- `tool_history`
- `fact`

## CLI Commands

```text
code-context index <path>
code-context sync <path|--all>
code-context search <path> <query> [limit] [types] [session-id]
code-context import knowledge <path> [source-id]
code-context import memory <type> <json-or-jsonl-path> [source-id]
code-context source list [type]
code-context source clear <type> <source-id>
code-context eval recall <path> <cases-json-or-jsonl> [limit] [types]
code-context config init [--force]
code-context config path
code-context status <path>
code-context clear <path>
```

## Import Documentation and Long-Term Memory

Import a Markdown documentation knowledge base:

```bash
./bin/code-context import knowledge /path/to/docs project-docs
```

Import experience memory:

```bash
cat > /tmp/agent-memory-experience.jsonl <<'EOF'
{"id":"go-log-rule","content":"Go log messages should put the description first and parameters after it","experience_kind":"domain","domain_path":"programming/go","tags":["go","logging"]}
EOF

./bin/code-context import memory experience /tmp/agent-memory-experience.jsonl go-experience
```

List sources:

```bash
./bin/code-context source list
./bin/code-context source list knowledge
```

Clear a source:

```bash
./bin/code-context source clear knowledge project-docs
```

## Embedding Configuration

### Default Hash Embedder

The default `hash` provider has zero external dependencies and can run offline. It is useful for quick workflow validation, but its semantic quality is limited.

### OpenAI-Compatible Embedding

```bash
export AGENT_MEMORY_EMBEDDING_PROVIDER=openai-compatible
export AGENT_MEMORY_OPENAI_BASE_URL=https://api.openai.com/v1
export AGENT_MEMORY_OPENAI_API_KEY=your-api-key
export AGENT_MEMORY_OPENAI_EMBEDDING_MODEL=text-embedding-3-small
export AGENT_MEMORY_OPENAI_EMBEDDING_DIMENSIONS=1024
export AGENT_MEMORY_OPENAI_MAX_BATCH_SIZE=10

./bin/code-context index /path/to/repo
```

### Local Ollama Embedding

```bash
export AGENT_MEMORY_EMBEDDING_PROVIDER=ollama
export AGENT_MEMORY_OLLAMA_HOST=http://127.0.0.1:11434
export AGENT_MEMORY_OLLAMA_EMBEDDING_MODEL=embeddinggemma

ollama pull embeddinggemma
./bin/code-context index /path/to/repo
```

## Vector Store Configuration

### Local JSON Store

The default `local` vector store writes index data to a local directory, which is suitable for development and single-machine trials.

### Milvus

```bash
export AGENT_MEMORY_VECTOR_STORE=milvus
export AGENT_MEMORY_MILVUS_ADDRESS=localhost:19530
export AGENT_MEMORY_MILVUS_COLLECTION=agent_memory_chunks

./bin/code-context index /path/to/repo
```

When using the built-in skill installer, you can choose the Milvus runtime mode:

```bash
# Docker Compose mode, suitable for local setups close to standalone deployment
python3 .codebuddy/skills/agent-memory-context/install.py \
  --project-root . \
  --milvus-mode docker \
  --embedding ollama

# Local non-Docker mode, using a Milvus Lite server on localhost:19530
python3 .codebuddy/skills/agent-memory-context/install.py \
  --project-root . \
  --milvus-mode lite \
  --embedding ollama
```

`--milvus-mode docker` requires Docker / Docker Desktop. `--milvus-mode lite` creates a Python virtual environment under the install directory and installs `pymilvus[milvus-lite]`, which is suitable for small local development tests on macOS, Windows, and Linux.

## MCP Server

Agent-Memory provides a stdio MCP server:

```bash
./bin/code-context-mcp
```

Current MCP tools:

- `index_codebase`: index or incrementally refresh a local codebase path.
- `search_code`: search code snippets in an indexed codebase.
- `clear_index`: clear vector data and snapshots for a codebase.
- `get_indexing_status`: query indexing status for a codebase.

Example MCP configuration snippet:

```json
{
  "mcpServers": {
    "agent-memory": {
      "command": "/absolute/path/to/code-context-mcp"
    }
  }
}
```

Configuration formats vary across MCP clients. Adjust the snippet according to the target client documentation.

## CodeBuddy / Codex Agent Loop

The project includes the `agent-memory-context` skill, which can install hooks for CodeBuddy and Codex:

```bash
python3 .codebuddy/skills/agent-memory-context/install.py --project-root .

# Choose Milvus Lite for local development without Docker
python3 .codebuddy/skills/agent-memory-context/install.py --project-root . --milvus-mode lite
```

The installer writes related templates into the target project:

- `.codebuddy/settings.json`
- `.codebuddy/rules/agent-memory-context.md`
- `.codex/config.toml`
- `AGENTS.md`

Runtime loop:

1. `SessionStart`: check the index and initialize or incrementally sync when needed.
2. `UserPromptSubmit`: search Agent-Memory from the user prompt and inject the returned context.
3. `Stop` / `SessionEnd`: run `sync` at task end so the next turn sees the latest content.

See `docs/agent_memory_skill_integration.md` for more details.

## Recall Quality Evaluation

Create an evaluation set:

```bash
cat > /tmp/agent-memory-recall-eval.jsonl <<'EOF'
{"id":"auth","query":"authenticate user token","expected":[{"relative_path":"auth.go"}]}
EOF
```

Run evaluation:

```bash
./bin/code-context eval recall /path/to/repo /tmp/agent-memory-recall-eval.jsonl 10 code
```

The evaluator also supports several SWE-bench / claude-context-style fields, such as `instances`, `problem_statement`, `patch`, `oracles`, and `oracle_files`.

Evaluation output includes recall quality metrics and runtime efficiency metrics. Quality metrics include hit rate, precision, recall, F1, MRR, nDCG, and file-level precision/recall/F1. Efficiency metrics include per-case and summary latency, estimated result tokens, result character counts, result counts, deduplicated counts, and searched namespace counts.

## Common Environment Variables

| Variable | Description |
| --- | --- |
| `AGENT_MEMORY_CONFIG` | Config file path |
| `AGENT_MEMORY_HOME` | Local storage directory |
| `AGENT_MEMORY_EMBEDDING_PROVIDER` | `hash` / `openai` / `openai-compatible` / `ollama` |
| `AGENT_MEMORY_OPENAI_BASE_URL` | OpenAI-compatible API endpoint |
| `AGENT_MEMORY_OPENAI_API_KEY` | OpenAI-compatible API key |
| `AGENT_MEMORY_OPENAI_EMBEDDING_MODEL` | OpenAI-compatible embedding model |
| `AGENT_MEMORY_OPENAI_EMBEDDING_DIMENSIONS` | OpenAI-compatible output vector dimensions, defaults to `1024` |
| `AGENT_MEMORY_OPENAI_MAX_BATCH_SIZE` | OpenAI-compatible batch size, defaults to `10` |
| `AGENT_MEMORY_OLLAMA_HOST` | Ollama endpoint |
| `AGENT_MEMORY_OLLAMA_EMBEDDING_MODEL` | Ollama embedding model |
| `AGENT_MEMORY_VECTOR_STORE` | `local` / `milvus` |
| `AGENT_MEMORY_MILVUS_ADDRESS` | Milvus address |
| `AGENT_MEMORY_MILVUS_COLLECTION` | Milvus collection name |
| `AGENT_MEMORY_DEFAULT_SEARCH_TYPES` | Default search types |
| `AGENT_MEMORY_CUSTOM_EXTENSIONS` | Additional extensions to scan, such as `.vue,.svelte` |
| `AGENT_MEMORY_CUSTOM_IGNORE_PATTERNS` | Additional ignore globs, such as `private/**,*.backup` |

## Project Layout

```text
cmd/
  code-context/      # CLI entry point
  code-context-mcp/  # MCP stdio server entry point
internal/
  catalog/           # Source catalog
  config/            # Config loading and default config generation
  contextdoc/        # Document nodes and knowledge type models
  embed/             # Hash / OpenAI-compatible / Ollama embedders
  indexer/           # Full and incremental indexing pipeline
  mcpserver/         # MCP stdio protocol and tool implementations
  scanner/           # File scanning and ignore rules
  searcher/          # Vector and hybrid retrieval
  snapshot/          # Index snapshots
  splitter/          # Tree-sitter and line-based chunking
  vectorstore/       # Local / Milvus vector stores
.codebuddy/skills/
  agent-memory-context/ # CodeBuddy / Codex skill, hooks, and installer
```

## Development

```bash
# Format
make fmt

# Test
make test

# Build CLI
make build

# Build MCP server
mkdir -p bin
go build -o bin/code-context-mcp ./cmd/code-context-mcp
```

Project development rules are documented in `docs/development_rules.md`.

## Roadmap

- More complete MCP tools for unified search across code, docs, experience, preferences, and tool history.
- Stronger session context construction that generates structured context packs for prompts.
- Source bundle export, import, backup, and cross-device sync.
- Tree-sitter structural chunking for more languages.
- More complete packaging, release binaries, and platform compatibility tests.
- Improve code search quality with ideas from claude-context: BM25 / sparse vectors, dense vectors, RRF rerank, or pluggable rerankers.
- Improve Milvus search with ideas from claude-context: true hybrid search in the Milvus backend, aligned with local keyword, path, and symbol metadata fusion strategies.
- Improve the evaluation system with ideas from claude-context: token, cost, latency, tool-call count, indexing duration, agent task success rate, and other benchmarks integrated with current recall / precision / F1 / MRR / nDCG metrics.

## Documentation

- `README.md`: default English README.
- `README.zh-CN.md`: Chinese README.
- `docs/design/overall_design.md`: overall design.
- `docs/modules/module_design.md`: detailed module design.
- `docs/development_rules.md`: project development rules.
- `docs/development_log.md`: development log.
- `docs/agent_memory_skill_integration.md`: Agent-Memory skill, CodeBuddy hooks, and Codex hooks integration guide.

## License

This project is licensed under the [GNU General Public License v3.0](LICENSE).

See [LICENSE](LICENSE) for details.
