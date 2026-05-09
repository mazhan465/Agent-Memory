# 模块细化设计

## 1. `internal/config`

### 职责

负责项目配置读取和默认值管理。

### 输入

- 环境变量
- CLI 参数

### 输出

- `Config` 结构体

### 后续扩展

支持 YAML / TOML 配置文件。

## 2. `internal/scanner`

### 职责

扫描代码库中的可索引文件。

### 规则

- 默认忽略 `.git`、`node_modules`、`dist`、`build`、`.env`、日志和缓存目录。
- 只扫描受支持的扩展名，默认覆盖 Go、TypeScript、JavaScript、Python、Java、C/C++、C#、Rust、PHP、Ruby、Swift、Kotlin、Scala、Objective-C、Dart、Solidity、Markdown 和 Notebook。
- 支持通过 `AGENT_MEMORY_CUSTOM_EXTENSIONS` 追加扩展名。
- 支持默认 glob 忽略规则和 `AGENT_MEMORY_CUSTOM_IGNORE_PATTERNS` 追加规则。
- 支持读取代码库根目录下 `.gitignore`、`.contextignore`、`.cursorignore` 等 `.xxxignore` 文件。
- 返回绝对路径和相对路径，并按相对路径稳定排序。

## 3. `internal/splitter`

### 职责

把源文件切分成适合 embedding 的代码片段。

### 当前实现

- `LineSplitter`：行级切块，支持 `MaxLines` 和 `OverlapLines`，保留起止行号。
- `TreeSitterSplitter`：统一 tree-sitter 多语言语法切块器，当前支持 Go 和 C++，解析失败或未配置语言时回退到 `LineSplitter`。
- `RangeChunker`：将 tree-sitter 语法节点行区间转换为 chunk，并写入 `chunk_kind`、`symbol_name`、`symbol_kind` 和 `parser` 元数据。
- `MarkdownSplitter`：作为 `document.MarkdownParser` 的适配层，将解析出的标题、摘要和正文节点转换为可向量化 chunk。
- Markdown chunk 会携带 `document_id`、`section_id`、`heading_path`、`knowledge_kind` 和 `node_kind`。
- Markdown body chunk 会尽量保持 fenced code block 和表格完整，避免将同一语义单元拆散。

### 后续扩展

- 继续扩展 tree-sitter language config，支持 JavaScript、TypeScript、Python、Rust 等语言。
- 继续细化 C++ 大 namespace / class 的子节点切分策略。
- 文档 chunk 后续继续补充 `version`、`keywords` 和 `symbols` 自动抽取。
- `DocumentChunker` 已接入 CLI 文档知识导入流程，后续继续沉淀为独立 source shard 索引服务。

## 4. `internal/embed`

### 职责

将文本转换为向量。

### 当前实现

- `HashEmbedder`：本地哈希向量，用于验证完整流程。
- `OpenAIEmbedder`：通过 OpenAI-compatible embeddings HTTP API 生成真实语义向量。

### 后续扩展

- Ollama embedding。
- Gemini / VoyageAI embedding。

## 5. `internal/vectorstore`

### 职责

抽象向量存储能力。

### 当前实现

- `LocalStore`：JSON 文件持久化，余弦相似度 TopK 检索，并支持 `ReplaceFiles` 按文件替换 chunk。
- `MilvusStore`：使用 Milvus collection 持久化向量，通过 `namespace` 字段隔离不同代码库，并支持按 `relative_path` 删除旧 chunk 后增量写入。

### 后续扩展

- 支持通用 `ContextDocument` 字段。
- 支持 `scope_type`、`scope_id`、`source_type`、`source_id` 等过滤条件。
- 支持 `document_id`、`section_id`、`heading_path`、`knowledge_kind`、`version`、`domain_path` 等文档知识过滤条件。
- 本地存储支持 source shard，一个 source 一个向量文件。
- 支持对多个 source shard 聚合检索并全局重排。
- 支持代码库、长期记忆、工具历史和文档知识库并行检索后统一合并。
- 支持 Milvus hybrid vector store，后续将 dense vector 和 BM25 sparse vector 下沉到 Milvus collection。
- 支持结果重排需要的元数据返回。

## 6. `internal/contextdoc`

### 职责

定义通用上下文文档模型，把代码片段、历史会话、工具记录、用户偏好、经验总结和文档知识统一为可索引文档。

### 核心模型

- `ContextDocument`：统一文档结构。
- `Scope`：描述用户级、工作区级、代码库级或工具级作用域。
- `Source`：描述文档来源，如代码库、会话、工具历史、偏好、经验、事实、文档知识。
- `ExperienceKind`：描述经验类型，如通用经验、领域专业经验、项目经验、工具经验。
- `Domain`：描述动态树形专业领域，如 `programming/go`、`database/milvus`。
- 文档知识字段：`document_id`、`section_id`、`heading_path`、`knowledge_kind`、`version`、`keywords`、`symbols`。

### 与现有模块关系

当前 `vectorstore.Document` 是代码库场景的早期模型，后续应逐步迁移或兼容 `ContextDocument`。

## 7. `internal/source`

### 职责

抽象不同数据源的读取能力。

### 计划实现

- `CodebaseSource`：复用现有 `scanner` 扫描本地代码库。
- `ConversationSource`：读取 openclaw 等工具的历史会话。
- `ToolHistorySource`：读取工具调用历史、执行结果和产物。
- `ManualMemorySource`：读取人工维护的偏好、经验和项目事实。
- `DocumentSource`：读取书籍、超长说明文档、技术文档、SDK 文档和项目规范。
- `MarkdownDocumentSource`：当前已支持读取 `.md` / `.markdown` 文件或目录，并调用 `MarkdownParser` 输出统一文档节点。
- `BasicMemorySource`：读取 Basic Memory Markdown 项目，将 frontmatter、Observations、Relations 和正文转换为外部 source。

### 输出

- 统一的 `SourceItem` / `DocumentItem` 列表，供后续切分、提取和向量化。

## 8. `internal/indexer`

### 职责

编排索引流程。当前以代码库索引为主，后续扩展为通用 `ContextIndexer`。

### 当前交互模块

- `scanner`
- `splitter`
- `embed`
- `vectorstore`
- `snapshot`

### 后续交互模块

- `source`
- `contextdoc`
- `domain`
- `memory`
- `document`
- `embed`
- `vectorstore`
- `snapshot`

## 9. `internal/searcher`

### 职责

编排搜索流程。当前按代码库路径搜索，后续扩展为按 `Scope`、`SourceType`、`ExperienceKind` 和 `Domain` 搜索。

### 当前交互模块

- `embed`
- `vectorstore`
- `snapshot`

### 后续扩展

- `SearchCodebase`：搜索代码库上下文。
- `SearchMemory`：搜索用户全局长期记忆。
- `SearchToolHistory`：搜索指定工具历史。
- `SearchDocumentKnowledge`：搜索文档知识库，支持章节路径、知识类型和版本过滤。
- `SearchContext`：按通用过滤条件搜索所有上下文来源。
- `SearchPromptContext`：根据提示词生成检索计划，并行搜索代码库、长期记忆、工具历史和文档知识库。
- `SearchDomainExperience`：在识别到专业领域时提高该领域经验的召回数量和 token budget。
- `SearchOptions` 当前已支持 `DocumentFilters`、`SectionFilters`、`HeadingFilters`、`KnowledgeKindFilters`、`NodeKindFilters` 和 `VersionFilters`。
- `SearchOptions` 后续需要继续支持 `SourceFilters`、`RequiredKeywords` 和 fallback 策略。

## 10. `internal/domain`

### 职责

识别任务所属领域，并为经验记忆标注通用经验或领域专业经验。

### 当前能力

- `DefaultClassifier`：基于规则的轻量分类器。
- `RuleSignal`：将规则分类器降级为领域判断信号源。
- `DomainProfile`：保存领域路径、描述、别名、典型问题和可选画像向量。
- `StaticProfileStore`：内存领域画像存储。
- `ProfileSignal`：基于领域画像文本匹配生成领域证据，后续可升级为 embedding 匹配。
- `MemoryDistributionSignal`：根据普通向量预检索 TopN 记忆的领域分布反推领域。
- `DomainResolver`：融合显式上下文、规则、领域画像和记忆分布多个信号，输出领域决策。

### 计划能力

- 输出动态 `domain_path`、`domain_confidence`、`experience_kind` 和 evidence。
- 当前支持两级领域路径，如 `programming/go`、`database/milvus`、`infrastructure/kubernetes`。
- 维护领域别名和关键词表，如 Go/golang、Milvus/vector database、Kubernetes/k8s。
- 支持父子领域匹配，二级领域命中时可同时召回一级领域经验。
- 将 `ProfileSignal` 从文本匹配升级为 query embedding 与 domain profile embedding 语义匹配。
- 低置信或多个候选领域冲突时，可选调用 LLM 做结构化领域判断。
- 为搜索器提供专业经验详细度建议，如 TopK 和 token budget 调整。

## 11. `internal/memory`

### 职责

从历史会话和工具记录中提取长期记忆。

### 计划能力

- 提取用户稳定偏好。
- 提取通用经验。
- 提取领域专业经验。
- 提取历史问题解决经验。
- 提取项目事实。
- 提取架构决策和约束。
- 将提取结果转换为带 `experience_kind` 和 `domain` 的 `ContextDocument`。

## 12. `internal/document`

### 职责

负责文档知识库的结构化处理，将书籍、超长说明文档、技术文档、SDK 文档和项目规范转换为可检索、可追溯的知识节点。

### 当前能力

- `Parser`：定义多格式文档解析器接口，后续 Markdown、PDF、DOCX 等格式都应实现该接口。
- `Node`：统一表示解析后的标题、摘要和正文节点。
- `MarkdownParser`：解析 Markdown 标题层级，忽略 fenced code block 内的伪标题。
- Markdown 解析节点会标注 `document_id`、`section_id`、`heading_path`、`knowledge_kind` 和 `node_kind`。
- Markdown 解析器可识别 `>`、`Summary:`、`Abstract:`、`摘要:` 等摘要节点。
- `DocumentChunker`：当前已支持将解析后的 title / summary / body 节点统一转换为可向量化 chunk。

### 计划能力

- 实现 `PDFParser`、`DOCXParser` 等格式解析器。
- 生成 `document_summary`、`chapter_summary`、`section_summary` 和 `chunk` 多层节点。
- 提取 `keywords` 和 `symbols`，用于 API 名、函数名、配置项、错误码等精确召回。
- 保留 `version` 和来源引用。
- 命中 chunk 后支持回补父级章节摘要、相邻 chunk 和引用路径。

## 13. `internal/contextassembler`

### 职责

根据用户提示词和会话状态，将代码库、长期记忆、工具历史和文档知识库的检索结果合并为一次返回给 AI 的统一上下文包。

### 输入

- 当前工作区、当前文件、工具名和用户提示词。
- `DomainResolver` 的领域决策和 evidence。
- `Searcher` 返回的多来源检索结果。
- token budget、来源优先级和过滤条件。

### 输出

- 统一提示词上下文包。
- 当前任务判断、领域路径和检索计划摘要。
- 代码上下文、用户偏好、项目事实、历史经验和文档知识。
- 冲突或版本差异说明。
- 来源引用、排序信息和压缩后的内容。

### 组织规则

1. 用户和项目强规则优先。
2. 当前代码上下文优先于普通历史经验。
3. 领域专业经验优先于通用经验。
4. 官方新版本文档优先于普通知识文档。
5. 文档知识命中 chunk 时，需要尽量补充父级 section 摘要和 `heading_path`。
6. 文档知识库与历史记忆并行搜索，并在同一个上下文包中一次返回。

## 14. `internal/sessioncontext`

### 职责

在每次会话开始和每次用户提示词到达时自动构建上下文包。

### 输入

- 当前工作区。
- 当前文件或任务描述。
- 当前用户输入。
- 当前工具名，如 openclaw。
- `DomainResolver` 的领域识别结果、evidence 和专业经验详细度。
- token budget 和过滤条件。
- 文档知识库候选 source、document、章节和知识类型过滤条件。

### 输出

- 代码相关上下文。
- 用户偏好。
- 通用经验。
- 领域专业经验。
- 文档知识，如规则、API、示例、排障说明和章节摘要。
- 项目事实。
- 来源引用、经验类型、领域、文档版本和排序信息。

## 15. `internal/snapshot`

### 职责

保存和读取索引状态。

### MVP 存储

- 默认目录：`~/.agent-memory/snapshots`
- 每个代码库一个 JSON 文件

### 后续扩展

- 按 `scope_type`、`scope_id`、`source_type`、`source_id` 记录索引状态。
- 支持长期记忆和工具历史的数据源状态。

## 16. `internal/catalog`

### 职责

记录已接入和已索引的数据源，用于增量索引、自动检索、导出导入和状态展示。

### 计划字段

- `source_type`
- `source_id`
- `scope_type`
- `scope_id`
- `tool_name`
- `version`
- `checksum`
- `indexed_at`
- `status`
- `document_count`
- `memory_count`
- `knowledge_count`
- `domain_paths`
- `document_ids`
- `versions`

## 17. `internal/sourcestore`

### 职责

管理 source 分片文件、source bundle 导出导入和多端同步。

### 计划能力

- `PutSource`：写入一个 source shard。
- `SearchSources`：聚合检索多个 source shard。
- `ExportSource`：导出单个 source bundle。
- `ImportSource`：导入 source bundle。
- `ListSources`：按 scope、source type、tool name、domain 过滤 source。
- `DeleteSource`：删除单个 source shard。
- `SyncSources`：基于 catalog、version 和 checksum 同步多端 source。

## 18. `internal/mcpserver`

### 职责

后续提供 MCP Server 封装。

### 计划工具

- `index_codebase`
- `search_code`
- `clear_index`
- `get_indexing_status`
- `index_conversation`
- `index_tool_history`
- `index_document_source`
- `search_agent_memory`
- `search_document_knowledge`
- `search_domain_experience`
- `search_prompt_context`
- `extract_memory`
- `get_user_preferences`
- `build_session_context`
- `clear_memory`
- `list_memory_sources`
- `export_source`
- `import_source`
- `sync_sources`

## 19. `cmd/code-context`

### 职责

提供 CLI 入口。

### 当前命令

- `index <path>`
- `search <path> <query> [limit] [types] [session-id]`：统一搜索入口，默认返回全部类型 JSON 结果；`types` 可指定 `code`、`knowledge`、`conversation`、`experience`、`preference`、`tool_history`、`fact`；未传 `session-id` 时自动生成，返回前按会话历史去重
- `import knowledge <path> [source-id]`
- `import memory <type> <json-or-jsonl-path> [source-id]`：导入 `conversation`、`experience`、`preference`、`tool_history`、`fact` 等长期记忆 source
- `source list [type]`：以 JSON 查看已导入 source
- `source clear <type> <source-id>`：清理指定 source 的向量和 catalog 记录
- `status <path>`
- `clear <path>`

### 后续命令

- `memory extract <conversation>`
- `memory domain-search <query>`
- `prompt-context build <query>`
- `session-context build <query>`
- `source export <source-id>`
- `source import <bundle>`
- `source sync <target>`
