# Agent-Memory

[English](README.md) | 简体中文

> 文档维护约定：以后修改默认英文 README 时，需要同步更新本中文版本；修改中文版本时也需要同步英文版本。

Agent-Memory 是一个面向 AI 编程助手、IDE Agent 和自动化工具链的上下文记忆与知识供给引擎。它可以把代码库、项目文档、历史会话、工具记录、用户偏好和经验沉淀统一索引成可检索的上下文，并在每次对话或任务执行时为 Agent 提供准确、可追溯、可增量更新的项目知识。

它的目标不是再做一个笔记系统，而是成为 Agent 工作流中的“上下文层”：在用户发出提示词时先检索相关代码、文档与记忆；在任务结束时把新增或修改的内容增量写回索引，让下一次检索拿到最新上下文。

## 为什么需要 Agent-Memory

AI 编程助手经常面临几个问题：

- 上下文窗口有限，无法长期记住整个代码库和历史决策。
- 代码、文档、团队规范、历史修复经验分散在不同位置。
- 每轮对话都要重新搜索项目背景，重复且容易遗漏。
- 代码修改后如果不重新索引，后续 Agent 仍会基于旧上下文工作。

Agent-Memory 通过“索引 → 检索 → 会话级去重 → 增量同步”的闭环解决这些问题。

## 核心能力

- **代码库索引**：扫描本地代码库，遵循 `.gitignore` / `.xxxignore`，支持自定义扩展名和忽略规则。
- **增量更新**：基于文件大小、修改时间和 hash snapshot 判断新增、修改、删除文件，避免每次全量重建。
- **语法切块**：基于 tree-sitter 对 Go / C++ 代码按 package、import、function、class、namespace 等结构切块；解析失败时回退行级切块。
- **文档知识库**：导入 Markdown 文档，保留标题路径、文档 ID、章节 ID、知识类型等元数据。
- **长期记忆导入**：导入历史会话、经验、偏好、工具历史和事实等 JSON/JSONL source。
- **混合检索**：结合向量相似度、关键词、路径和符号元数据，并支持按 source 类型配置权重。
- **会话级去重**：搜索结果返回 `session_id`；同一会话后续搜索带上该 ID，可避免重复返回已看过的内容。
- **多种 Embedding 后端**：内置 hash embedder 便于本地快速试用，也支持 OpenAI-compatible API 和 Ollama 本地模型。
- **多种向量存储**：默认本地 JSON 存储，支持切换到 Milvus。
- **CLI 与 MCP**：提供 `code-context` 命令行工具和 `code-context-mcp` stdio MCP Server。
- **Agent 集成闭环**：提供 CodeBuddy / Codex skill、hooks 和安装脚本，使 Agent 在对话开始检索上下文、结束时增量同步。
- **召回评估**：支持 JSON/JSONL 评估集，计算 hit rate、precision、recall、F1、MRR、nDCG，并兼容 SWE-bench 风格 oracle 文件。

## 当前状态

Agent-Memory 目前处于可运行的 MVP / early preview 阶段，适合：

- 本地代码库语义检索。
- 为 IDE Agent / CLI Agent 提供项目上下文。
- 导入项目文档、团队规范和经验记忆。
- 评估不同检索策略或 embedding/vector store 对召回质量的影响。
- 探索 CodeBuddy / Codex hooks 驱动的上下文闭环。

接口和数据格式仍可能演进，欢迎基于实际 Agent 工作流反馈问题和改进建议。

## 快速开始

先安装 Agent-Memory，再让 AI 助手自动使用它。请选择下面其中一种安装方式。

### 1. 通过 Python 安装脚本手工安装

这是大多数用户推荐使用的方式。安装脚本会从 GitHub Release 下载二进制文件，把 `code-context` / `code-context-mcp` 安装到安装目录的 `bin` 下并加入 `PATH`，同时把 `agent-memory-context` skill 和 hooks 安装到目标项目，并引导配置 embedding 与 Milvus。

```bash
git clone https://github.com/mazhan465/Agent-Memory.git
cd Agent-Memory

python3 .codebuddy/skills/agent-memory-context/install.py \
  --project-root /path/to/your/project \
  --milvus-mode lite \
  --embedding ollama \
  --yes
```

如果只想快速试用 CLI，不安装 hooks 和 Milvus：

```bash
python3 .codebuddy/skills/agent-memory-context/install.py \
  --project-root /path/to/your/project \
  --skip-hooks \
  --skip-milvus \
  --embedding hash \
  --yes
```

如果希望二进制文件本地运行、Milvus 和 Ollama 由 Docker 启动：

```bash
python3 .codebuddy/skills/agent-memory-context/install.py \
  --project-root /path/to/your/project \
  --milvus-mode docker \
  --embedding ollama \
  --ollama-mode docker \
  --ollama-accelerator auto \
  --ollama-model embeddinggemma \
  --yes
```

Linux NVIDIA 环境可使用 `--ollama-accelerator nvidia`，需要 NVIDIA Container Toolkit；Linux ROCm 环境可使用 `--ollama-accelerator amd-rocm`。macOS Docker 只能走 CPU，如需 Apple Silicon GPU 加速建议使用宿主机 Ollama。

常用安装参数：

- `--install-root <dir>`：指定二进制、`agent-memory.env` 和 Milvus 运行文件安装位置。
- `--version <tag|latest>`：指定下载哪个 GitHub Release。
- `--milvus-mode docker|lite|binary-guide`：选择 Docker Compose、本地 Milvus Lite 或 Linux 手工二进制安装指引。
- `--embedding hash|ollama|openai-compatible`：选择 embedding provider。
- `--install-ollama`：尝试安装 Ollama 并拉取配置的 embedding 模型。
- `--ollama-mode auto|host|docker|skip`：使用宿主机 Ollama、Docker Ollama 或跳过 Ollama 设置。
- `--ollama-accelerator auto|cpu|nvidia|amd-rocm`：选择 Ollama Docker 加速 profile。
- `--ollama-dimensions <n>`：可选 `/api/embed` dimensions 参数；`0` 表示使用模型默认维度。

基础要求：

- Git。
- Python 3（运行安装脚本、安装 skill/hooks、使用 Milvus Lite 时需要）。
- Docker / Docker Desktop（`--milvus-mode docker` 或 `--ollama-mode docker` 需要）。
- Ollama（仅 `--embedding ollama` 需要；如果宿主机或 Docker 安装流程已启动则无需手工安装）。
- OpenAI-compatible API Key（仅 `--embedding openai-compatible` 需要）。
- Go 1.25+（仅源码构建或开发时需要）。

如果需要从源码构建，再安装 Go 1.25+ 并执行：

```bash
make build

# 可选：构建 MCP Server
mkdir -p bin
go build -o bin/code-context-mcp ./cmd/code-context-mcp
```

### 2. AI 安装指导

如果希望 AI 编程助手代为安装 Agent-Memory，请把下面指令交给它：

1. 从 `https://github.com/mazhan465/Agent-Memory/releases` 下载匹配当前平台的 GitHub Release 压缩包。
2. 解压 `code-context` 和 `code-context-mcp`，放到 `PATH` 中的目录，例如 `~/.agent-memory/bin`；类 Unix 系统需要执行 `chmod +x`。
3. 验证 `code-context config path` 可运行。如果无法修改 `PATH`，为 hooks 或 Agent 命令设置 `AGENT_MEMORY_CODE_CONTEXT_BIN=/absolute/path/to/code-context`。
4. 把 `agent-memory-context` skill 安装到目标项目，并接入当前 AI 助手的 hook 系统。如果可以使用 Python 安装脚本，二进制已在 `PATH` 后可带 `--skip-binary` 执行安装脚本；否则复制 skill 目录并按目标助手适配 hook 模板。
5. 对支持 hooks 的助手，接入等价事件：
   - 会话启动：确保代码库已索引或已增量同步；
   - 用户 prompt：使用同一个 `session_id` 查询 `code` 和 `doc` 上下文；
   - 会话停止/结束：写入 `conversation`，提炼 `experience` / `preference` / `tool_history` / `fact`，再执行增量 `sync`。
6. 如果当前助手不支持 hooks，使用 MCP 配置、启动指令、包装命令或定时自动化作为替代。替代方案仍必须自动完成 `code` 查询、用户明确要求时的 `knowledge` 导入、`experience` / 会话记忆写入，以及最终 `sync`。

最小 hook 等价 CLI 流程：

```bash
code-context index /path/to/repo
code-context search /path/to/repo "<user task>" 8 code
code-context search /path/to/repo "<user task>" 8 doc --session-id=<session_id>
code-context import knowledge /path/to/docs project-docs   # 仅在用户明确要求时执行
code-context import memory experience /path/to/experience.jsonl project-experience
code-context sync /path/to/repo
```

### 3. 选择 Embedding 和向量存储

首次大规模索引前，请先选择合适的 embedding provider 与向量存储：

| 场景 | Embedding | 向量存储 | 依赖 |
| --- | --- | --- | --- |
| 离线快速试用 | `hash` | `local` | 除二进制外无额外依赖 |
| 本地私有开发 | `ollama` | `local` 或 Milvus Lite | Ollama 和所选模型；Milvus Lite 需要 Python 3 |
| 更大的本地或团队场景 | `openai-compatible` 或 `ollama` | Milvus Docker / 远端 Milvus | API Key 或 Ollama；Docker / Docker Desktop 或可访问的 Milvus 服务 |

Hash embedding 方便但语义质量有限。真实 Agent 工作流建议优先选择 Ollama 本地 embedding 或 OpenAI-compatible embedding API；当索引规模较大或需要多工具共享时，建议使用 Milvus。

Ollama 示例：

```bash
export AGENT_MEMORY_EMBEDDING_PROVIDER=ollama
export AGENT_MEMORY_OLLAMA_HOST=http://127.0.0.1:11434
export AGENT_MEMORY_OLLAMA_EMBEDDING_MODEL=embeddinggemma
# 如果所用 Ollama 模型支持 /api/embed dimensions，可按需开启。
export AGENT_MEMORY_OLLAMA_EMBEDDING_DIMENSIONS=384

ollama pull embeddinggemma
```

OpenAI-compatible 示例：

```bash
export AGENT_MEMORY_EMBEDDING_PROVIDER=openai-compatible
export AGENT_MEMORY_OPENAI_BASE_URL=https://api.openai.com/v1
export AGENT_MEMORY_OPENAI_API_KEY=your-api-key
export AGENT_MEMORY_OPENAI_EMBEDDING_MODEL=text-embedding-3-small
export AGENT_MEMORY_OPENAI_EMBEDDING_DIMENSIONS=1024
export AGENT_MEMORY_OPENAI_MAX_INPUT_LENGTH=8192
```

Milvus 示例：

```bash
export AGENT_MEMORY_VECTOR_STORE=milvus
export AGENT_MEMORY_MILVUS_ADDRESS=localhost:19530
export AGENT_MEMORY_MILVUS_COLLECTION=agent_memory_chunks
```

小规模本地非 Docker 环境可用 `--milvus-mode lite`，本地 standalone Milvus 可用 `--milvus-mode docker`，已有 Milvus 服务则配置 `AGENT_MEMORY_MILVUS_ADDRESS` 指向该服务。

### 4. 初始化配置

```bash
code-context config init
code-context config path
```

默认配置文件位于用户目录下的 `.AgentMemory/config.yaml`，也可以用 `AGENT_MEMORY_CONFIG` 指定其他路径。环境变量优先级高于配置文件。

### 5. 索引代码库

```bash
code-context index /path/to/repo
```

查看索引状态：

```bash
code-context status /path/to/repo
```

后续增量同步：

```bash
code-context sync /path/to/repo
# 或同步所有已索引路径
code-context sync --all
```

### 6. 搜索上下文

```bash
code-context search /path/to/repo "where is authentication handled"
```

指定返回数量和搜索类型：

```bash
code-context search /path/to/repo "Milvus vector store" 5 knowledge
```

复用 `session_id` 做会话级去重：

```bash
code-context search /path/to/repo "Milvus vector store" 5 knowledge --session-id=session-dev
```

可用搜索类型：

- `all`
- `code`
- `doc`：所有非 code 来源，包括 `knowledge`、`conversation`、`experience`、`preference`、`tool_history`、`fact`
- `knowledge`
- `conversation`
- `experience`
- `preference`
- `tool_history`
- `fact`

## CLI 命令

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

## 导入文档和长期记忆

导入 Markdown 文档知识库：

```bash
./bin/code-context import knowledge /path/to/docs project-docs
```

导入经验记忆：

```bash
cat > /tmp/agent-memory-experience.jsonl <<'EOF'
{"id":"go-log-rule","content":"Go 日志文案应描述在前、参数在后","experience_kind":"domain","domain_path":"programming/go","tags":["go","logging"]}
EOF

./bin/code-context import memory experience /tmp/agent-memory-experience.jsonl go-experience
```

查看 source：

```bash
./bin/code-context source list
./bin/code-context source list knowledge
```

清理 source：

```bash
./bin/code-context source clear knowledge project-docs
```

## Embedding 配置

### 默认 hash embedder

默认 `hash` provider 零依赖、可离线运行，适合快速验证流程，但语义效果较弱。

### OpenAI-compatible Embedding

```bash
export AGENT_MEMORY_EMBEDDING_PROVIDER=openai-compatible
export AGENT_MEMORY_OPENAI_BASE_URL=https://api.openai.com/v1
export AGENT_MEMORY_OPENAI_API_KEY=your-api-key
export AGENT_MEMORY_OPENAI_EMBEDDING_MODEL=text-embedding-3-small
export AGENT_MEMORY_OPENAI_EMBEDDING_DIMENSIONS=1024
export AGENT_MEMORY_OPENAI_MAX_BATCH_SIZE=10
export AGENT_MEMORY_OPENAI_MAX_INPUT_LENGTH=8192

./bin/code-context index /path/to/repo
```

### Ollama 本地 Embedding

```bash
export AGENT_MEMORY_EMBEDDING_PROVIDER=ollama
export AGENT_MEMORY_OLLAMA_HOST=http://127.0.0.1:11434
export AGENT_MEMORY_OLLAMA_EMBEDDING_MODEL=embeddinggemma
# 如果所用 Ollama 模型支持 /api/embed dimensions，可按需开启。
export AGENT_MEMORY_OLLAMA_EMBEDDING_DIMENSIONS=384

ollama pull embeddinggemma
./bin/code-context index /path/to/repo
```

## Vector Store 配置

### 本地 JSON 存储

默认 `local` vector store 会把索引数据写入本地目录，适合开发和单机试用。

### Milvus

```bash
export AGENT_MEMORY_VECTOR_STORE=milvus
export AGENT_MEMORY_MILVUS_ADDRESS=localhost:19530
export AGENT_MEMORY_MILVUS_COLLECTION=agent_memory_chunks

./bin/code-context index /path/to/repo
```

如果使用内置 skill 安装脚本，可以选择 Milvus 和 Ollama 运行模式：

```bash
# Docker 依赖模式：本地二进制 + Docker Milvus + Docker Ollama。
python3 .codebuddy/skills/agent-memory-context/install.py \
  --project-root . \
  --milvus-mode docker \
  --embedding ollama \
  --ollama-mode docker \
  --ollama-accelerator auto

# 本地非 Docker Milvus 模式，使用 Milvus Lite server 监听 localhost:19530。
python3 .codebuddy/skills/agent-memory-context/install.py \
  --project-root . \
  --milvus-mode lite \
  --embedding ollama
```

`--milvus-mode docker` 和 `--ollama-mode docker` 需要 Docker / Docker Desktop。Ollama Docker 支持 `cpu`、`nvidia`、`amd-rocm` 三类加速 profile；NVIDIA 需要 NVIDIA Container Toolkit，AMD ROCm 需要 Linux `/dev/kfd` 和 `/dev/dri`，macOS Docker 会使用 CPU。`--milvus-mode lite` 会在安装目录创建 Python venv 并安装 `pymilvus[milvus-lite]`，适合 macOS、Windows 和 Linux 的本地小规模开发验证。

## MCP Server

Agent-Memory 提供 stdio MCP Server：

```bash
./bin/code-context-mcp
```

当前 MCP tools：

- `index_codebase`：索引或增量刷新本地代码库路径。
- `search_code`：在已索引代码库中搜索代码片段。
- `clear_index`：清理指定代码库的向量数据和 snapshot。
- `get_indexing_status`：查询指定代码库索引状态。

示例 MCP 配置片段：

```json
{
  "mcpServers": {
    "agent-memory": {
      "command": "/absolute/path/to/code-context-mcp"
    }
  }
}
```

不同 MCP Client 的配置格式略有差异，请按对应客户端文档调整。

## CodeBuddy / Codex Agent 闭环

项目内置 `agent-memory-context` skill，可为 CodeBuddy 和 Codex 安装 hooks：

```bash
python3 .codebuddy/skills/agent-memory-context/install.py --project-root .

# 无 Docker 本地开发可选择 Milvus Lite
python3 .codebuddy/skills/agent-memory-context/install.py --project-root . --milvus-mode lite
```

安装后会把相关模板写入目标项目：

- `.codebuddy/settings.json`
- `.codebuddy/rules/agent-memory-context.md`
- `.codex/config.toml`
- `AGENTS.md`

运行时闭环：

1. `SessionStart`：检查索引，必要时初始化或增量同步。
2. `UserPromptSubmit`：根据用户 prompt 搜索 Agent-Memory，并注入上下文。
3. `Stop` / `SessionEnd`：任务结束时执行 `sync`，确保下一轮拿到最新内容。

更多说明见 `docs/agent_memory_skill_integration.md`。

## 召回质量评估

创建评估集：

```bash
cat > /tmp/agent-memory-recall-eval.jsonl <<'EOF'
{"id":"auth","query":"authenticate user token","expected":[{"relative_path":"auth.go"}]}
EOF
```

运行评估：

```bash
./bin/code-context eval recall /path/to/repo /tmp/agent-memory-recall-eval.jsonl 10 code
```

也支持部分 SWE-bench / claude-context 风格字段，例如 `instances`、`problem_statement`、`patch`、`oracles`、`oracle_files`。

评估结果会输出召回质量指标和运行效率指标：质量指标包括 hit rate、precision、recall、F1、MRR、nDCG、文件级 precision/recall/F1；效率指标包括单 case 与汇总 latency、估算结果 token、结果字符数、结果数量、去重数量和搜索 namespace 数。

## 常用环境变量

| 变量 | 说明 |
| --- | --- |
| `AGENT_MEMORY_CONFIG` | 指定配置文件路径 |
| `AGENT_MEMORY_HOME` | 指定本地存储目录 |
| `AGENT_MEMORY_EMBEDDING_PROVIDER` | `hash` / `openai` / `openai-compatible` / `ollama` |
| `AGENT_MEMORY_OPENAI_BASE_URL` | OpenAI-compatible API 地址 |
| `AGENT_MEMORY_OPENAI_API_KEY` | OpenAI-compatible API Key |
| `AGENT_MEMORY_OPENAI_EMBEDDING_MODEL` | OpenAI-compatible embedding 模型 |
| `AGENT_MEMORY_OPENAI_EMBEDDING_DIMENSIONS` | OpenAI-compatible 输出向量维度，默认 `1024` |
| `AGENT_MEMORY_OPENAI_MAX_BATCH_SIZE` | OpenAI-compatible 单次批量请求条数，默认 `10` |
| `AGENT_MEMORY_OPENAI_MAX_INPUT_LENGTH` | OpenAI-compatible 单条输入文本长度限制，默认 `8192` |
| `AGENT_MEMORY_OLLAMA_HOST` | Ollama 地址 |
| `AGENT_MEMORY_OLLAMA_EMBEDDING_MODEL` | Ollama embedding 模型 |
| `AGENT_MEMORY_OLLAMA_EMBEDDING_DIMENSIONS` | 可选 Ollama `/api/embed` 输出维度；`0` 或未设置表示使用模型默认维度 |
| `AGENT_MEMORY_VECTOR_STORE` | `local` / `milvus` |
| `AGENT_MEMORY_MILVUS_ADDRESS` | Milvus 地址 |
| `AGENT_MEMORY_MILVUS_COLLECTION` | Milvus collection 名称 |
| `AGENT_MEMORY_DEFAULT_SEARCH_TYPES` | 默认搜索类型 |
| `AGENT_MEMORY_CUSTOM_EXTENSIONS` | 追加扫描扩展名，如 `.vue,.svelte` |
| `AGENT_MEMORY_CUSTOM_IGNORE_PATTERNS` | 追加忽略 glob，如 `private/**,*.backup` |

## 项目结构

```text
cmd/
  code-context/      # CLI 入口
  code-context-mcp/  # MCP stdio Server 入口
internal/
  catalog/           # Source catalog
  config/            # 配置加载和默认配置生成
  contextdoc/        # 文档节点与知识类型模型
  embed/             # Hash / OpenAI-compatible / Ollama embedder
  indexer/           # 全量与增量索引流程
  mcpserver/         # MCP stdio 协议与工具实现
  scanner/           # 文件扫描和 ignore 规则
  searcher/          # 向量与混合检索
  snapshot/          # 索引快照
  splitter/          # tree-sitter 与行级切块
  vectorstore/       # Local / Milvus vector store
.codebuddy/skills/
  agent-memory-context/ # CodeBuddy / Codex skill、hooks 和安装器
```

## 开发

```bash
# 格式化
make fmt

# 测试
make test

# 构建 CLI
make build

# 构建 MCP Server
mkdir -p bin
go build -o bin/code-context-mcp ./cmd/code-context-mcp
```

项目开发规则见 `docs/development_rules.md`。

## Roadmap

- 更完整的 MCP tools：统一搜索代码、文档、经验、偏好和工具历史。
- 更强的会话上下文构建：面向 prompt 自动生成结构化 context pack。
- Source bundle 导出、导入、备份和跨设备同步。
- 更多语言的 tree-sitter 结构切块。
- 更完善的安装包、Release 二进制和平台兼容测试。
- 参考 claude-context 增强代码搜索核心质量：补齐 BM25 / sparse vector、dense vector、RRF rerank 或可插拔 reranker 等能力。
- 参考 claude-context 增强 Milvus 搜索：让 Milvus 后端支持真正的 hybrid search，并与本地搜索的关键词、路径、符号元数据融合策略保持一致。
- 参考 claude-context 完善评估体系：补充 token、成本、延迟、工具调用次数、索引耗时、Agent 任务成功率等 benchmark，和现有 recall / precision / F1 / MRR / nDCG 指标形成闭环。

## 文档

- `README.md`：默认英文 README。
- `README.zh-CN.md`：中文 README。
- `docs/design/overall_design.md`：整体设计。
- `docs/modules/module_design.md`：模块细化设计。
- `docs/development_rules.md`：项目开发规则。
- `docs/development_log.md`：开发过程记录。
- `docs/agent_memory_skill_integration.md`：Agent-Memory skill、CodeBuddy hooks 与 Codex hooks 集成说明。

## License

本项目基于 [GNU General Public License v3.0](LICENSE) 开源。

详见 [LICENSE](LICENSE) 文件。
