# 开发过程记录

## 2026-05-07 初始化设计

### 目标

创建 `Agent-Memory` 项目，完成整体设计和模块设计，为后续 Go 实现代码库语义检索能力打基础。

### 已完成

- 创建项目目录。
- 初始化 Go Module。
- 初始化 Git 仓库。
- 编写项目铁则文档。
- 编写整体设计文档。
- 编写模块细化设计文档。
- 编写 README。

### Git 记录

- `docs: 初始化项目设计文档`

## 2026-05-07 MVP 基础能力实现

### 目标

实现可运行的 CLI MVP，先用本地 HashEmbedder 和 JSON VectorStore 打通代码库索引与搜索闭环。

### 已完成

- `internal/config`：默认配置和环境变量读取。
- `internal/scanner`：代码文件扫描和 ignore 过滤。
- `internal/splitter`：行级代码切块，保留起止行号。
- `internal/embed`：本地哈希向量化实现。
- `internal/vectorstore`：本地 JSON 向量存储和余弦相似度检索。
- `internal/snapshot`：索引状态快照。
- `internal/indexer`：索引流程编排。
- `internal/searcher`：搜索流程编排。
- `internal/mcpserver`：MCP 工具边界占位。
- `cmd/code-context`：CLI 命令入口。
- `cmd/code-context-mcp`：MCP 命令入口占位。

### 验证方式

- `gofmt -w ./cmd ./internal`
- `go test ./...`
- `go run ./cmd/code-context index .`
- `go run ./cmd/code-context search . "vector store" 3`

### 后续计划

- 接入真实 OpenAI-compatible Embedder。
- 接入 Milvus VectorStore。
- 实现 MCP stdio server。
- 实现增量索引。

## 2026-05-07 OpenAI-compatible Embedder 接入

### 目标

接入真实 OpenAI-compatible embeddings API，让 CLI 在默认本地 HashEmbedder 之外，可以通过环境变量切换到真实语义向量。

### 方案

- `internal/config` 增加 embedding provider、OpenAI Base URL、API Key 和模型名配置。
- `internal/embed` 新增 `OpenAIEmbedder`，通过 HTTP POST 调用 `/embeddings` 接口并保持批量返回顺序。
- `cmd/code-context` 根据 `AGENT_MEMORY_EMBEDDING_PROVIDER` 选择 Hash 或 OpenAI-compatible Embedder。
- README 和设计文档同步更新使用方式与模块状态。

### 模块影响

- `internal/config`：新增 OpenAI-compatible embedding 环境变量读取。
- `internal/embed`：新增真实 embedding provider 实现与测试。
- `cmd/code-context`：CLI 初始化逻辑支持 provider 切换。
- `docs`、`README.md`：更新当前能力和后续计划。

### 验证方式

- `gofmt -w ./cmd ./internal`
- `go test ./...`
- `make build`

### 后续计划

- 接入 Milvus VectorStore。
- 实现 MCP stdio server。
- 实现增量索引。

## 2026-05-07 Milvus VectorStore 接入

### 目标

接入 Milvus Go SDK，让 CLI 可以在本地 JSON 存储之外，通过环境变量切换到远端 Milvus 向量数据库。

### 方案

- `internal/config` 增加 vector store provider、Milvus 地址、认证信息和 collection 名称配置。
- `internal/vectorstore` 新增 `MilvusStore`，使用单个 collection 存储所有代码库 chunk。
- `MilvusStore` 通过 `namespace` 字段隔离代码库索引，支持覆盖写入、TopK 检索、清理和计数。
- 清理 Milvus namespace 前确保 collection 已加载，避免 Delete 阶段出现 collection not loaded。
- 搜索 Milvus 前先确认当前过滤条件下存在数据，避免空结果集触发 SDK 字段类型不匹配。
- `cmd/code-context` 根据 `AGENT_MEMORY_VECTOR_STORE` 选择 `LocalStore` 或 `MilvusStore`。
- README 和设计文档同步更新使用方式与模块状态。

### 模块影响

- `internal/config`：新增 Milvus VectorStore 环境变量读取。
- `internal/vectorstore`：新增 Milvus dense vector store 实现与 helper 测试。
- `cmd/code-context`：CLI 初始化逻辑支持 vector store provider 切换。
- `go.mod`、`go.sum`：新增 Milvus Go SDK 依赖。
- `docs`、`README.md`：更新当前能力和后续计划。

### 验证方式

- `gofmt -w ./cmd ./internal`
- `go mod tidy`
- `go test ./...`
- `make build`
- `AGENT_MEMORY_HOME=/Users/aaq/Desktop/project/Agent-Memory/bin/index-test ./bin/code-context index .`
- `AGENT_MEMORY_HOME=/Users/aaq/Desktop/project/Agent-Memory/bin/index-test ./bin/code-context search . "Milvus vector store" 3`
- `AGENT_MEMORY_VECTOR_STORE=milvus AGENT_MEMORY_MILVUS_ADDRESS=21.91.243.205:19530 AGENT_MEMORY_MILVUS_COLLECTION=agent_memory_test ./bin/code-context index .`
- `AGENT_MEMORY_VECTOR_STORE=milvus AGENT_MEMORY_MILVUS_ADDRESS=21.91.243.205:19530 AGENT_MEMORY_MILVUS_COLLECTION=agent_memory_test ./bin/code-context search . "Milvus vector store" 3`
- `AGENT_MEMORY_VECTOR_STORE=milvus AGENT_MEMORY_MILVUS_ADDRESS=21.91.243.205:19530 AGENT_MEMORY_MILVUS_COLLECTION=agent_memory_test ./bin/code-context clear .`
- `AGENT_MEMORY_VECTOR_STORE=milvus AGENT_MEMORY_MILVUS_ADDRESS=21.91.243.205:19530 AGENT_MEMORY_MILVUS_COLLECTION=agent_memory_test ./bin/code-context search . "Milvus vector store" 3`，清理后返回 `no results`。

### 后续计划

- 实现 MCP stdio server。
- 实现增量索引。
- 实现 dense + BM25 hybrid search。

## 2026-05-08 长期记忆架构设计

### 目标

在代码库索引能力之外，将项目定位扩展为通用语义上下文系统，后续支持 openclaw 等工具的历史会话、用户全局长期记忆、偏好和经验沉淀，并能在每次会话开始时自动检索相关历史上下文。

### 方案

- 将代码库从唯一核心对象调整为一种 `source_type=codebase` 数据源。
- 引入通用 `ContextDocument` 设计，兼容代码片段、历史会话、工具记录、偏好、经验和项目事实。
- 引入 `Scope` 和通用 namespace 设计，支持用户级、工作区级、代码库级和工具级长期记忆隔离。
- 规划 `SourceReader`、`MemoryExtractor`、`SourceCatalog` 和 `SessionContextBuilder`。
- MCP 工具规划从代码搜索扩展到历史会话导入、记忆搜索和会话启动上下文构建。
- 当前阶段暂不考虑安全和隐私策略，后续作为独立模块补齐。

### 模块影响

- `docs/design/overall_design.md`：补充通用上下文、长期记忆、会话启动上下文构建和存储模型。
- `docs/modules/module_design.md`：新增 `contextdoc`、`source`、`memory`、`sessioncontext`、`catalog` 等规划模块。
- `README.md`：更新项目长期定位和当前阶段说明。

### 验证方式

- 文档审阅。

### 后续计划

- 抽象 `ContextDocument` 和 `Scope`。
- 将现有代码库索引迁移为 `CodebaseSource`。
- 实现 MCP Server 基础能力。
- 实现历史会话导入和记忆检索入口。

## 2026-05-08 Source 分片与同步设计

### 目标

为长期记忆补充 source 级存储、导出、导入和多端同步方案，避免所有记忆集中在单个大向量文件中，提升记忆迁移、备份、删除和同步的可维护性。

### 方案

- 本地长期记忆采用 source-sharded 存储，一个 source 一个目录和向量文件。
- 每个 source 目录包含 `manifest.json`、`vectors.json`、`memories.jsonl`、可选 `raw.jsonl` 和 `snapshot.json`。
- `SourceCatalog` 记录 source 的版本、checksum、文档数量和记忆数量。
- 搜索时先通过 `SourceCatalog` 找到候选 source shard，再聚合检索和全局重排。
- source bundle 作为导出、导入和同步的最小推荐单位。
- 多端同步先基于 catalog、version 和 checksum 对比，后续再补充 append-only event log。

### 模块影响

- `docs/design/overall_design.md`：新增 source-sharded 本地存储、source manifest、导出导入和同步流程。
- `docs/modules/module_design.md`：新增 `sourcestore` 模块规划，并扩展 `contextdoc`、`vectorstore`、`catalog`、MCP 和 CLI 规划。
- `README.md`：补充 source 分片设计方向。
- `internal/contextdoc`：新增通用上下文文档、作用域、来源和 manifest 模型。

### 验证方式

- `gofmt -w ./internal/contextdoc`
- `go test ./...`

### 后续计划

- 实现 `SourceCatalog` 本地持久化。
- 实现 source bundle 导出和导入。
- 实现多 source shard 聚合检索。

## 2026-05-08 SourceCatalog 本地持久化实现

### 目标

实现 source 元信息的本地持久化能力，为后续 source shard 检索、导出、导入和多端同步提供统一目录。

### 方案

- 新增 `internal/catalog` 包。
- 使用 `catalog/sources.json` 保存所有 source 元信息。
- 定义 `Entry`、`Filter`、`Status` 和 `Store`。
- 支持 `Upsert`、`UpsertManifest`、`List`、`Get` 和 `Delete`。
- 通过 `scope_type + scope_id + source_type + source_id` 生成稳定 key，实现幂等覆盖。
- 写入时先写临时文件，再通过 rename 替换正式文件。

### 模块影响

- `internal/catalog`：新增 SourceCatalog 本地持久化实现和单元测试。
- `internal/contextdoc`：复用 `Scope`、`Source` 和 `Manifest` 作为 catalog entry 的来源模型。

### 验证方式

- `gofmt -w ./internal/catalog`
- `go test ./...`
- `GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/code-context`
- `git diff --check`

### 后续计划

- 实现 source bundle 导出和导入。
- 实现多 source shard 聚合检索。
- 将索引流程逐步接入 `SourceCatalog`。

## 2026-05-08 Agent/Coding 定位与领域经验设计

### 目标

分析 Basic Memory 与本项目的定位差异，避免将本项目做成 Markdown 笔记知识库的 Go 复刻；同时评估“通用经验”和“领域专业经验”分层检索需求的可实现性，并将可行方案纳入设计。

### 方案

- 将项目长期定位收敛为面向 AI 编程助手、IDE Agent 和自动化工具链的 Agent/Coding 上下文记忆引擎。
- 明确 Basic Memory 偏 Markdown 笔记、轻量知识图谱和人机共写知识库；本项目偏代码库、历史会话、工具轨迹、领域专业经验和会话启动上下文。
- 规划 `BasicMemorySource`，将 Basic Memory Markdown 项目作为外部 source 兼容导入，而不是复刻其笔记编辑能力。
- 增加经验维度：`experience_kind=general|domain|project|tool`。
- 增加领域维度：`domain=golang|milvus|kubernetes|frontend|database|testing|deployment|...`。
- 搜索时先进行领域识别；如果 query 命中专业领域，则提高该领域专业经验的 TopK 和 token budget，同时保留少量通用经验。
- 领域识别先使用规则、关键词、文件扩展名、工具名和 source metadata，可后续接入 LLM 或轻量分类器。

### 模块影响

- `README.md`：更新项目定位为 `Agent-Memory`，补充领域经验设计和 Basic Memory 兼容方向。
- `docs/design/overall_design.md`：补充 Agent/Coding 定位、Basic Memory 差异、领域经验维度和领域检索策略。
- `docs/modules/module_design.md`：新增 `internal/domain` 规划，补充 `ExperienceKind`、`Domain`、`BasicMemorySource` 和领域专业经验搜索能力。

### 验证方式

- 文档审阅。
- `git diff --check`

### 后续计划

- 在 `internal/contextdoc` 中补充 `ExperienceKind`、`Domain` 和 `DomainConfidence` 字段。
- 实现 `internal/domain` 的基础规则分类器。
- 在搜索器中增加领域匹配后的经验预算调整。

## 2026-05-08 领域经验基础模型实现

### 目标

按照领域经验设计继续落地基础模型和规则分类能力，为后续领域专业经验检索和会话启动上下文预算调整打基础。

### 方案

- `internal/contextdoc` 新增 `ExperienceKind`、`Domain` 和 `DomainConfidence` 字段。
- 定义通用经验、领域专业经验、项目经验、工具经验四类 `ExperienceKind`。
- 定义 Go、Milvus、Kubernetes、前端、数据库、测试、部署等基础 `Domain`。
- 新增 `internal/domain` 包，提供基于规则的 `DefaultClassifier`。
- 规则分类器根据 query、文件扩展名、工具名和 source metadata 打分。
- 分类结果包含 `domain`、`domain_confidence`、`experience_kind` 和检索预算建议。

### 模块影响

- `internal/contextdoc`：新增领域经验字段和测试。
- `internal/domain`：新增基础领域分类器和测试。
- `docs/development_log.md`：记录本次实现。

### 验证方式

- `gofmt -w ./internal/contextdoc ./internal/domain`
- `go test ./...`
- `GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/code-context`
- `git diff --check`

### 后续计划

- 在搜索器中接入 `domain.Classifier`。
- 在 `SourceCatalog` 和 source metadata 中补充领域过滤能力。
- 实现领域匹配后的多 source shard 聚合检索预算调整。

## 2026-05-08 动态树形领域模型调整

### 目标

将专业领域从固定枚举调整为动态创建的树形领域路径，支持类似标签的灵活扩展。当前先支持两级领域，后续可扩展到三级和四级。

### 方案

- `Domain` 不再表示固定枚举，而是动态树形路径。
- 当前领域路径最多两级，例如 `programming/go`、`database/milvus`、`medicine/cardiology`。
- 一级领域表示大类，二级领域表示细分专业方向。
- 新增 `NormalizeDomain`、`Levels`、`Depth`、`Parent` 和 `Match`，支持路径归一化和父子领域匹配。
- 基础规则分类器的默认规则改为动态领域路径。
- 搜索领域判断应先识别一级和二级候选；二级命中时同时召回父级领域经验。
- 领域排序应综合层级匹配度、领域置信度、向量相似度、时间新鲜度和成功次数。

### 模块影响

- `internal/contextdoc`：调整 `Domain` 为动态树形路径，新增领域路径 helper 和测试。
- `internal/domain`：默认规则改为两级领域路径。
- `docs/design/overall_design.md`：更新领域模型和搜索判断策略。
- `docs/modules/module_design.md`：更新领域模块规划。

### 验证方式

- `gofmt -w ./internal/contextdoc ./internal/domain`
- `go test ./...`
- `GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/code-context`
- `git diff --check`

### 后续计划

- 在搜索器中基于父子领域匹配调整检索预算。
- 将 `SourceCatalog` 和 source metadata 的领域过滤字段改为 `domain_path`。
- 后续按需要将最大领域深度从两级扩展到三到四级。

## 2026-05-08 多信号领域判断方案设计

### 目标

当前规则分类器虽然简单可控，但领域判断方式过于依赖关键词。需要将规则分类器降级为信号源，并规划更准确、可扩展、可解释的多信号领域判断方案。

### 方案

- 将当前 `DefaultClassifier` 后续降级为 `RuleSignal`，只负责提供关键词、别名、文件扩展名等规则信号。
- 规划 `DomainResolver`，统一融合显式上下文信号、规则信号、领域画像向量匹配和记忆命中分布。
- 规划 `DomainProfileStore`，保存领域路径、描述、别名、典型问题样例和领域画像向量。
- 规划 `ProfileMatcher`，使用 query embedding 与 domain profile embedding 计算语义相似度。
- 规划 `MemoryDistributionSignal`，通过一次轻量普通向量预检索，根据 TopN 记忆的 `domain_path` 分布反推领域。
- 低置信或多个候选领域冲突时，可选调用 LLM 进行结构化领域判断。
- `DomainResolver` 最终输出 `domain_path`、父级领域、置信度、evidence 和检索预算建议。

### 模块影响

- `docs/design/overall_design.md`：补充多信号领域判断、领域画像和融合打分方案。
- `docs/modules/module_design.md`：扩展 `internal/domain` 的后续 TODO，加入 `DomainResolver`、`DomainProfileStore`、`ProfileMatcher` 和 `MemoryDistributionSignal`。

### 验证方式

- 文档审阅。
- `git diff --check`

### 后续计划

- 将 `internal/domain.DefaultClassifier` 重命名或封装为 `RuleSignal`。
- 新增 `DomainProfile` 和 `DomainProfileStore`。
- 实现 `DomainResolver` 的多信号融合框架。

## 2026-05-08 多信号领域解析框架实现

### 目标

落地多信号领域判断方案的基础框架，让领域判断从单一规则分类器升级为可融合多种证据的 `DomainResolver`。

### 方案

- 新增 `DomainProfile`，保存领域路径、描述、别名、典型问题和可选画像向量。
- 新增 `ProfileStore` 接口和 `StaticProfileStore` 内存实现。
- 新增 `DomainResolver`，融合多个 `Signal` 输出的领域证据。
- 新增 `ExplicitSignal`，根据 metadata domain_path 和文件扩展名生成证据。
- 新增 `RuleSignal`，将现有 `DefaultClassifier` 包装为规则证据源。
- 新增 `ProfileSignal`，基于领域画像文本匹配生成证据，后续可替换为 embedding 匹配。
- 新增 `MemoryDistributionSignal`，根据预检索记忆的领域分布反推领域。
- `DomainResolver` 输出 `Domain`、`ParentDomain`、`DomainConfidence`、`ExperienceKind`、预算和 evidence。

### 模块影响

- `internal/domain`：新增 `profile.go`、`resolver.go` 和对应测试。
- `docs/modules/module_design.md`：更新 `internal/domain` 当前能力。

### 验证方式

- `gofmt -w ./internal/domain`
- `go test ./...`
- `GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/code-context`
- `git diff --check`

### 后续计划

- 将 `ProfileSignal` 升级为 embedding 相似度匹配。
- 在搜索器中接入 `DomainResolver`。
- 将 source metadata 和 `SourceCatalog` 的领域字段统一为 `domain_path`。

## 2026-05-08 领域强过滤搜索接入

### 目标

按照“直接强过滤”的要求，将 `DomainResolver` 接入搜索流程。搜索 query 命中领域时，向量检索必须带上 `domain_path` 过滤条件，只返回对应领域和父级领域的文档。

### 方案

- `vectorstore.SearchOptions` 新增 `DomainFilters`。
- `LocalStore.Search` 按 `MetadataDomainPath` 执行强过滤。
- `MilvusStore.Search` 将 `DomainFilters` 编入 Milvus 表达式。
- Milvus schema、写入列、输出字段和结果解析增加 `domain_path`。
- `Indexer` 在写入 chunk 时调用 `DomainResolver`，将 `domain_path` 和 `experience_kind` 写入文档 metadata。
- `Searcher` 在搜索时调用 `DomainResolver`，命中二级领域时传入二级领域和父级领域作为强过滤条件。
- 当前 CLI search 复用该行为，不再只做软 boost。

### 模块影响

- `internal/searcher`：接入 `DomainResolver` 并传递强领域过滤条件。
- `internal/vectorstore`：本地和 Milvus 检索均支持 `DomainFilters`。
- `internal/indexer`：索引时写入 `domain_path` 和 `experience_kind`。
- `internal/vectorstore`、`internal/searcher`：新增相关测试。

### 验证方式

- `gofmt -w ./internal/searcher ./internal/vectorstore ./internal/indexer`
- `go test ./...`
- `GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/code-context`
- `git diff --check`

### 后续计划

- 将 `ProfileSignal` 升级为 embedding 相似度匹配。
- 支持 source shard 聚合检索时的领域强过滤。
- 为 CLI 输出领域过滤信息，便于解释为什么没有结果。

## 2026-05-08 文档知识库与提示词级上下文设计

### 目标

补充文档知识库能力设计，使书籍、超长说明文档、技术文档、SDK 文档和项目规范能与现有长期记忆历史并存。搜索时需要同时检索代码库、长期记忆、工具历史和文档知识库，并一次性返回统一上下文包。本项目最终目标是在每次对话开始或每次用户发送提示词时，自动为 AI 提供足量、准确、可追溯的上下文记忆以及需要参考的知识。

### 方案

- 在整体目标中加入文档知识库和提示词级上下文供给能力。
- 规划 `DocumentSource` 和 `BasicMemorySource`，将文档知识作为 source shard 与历史记忆 source shard 并存。
- 规划 `MarkdownSplitter`，按标题层级切分文档，保留 `heading_path`，并保持代码块、表格和列表完整。
- 文档入库时生成 `document_summary`、`chapter_summary`、`section_summary` 和 `chunk` 多层节点。
- 增加文档知识元数据：`document_id`、`section_id`、`heading_path`、`knowledge_kind`、`version`、`keywords` 和 `symbols`。
- 规划 `SearchDocumentKnowledge` 和 `SearchPromptContext`，根据提示词生成 `RetrievalPlan`，并行搜索代码库、长期记忆、工具历史和文档知识库。
- 规划 `ContextAssembler`，负责去重、冲突检测、版本选择、章节回补、压缩和统一上下文包组织。
- 明确上下文包组织顺序：任务判断、强规则、代码上下文、历史经验、文档知识、冲突说明和来源引用。

### 模块影响

- `README.md`：补充文档知识库、提示词级上下文构建和最终目标说明。
- `docs/design/overall_design.md`：补充文档知识索引流程、多阶段检索策略、并行搜索和统一上下文组织方案。
- `docs/modules/module_design.md`：新增 `internal/document`、`internal/contextassembler` 规划，并扩展 `splitter`、`searcher`、`sessioncontext`、MCP 和 CLI 规划。

### 验证方式

- 文档审阅。
- `git diff --check`

### 后续计划

- 实现 `MarkdownSplitter`。
- 实现文档知识多层节点模型和元数据字段。
- 扩展 `SearchOptions` 支持文档过滤条件。
- 实现 `ContextAssembler` 和提示词级统一上下文包。

## 2026-05-08 Markdown 文档切分与知识元数据模型实现

### 目标

继续落地文档知识库能力，先实现 Markdown 文档结构化切分和基础知识元数据模型，为后续 `DocumentSource`、文档知识搜索和 `ContextAssembler` 做准备。

### 方案

- `splitter.Chunk` 增加 `Metadata` 字段，用于携带文档知识元数据。
- 新增 `MarkdownSplitter`，按 Markdown 标题层级切分文档。
- Markdown chunk 写入 `document_id`、`section_id`、`heading_path` 和 `knowledge_kind`。
- Markdown 切分时避免在 fenced code block 和表格中间截断。
- `Indexer` 写入向量文档时合并 chunk metadata，使文档元数据可进入 `vectorstore.Document.Metadata`。
- `contextdoc` 增加 `SourceTypeDocument`、`SourceTypeExternalKnowledge`、文档摘要/章节/原文 chunk 类型、`KnowledgeKind` 和 `DocumentLocation`。
- `Manifest` 和 `catalog.Entry` 增加 `KnowledgeCount`，用于统计文档知识节点数量。
- `vectorstore` 增加文档知识相关 metadata key 常量，供后续过滤检索使用。

### 模块影响

- `internal/splitter`：新增 `markdown.go` 和 `markdown_test.go`，扩展 `Chunk.Metadata`。
- `internal/indexer`：合并 splitter 输出的 metadata。
- `internal/contextdoc`：补充文档 source、chunk type、knowledge kind 和 document location 模型。
- `internal/catalog`：补充 `KnowledgeCount` 字段和校验。
- `internal/vectorstore`：补充文档知识 metadata key 常量。
- `README.md`、`docs/design/overall_design.md`、`docs/modules/module_design.md`：同步更新当前实现状态。

### 验证方式

- `gofmt -w ./internal/splitter ./internal/contextdoc ./internal/catalog ./internal/indexer ./internal/vectorstore`
- `go test ./...`

### 后续计划

- 实现 `DocumentSource`。
- 扩展 `SearchOptions` 支持 `document_id`、`knowledge_kind` 和 `heading_path` 过滤。
- 实现文档多层摘要节点生成。
- 实现 `ContextAssembler` 和提示词级统一上下文包。

## 2026-05-09 多格式文档解析器架构调整

### 目标

根据文档知识库后续需要支持 PDF 等非 Markdown 格式的方向，将 Markdown 从“文档切分器”调整为“文档解析器”。不同格式文档应先由对应 Parser 判断标题、摘要和正文等结构，再交给通用切块和存储流程。

### 方案

- 新增 `internal/document` 包，定义 `Parser`、`Node` 和 `NodeKind`。
- `NodeKind` 当前支持 `title`、`summary` 和 `body`。
- 新增 `MarkdownParser`，负责解析 Markdown 标题层级、摘要行和正文节点。
- Markdown 解析器忽略 fenced code block 内的伪标题，避免把代码示例中的 `#` 当成文档标题。
- `MarkdownSplitter` 改为 `MarkdownParser` 的适配层，只负责把解析节点转换为 `splitter.Chunk`。
- Markdown chunk 额外写入 `node_kind`，区分标题、摘要和正文。
- 设计文档中明确后续 PDF、DOCX 等格式应实现各自 Parser，并通过统一 `DocumentChunker` 切块和存储。

### 模块影响

- `internal/document`：新增通用文档解析器模型和 Markdown 解析器。
- `internal/splitter`：`MarkdownSplitter` 调整为解析器适配层。
- `README.md`、`docs/design/overall_design.md`、`docs/modules/module_design.md`：更新多格式文档解析架构说明。

### 验证方式

- `gofmt -w ./internal/document ./internal/splitter`
- `go test ./internal/document ./internal/splitter`
- `go test ./...`

### 后续计划

- 实现 `DocumentSource`，根据文件类型选择合适 Parser。
- 实现 `PDFParser`，将 PDF 页码、标题候选、段落和表格解析为统一 `Node`。
- 抽象通用 `DocumentChunker`，替换当前 Markdown 专属适配层。
- 扩展检索过滤条件支持 `node_kind`。

## 2026-05-09 文档源、通用切块器与检索过滤实现

### 目标

暂不继续实现 PDF、DOCX 等其他解析器，先完善 Markdown 解析器之外的文档知识库基础设施，包括通用文档节点切块、Markdown 文档源读取和文档知识检索过滤条件。

### 方案

- 新增 `DocumentChunker`，将 Parser 输出的 `title`、`summary`、`body` 节点统一转换为 `splitter.Chunk`。
- `MarkdownSplitter` 改为复用 `DocumentChunker`，只保留 Markdown 特有的 fenced code block 和表格边界扩展逻辑。
- `vectorstore.SearchOptions` 增加 `DocumentFilters`、`SectionFilters`、`HeadingFilters`、`KnowledgeKindFilters`、`NodeKindFilters` 和 `VersionFilters`。
- `LocalStore` 支持按文档知识元数据过滤。
- `MilvusStore` schema、写入列、输出字段、结果解析和表达式构建增加文档知识元数据字段。
- 新增 `MarkdownDocumentSource`，支持读取单个 `.md` / `.markdown` 文件或目录，并调用 `MarkdownParser` 输出统一文档节点。

### 模块影响

- `internal/splitter`：新增 `DocumentChunker` 和对应测试，`MarkdownSplitter` 复用通用切块器。
- `internal/vectorstore`：扩展文档知识过滤条件，本地存储和 Milvus 表达式均支持。
- `internal/source`：新增 Markdown 文档源读取能力。
- `README.md`、`docs/design/overall_design.md`、`docs/modules/module_design.md`：同步更新实现状态。

### 验证方式

- `gofmt -w ./internal/document ./internal/splitter ./internal/source ./internal/vectorstore`
- `go test ./...`
- `GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/code-context`
- `git diff --check`

### 后续计划

- 将 `MarkdownDocumentSource` 接入正式文档索引命令。
- 实现 source shard 级文档知识写入。
- 增加 CLI / MCP 的 `import knowledge` 与统一 `search` 入口。
- 暂不实现其他格式 Parser，待 Markdown 文档链路闭环后再扩展 PDF。

## 2026-05-09 项目目录与模块路径迁移到 Agent-Memory

### 目标

用户已将项目目录改为 `/Users/aaq/Desktop/project/Agent-Memory`。需要在新目录下工作，并修复旧项目名、旧 module path、旧默认存储目录和旧环境变量前缀残留造成的问题。

### 方案

- 将 Go module 从 `github.com/aaq/go-code-context` 更新为 `github.com/mazhan465/Agent-Memory`。
- 将所有内部 import 路径同步到新 module path。
- 将项目文档主名称更新为 `Agent-Memory`。
- 将默认本地存储目录更新为 `~/.agent-memory`。
- 将 Milvus 默认 collection 更新为 `agent_memory_chunks`。
- 将环境变量前缀从 `GO_CODE_CONTEXT_` 更新为 `AGENT_MEMORY_`。
- 在新目录 `/Users/aaq/Desktop/project/Agent-Memory` 下执行后续测试、编译和检查。
- 为新目录启动 claude-context 索引，后续代码理解优先使用新目录索引。

### 模块影响

- `go.mod`：更新 module path。
- `cmd`、`internal`：更新内部 import 路径。
- `internal/config`：更新默认存储目录、环境变量前缀和默认 Milvus collection。
- `README.md`、`docs/design/overall_design.md`、`docs/modules/module_design.md`、`docs/development_rules.md`、`docs/development_log.md`：更新项目名称、环境变量和路径说明。

### 验证方式

- `gofmt -w ./cmd ./internal`
- `go test ./...`
- `GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/code-context`
- `git diff --check`

### 后续计划

- 若后续决定重命名 CLI，再将 `cmd/code-context` 迁移为 `cmd/agent-memory` 并保留兼容入口。

## 2026-05-09 文档知识导入与统一搜索闭环

### 目标

将 `MarkdownDocumentSource` 接入正式 CLI，使 Markdown 文档 source 可以被导入到独立 source shard；搜索统一收敛到普通 `search` 入口，默认一次返回代码、知识文档、历史会话、经验和用户偏好等全部类型的 JSON 结果。

### 方案

- `cmd/code-context` 新增顶层 `import knowledge <path> [source-id]` 命令，后续 `import` 入口也可扩展为数据迁移导入入口。
- 导入流程读取 `.md` / `.markdown` 文档，使用 `DocumentChunker` 转换为向量文档。
- 每个文档 source 使用 `contextdoc.NamespaceForSource` 生成独立 namespace，避免与代码库索引混写。
- 写入向量文档时保留 `document_id`、`section_id`、`heading_path`、`knowledge_kind` 和 `node_kind`。
- 写入向量文档时补充 `scope_type`、`scope_id`、`source_type`、`source_id` 和 `absolute_path` 元数据。
- 导入成功后通过 `SourceCatalog` 记录 manifest、checksum、文档数和知识 chunk 数。
- `cmd/code-context search` 统一检索代码 namespace 和 `SourceCatalog` 中已索引的 source namespace。
- `search` 支持通过 `types` 参数只搜索指定类型，例如 `code`、`knowledge`、`conversation`、`experience`、`preference`、`tool_history`、`fact`。
- 搜索结果统一输出 JSON，并用 `category` 标注 `code`、`knowledge_document`、`experience`、`user_preference` 等类型。

### 模块影响

- `cmd/code-context`：新增 `import knowledge` 导入命令，并将普通 `search` 改为统一 JSON 搜索入口。
- `internal/source`、`internal/splitter`、`internal/vectorstore`、`internal/catalog`：被 CLI 文档知识链路正式串联。
- `README.md`、`docs/modules/module_design.md`、`docs/development_log.md`：同步更新当前能力和命令说明。

### 验证方式

- `gofmt -w cmd/code-context/main.go`
- `go test ./...`
- `GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/code-context`
- `git diff --check`

### 后续计划

- 增加 source 查看、清理、导出和导入能力。
- 将统一搜索扩展为更完整的跨 source shard 聚合检索和上下文组装。
- 为 `search` 增加 `document_id`、`heading_path`、`knowledge_kind` 和 `node_kind` 过滤参数。
- 增加 MCP 的 `import knowledge` 和统一 `search` 工具入口。

## 2026-05-09 长期记忆基础导入与 CLI 拆分

### 目标

让项目从“可索引代码和知识文档”推进到“基本可用的 Agent 记忆检索系统”：除代码和知识文档外，至少可以导入历史会话、经验、用户偏好、工具历史和项目事实，并通过统一 `search` JSON 入口返回。

### 方案

- 新增 `cmd/code-context/import.go`，承载导入相关逻辑。
- 新增 `cmd/code-context/search.go`，承载统一搜索、类型过滤和 JSON 结果组装逻辑。
- `cmd/code-context/main.go` 保留 CLI 入口、配置初始化、`index`、`status` 和 `clear`，避免单文件超过 800 行。
- `import memory <type> <json-or-jsonl-path> [source-id]` 支持导入 `conversation`、`experience`、`preference`、`tool_history` 和 `fact`。
- memory 导入文件支持 JSON 数组和 JSONL 两种格式，每条记录必须包含 `content` 字段。
- memory 向量文档写入 `source_type`、`source_id`、`record_id`、`role`、`conversation_id`、`message_id`、`tool_name`、`tags`、`domain_path` 和 `experience_kind` 等元数据。
- memory 导入成功后写入 `SourceCatalog`，统一 `search` 可按类型检索并通过 `category` 标注结果来源。

### 模块影响

- `cmd/code-context`：拆分入口、搜索和导入逻辑，新增 memory 导入能力。
- `README.md`：补充 memory JSONL 导入示例。
- `docs/design/overall_design.md`、`docs/modules/module_design.md`、`docs/development_log.md`：更新基本可用范围。

### 验证方式

- `gofmt -w cmd/code-context/main.go cmd/code-context/search.go cmd/code-context/import.go cmd/code-context/import_test.go`
- `go test ./cmd/code-context`
- `go test ./...`
- `GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/code-context`
- `git diff --check`

### 后续计划

- 增加 source 导出和导入能力。
- 增加 `search` 的元数据过滤参数。
- 增加 MCP 工具入口，让 IDE Agent 可直接调用统一搜索和导入能力。
- 实现会话启动时的 `SessionContext` 自动上下文包组装。

## 2026-05-09 Source 管理命令实现

### 目标

补齐基本可用所需的 source 管理能力，让用户可以查看已经导入的知识和记忆 source，并在导入错误或数据过期时清理对应 source。

### 方案

- 新增 `cmd/code-context/source.go`，提供 `source list [type]` 和 `source clear <type> <source-id>`。
- `source list` 从 `SourceCatalog` 读取 source 元信息，并以 JSON 输出。
- `source clear` 根据 source type 和 source id 找到匹配 catalog entry，清理对应 namespace 的向量数据并删除 catalog 记录。
- 新增 source type 参数别名，支持 `knowledge`、`conversation`、`experience`、`preference`、`tool_history` 和 `fact`。
- 新增 `cmd/code-context/source_test.go` 验证 source type 解析。

### 模块影响

- `cmd/code-context`：新增 source 管理命令和测试。
- `README.md`、`docs/design/overall_design.md`、`docs/modules/module_design.md`、`docs/development_log.md`：同步更新基本可用能力。

### 验证方式

- `gofmt -w cmd/code-context/source.go cmd/code-context/source_test.go`
- `go test ./cmd/code-context`
- `go test ./...`
- `GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/code-context`
- `git diff --check`

### 后续计划

- 实现 source bundle 导出和导入。
- 实现 MCP 工具入口。
- 实现 `SessionContext` 自动上下文包组装。

## 2026-05-09 搜索会话级去重

### 目标

先不做复杂的单次搜索语义去重，只实现会话级去重：同一个 session 中，后续搜索在返回前读取该 session 已返回过的数据，并过滤重复结果。如果用户没有传入 session id，则自动生成一个并在 JSON 响应中返回。

### 方案

- `search` 支持 `session-id` 位置参数和 `--session-id` / `--session-id=<id>` 参数。
- 搜索响应新增 `session_id` 和 `deduped_count`。
- 搜索结果新增 `result_id` 和 `content_hash`。
- 新增本地 `sessions/<session-id>.json` 会话状态文件，记录 `returned_result_keys` 和 `returned_content_hashes`。
- 会话去重判断：如果 `namespace + document.id` 已返回，或 normalized content hash 已返回，则本次不再返回该结果。
- 为减少被旧 TopK 结果占满导致无新结果，实际检索候选数扩大为用户 limit 的 3 倍，最终返回仍按用户 limit 截断。

### 模块影响

- `cmd/code-context/search.go`：解析 session id、搜索前加载 session、返回前过滤并保存 session 状态。
- `cmd/code-context/session.go`：新增会话状态读写、session id 生成、result key 和 content hash 计算。
- `cmd/code-context/session_test.go`：新增会话去重和参数解析测试。
- `README.md`、`docs/design/overall_design.md`、`docs/modules/module_design.md`、`docs/development_log.md`：同步更新说明。

### 验证方式

- `gofmt -w cmd/code-context/search.go cmd/code-context/session.go cmd/code-context/session_test.go`
- `go test ./cmd/code-context`
- `go test ./...`
- `GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/code-context`
- `git diff --check`

### 后续计划

- 增加单次搜索内的相邻 chunk 合并和跨 source 去重。
- 增加 session 清理和过期策略。
- 将 session 去重沉淀为独立 internal 包，供 MCP Server 和 SessionContext 复用。

## 2026-05-09 非 MCP 优先级确认与文件规则增强

### 目标

根据和 `claude-context` 的对比结果，确认 MCP Server 实装暂不作为最高优先级，优先推进索引状态、增量索引、代码切块、混合检索、文件包含/排除规则、Ollama embedding 和代码项目隔离机制。本轮先完成路线文档更新，并落地文件包含/排除规则增强。

### 方案

- `docs/design/overall_design.md` 增加近期开发优先级，明确 MCP 进入未来规划，当前优先做非 MCP 的代码检索工程化能力。
- `internal/config` 增加 `AGENT_MEMORY_CUSTOM_EXTENSIONS` 和 `AGENT_MEMORY_CUSTOM_IGNORE_PATTERNS`，支持追加文件扩展名和 glob 忽略规则。
- 默认扩展名补齐 Objective-C、Dart、Solidity、Notebook 等类型。
- 默认忽略规则从目录名扩展为 glob pattern，覆盖构建产物、缓存、日志、环境文件、minified/bundle/source map 等常见无关文件。
- `internal/scanner` 支持读取代码库根目录 `.gitignore`、`.contextignore`、`.cursorignore` 等 `.xxxignore` 文件，并和配置规则合并。
- `Scanner.Scan` 返回结果按相对路径稳定排序，为后续增量索引和 snapshot 对比提供稳定输入。

### 模块影响

- `internal/config`：新增扩展名和忽略规则配置。
- `internal/scanner`：新增 ignore 文件读取、glob 匹配和稳定排序。
- `cmd/code-context/main.go`：索引流程改用 `scanner.NewWithPatterns`。
- `internal/scanner/scanner_test.go`：新增 ignore 文件和自定义规则测试。
- `README.md`、`docs/design/overall_design.md`、`docs/modules/module_design.md`、`docs/development_log.md`：同步更新当前能力和后续路线。

### 验证方式

- `gofmt -w internal/config/config.go internal/scanner/scanner.go internal/scanner/scanner_test.go cmd/code-context/main.go`
- `go test ./internal/scanner ./internal/config ./cmd/code-context`
- `go test ./...`
- `GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/code-context`
- `git diff --check`

### 后续计划

- 下一步实现文件 hash snapshot 和增量索引基础能力。
- 然后实现 Go AST splitter，并规划 tree-sitter 多语言切块。
- 再补充本地关键词/BM25 召回抽象和 Milvus hybrid collection 规划。
- 接入 Ollama embedding provider。

## 2026-05-09 文件 hash 增量索引基础能力

### 目标

在现有 CLI `index` 流程中接入文件 hash snapshot，首次或旧快照缺少哈希时执行全量索引，后续自动对比新增、修改和删除文件，只替换变化文件对应的 chunk。

### 方案

- `snapshot.Info` 增加 `file_hashes` 字段，记录 `relative_path -> sha256(content)`。
- `Indexer.Index` 调整为增量优先：先读取旧快照，再写入 `indexing` 状态，避免覆盖旧 hash。
- 首次索引或旧快照缺少 `file_hashes` 时执行全量 `VectorStore.Put`。
- 有可用旧快照时，对比当前文件 hash，得到 added / modified / removed。
- 只对 added / modified 文件重新切块、embedding 和构建向量文档。
- `VectorStore` 新增 `ReplaceFiles` 接口，用于删除指定 `relative_path` 的旧 chunk 并写入新 chunk。
- `LocalStore.ReplaceFiles` 通过加载本地 JSON、过滤旧文件 chunk、追加新文档并稳定排序实现。
- `MilvusStore.ReplaceFiles` 通过 namespace + relative_path 过滤表达式删除旧 chunk，再插入新文档并 flush。
- CLI `index` 输出新增 `added`、`modified`、`removed` 和 `full_reindex`，方便判断本次索引类型。

### 模块影响

- `internal/indexer`：实现增量索引流程、文件 hash 对比和变更统计。
- `internal/snapshot`：快照增加 `file_hashes`。
- `internal/vectorstore`：接口增加 `ReplaceFiles`，本地和 Milvus 实现同步补齐。
- `cmd/code-context`：索引命令输出增量统计。
- `internal/indexer/indexer_test.go`：新增首次全量、后续新增/修改/删除的增量索引测试。
- `README.md`、`docs/design/overall_design.md`、`docs/modules/module_design.md`、`docs/development_log.md`：同步更新设计和当前能力。

### 验证方式

- `gofmt -w internal/indexer internal/snapshot internal/vectorstore cmd/code-context/main.go`
- `go test ./internal/indexer ./internal/vectorstore ./cmd/code-context`
- `go test ./...`
- `GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/code-context`
- `git diff --check`

### 后续计划

- 继续补充索引状态和自动同步入口，例如独立 `sync` 命令或后台同步服务。
- 实现 Go AST splitter，提升代码 chunk 的语义完整度。
- 规划代码 source 的 collection / namespace 隔离策略，避免长期单 collection 在大规模代码索引场景下扩展受限。
