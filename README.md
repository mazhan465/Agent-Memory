# Agent-Memory

`Agent-Memory` 是一个使用 Go 实现的语义上下文索引与检索项目。早期目标能力参考 `claude-context`：扫描代码库、切分代码片段、生成向量、存储到向量存储层，并通过 CLI / MCP 工具为大模型提供代码上下文检索能力。长期目标是扩展为面向 AI 编程助手、IDE Agent 和自动化工具链的上下文记忆与知识供给底座，支持代码库、历史会话、工具记录、用户偏好、经验沉淀、书籍、技术文档、SDK 文档和项目规范等多类数据源。

项目最终目标是在每次对话开始或每次用户发送提示词时，自动并行搜索代码库、长期记忆、工具历史和文档知识库，一次性为 AI 提供足量、准确、可追溯的上下文记忆以及需要参考的知识。

## 当前阶段

当前版本先实现可运行的 MVP：

- 本地代码库扫描
- 通用行级代码切块
- 文档解析器抽象、Markdown 解析器和通用文档节点切块器，解析标题、摘要、正文，并保留 `document_id`、`section_id`、`heading_path`、`knowledge_kind` 和 `node_kind`
- Markdown 文档源读取能力，可读取 `.md` / `.markdown` 文件并解析为统一文档节点
- 文档知识 CLI 闭环：`import knowledge` 可导入 Markdown 文档 source，普通 `search` 默认统一检索代码、知识、历史会话、经验和用户偏好等 source，并返回 JSON
- 哈希向量 Embedder（默认本地可运行，便于验证流程）
- OpenAI-compatible Embedder（通过环境变量启用真实语义向量）
- 本地 JSON 向量存储（便于无 Milvus 环境下开发测试）
- CLI：`index`、`search`、`import knowledge`、`import memory`、`source list`、`source clear`、`clear`、`status`
- 模块化接口：后续可替换为 Ollama Embedder 和 Milvus VectorStore
- 长期记忆设计：后续支持历史会话、工具记录、用户偏好和会话启动上下文构建
- 文档知识库设计：后续支持书籍、超长说明文档、技术文档、SDK 文档和项目规范入库
- 经验维度设计：区分通用经验和领域专业经验，领域匹配时提高专业经验检索详细度
- 提示词级上下文构建：后续并行搜索代码库、长期记忆、工具历史和文档知识库，并一次返回统一上下文包
- Source 分片设计：后续支持按 source 导出、导入、备份和多端同步
- 兼容外部记忆源：后续可将 Basic Memory 的 Markdown 项目作为一种外部 source 导入

## 项目铁则

详见 `docs/development_rules.md`。核心要求：

1. 所有开发过程必须落到文档中。
2. 每完成一块完整需求，自动提交一次 Git 记录。
3. 所有 Go 文件头部必须有文件说明，包含：简要说明、实现原理、如何使用、注意事项、交互模块。

## 快速使用

```bash
# 编译
make build

# 索引当前项目；会自动读取根目录 .gitignore / .contextignore 等 .xxxignore 文件
./bin/code-context index /path/to/repo

# 追加自定义扩展名和忽略规则
export AGENT_MEMORY_CUSTOM_EXTENSIONS=.vue,.svelte,.astro
export AGENT_MEMORY_CUSTOM_IGNORE_PATTERNS='private/**,*.backup'

# 统一搜索代码、知识、历史会话、经验和用户偏好等，返回 JSON；未传 session id 时会自动生成并返回
./bin/code-context search /path/to/repo "where is authentication handled"

# 只搜索知识文档，并复用 session id 做会话级去重
./bin/code-context search /path/to/repo "Milvus vector store" 5 knowledge session-dev
# 或使用显式参数
./bin/code-context search /path/to/repo "Milvus vector store" 5 knowledge --session-id=session-dev

# 查看状态
./bin/code-context status /path/to/repo

# 导入 Markdown 文档知识 source
./bin/code-context import knowledge /path/to/docs project-docs

# 导入 JSONL 长期记忆 source，content 为必填字段
cat > /tmp/agent-memory-experience.jsonl <<'EOF'
{"id":"go-log-rule","content":"Go 日志文案应描述在前、参数在后","experience_kind":"domain","domain_path":"programming/go","tags":["go","logging"]}
EOF
./bin/code-context import memory experience /tmp/agent-memory-experience.jsonl go-experience

# 使用 OpenAI-compatible embedding
export AGENT_MEMORY_EMBEDDING_PROVIDER=openai
export AGENT_MEMORY_OPENAI_BASE_URL=https://api.openai.com/v1
export AGENT_MEMORY_OPENAI_API_KEY=your-api-key
export AGENT_MEMORY_OPENAI_EMBEDDING_MODEL=text-embedding-3-small
./bin/code-context index /path/to/repo

# 使用 Milvus VectorStore
export AGENT_MEMORY_VECTOR_STORE=milvus
export AGENT_MEMORY_MILVUS_ADDRESS=localhost:19530
export AGENT_MEMORY_MILVUS_COLLECTION=agent_memory_chunks
./bin/code-context index /path/to/repo
./bin/code-context search /path/to/repo "where is authentication handled"

# 清理索引
./bin/code-context clear /path/to/repo
```

## 文档

- `docs/design/overall_design.md`：整体设计
- `docs/modules/module_design.md`：模块细化设计
- `docs/development_rules.md`：开发铁则
- `docs/development_log.md`：开发过程记录
