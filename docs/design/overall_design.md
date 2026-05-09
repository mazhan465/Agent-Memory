# Agent-Memory 整体设计

## 1. 项目目标

`Agent-Memory` 目标是用 Go 实现一套面向 AI 编程助手、IDE Agent 和自动化工具链的上下文记忆与知识供给引擎。项目早期能力参考 `claude-context`，优先打通代码库索引与检索；后续会扩展为 Agent/Coding 场景的长期记忆、文档知识库和上下文管理底座，支持代码库、历史会话、工具轨迹、用户偏好、通用经验、领域专业经验、书籍、技术文档、SDK 文档和项目规范等多类 source。

项目需要刻意区别于 Basic Memory 一类 Markdown 知识库：Basic Memory 的核心是人机共写 Markdown 笔记和轻量知识图谱；本项目的核心是自动收集和检索 Agent/Coding 工作流中的代码上下文、工具历史、调试经验、文档知识和会话启动上下文。Basic Memory 可以作为外部记忆源接入，但不作为本项目的主形态。

最终目标是在每次对话开始或每次用户发送提示词时，自动为 AI 提供足量、准确、可追溯的上下文记忆和需要参考的知识，使 AI 不仅能看到当前代码环境，也能同时获得历史经验、用户偏好、项目事实、领域知识和相关文档依据。

核心目标：

1. 将大型代码库转换为可检索的语义索引。
2. 将用户历史会话、工具轨迹和长期偏好转换为可检索的 Agent 记忆索引。
3. 将书籍、超长说明文档、技术文档、SDK 文档和项目规范转换为可检索的文档知识索引。
4. 支持自然语言查询相关代码片段、历史经验、用户偏好、领域专业经验和文档知识。
5. 区分通用经验、领域专业经验和文档知识，在领域匹配时提升专业经验与相关文档的检索详细度和召回预算。
6. 在每次会话开始和每次用户提示词到达时自动构建相关上下文包，辅助 AI 助手理解当前任务。
7. 同一次上下文构建中并行搜索代码库、长期记忆、历史会话和文档知识库，并一次性返回统一上下文包。
8. 通过 CLI 和 MCP Server 暴露索引、搜索、记忆检索、知识检索和会话上下文构建能力。
9. 底层支持本地存储和 Milvus 等向量数据库。
10. 后续支持增量索引、混合检索、AST 切块、Markdown 结构化切分、多 embedding provider、rerank 和上下文压缩。

### 1.1 近期开发优先级

MCP Server 会保留在未来规划中，但当前不作为最高优先级。近期按照以下顺序逐步补齐 `claude-context` 已验证的代码检索工程化能力：

1. **索引状态和可搜索性增强**：在非 MCP 场景先完善索引状态、进度记录和部分可搜索基础能力，为后续异步 MCP 复用。
2. **增量索引与自动同步基础**：基于文件 hash snapshot 识别新增、删除和修改文件，避免每次全量重建。
3. **代码切块质量提升**：优先实现 Go AST splitter，后续再评估 tree-sitter 多语言 splitter；无法解析时回退到行级切块。
4. **混合检索**：在 dense vector 基础上补充关键词/BM25 类召回信号，先完成本地和接口抽象，再规划 Milvus hybrid collection。
5. **文件包含与排除规则增强**：支持默认规则、环境变量自定义扩展名/忽略规则、根目录 `.gitignore` 和 `.xxxignore`。
6. **Ollama embedding**：先支持本地 Ollama embedding provider，VoyageAI、Gemini 暂缓。
7. **代码项目隔离机制**：规划代码索引独立 namespace/collection 策略，避免代码库、知识库和长期记忆互相影响；本地默认仍可使用 namespace，Milvus 后续按 codebase collection 或稳定隔离键演进。

MCP 实装、搜索高级参数、IDE 插件、性能大仓库保护、评测体系、rerank 和上下文压缩进入后续规划，但相关接口设计应避免阻塞未来接入。

## 2. 系统边界

### 输入

- 本地代码库路径。
- 历史会话记录，如 openclaw 等工具的历史会话。
- 用户长期偏好、经验总结、项目事实和人工记忆。
- 文档知识源，如书籍、超长说明文档、技术文档、SDK 文档、项目规范和 Basic Memory Markdown 项目。
- 自然语言查询。
- 会话启动信息，如当前工作区、当前文件、用户问题、工具名称。
- 可选过滤条件，如数据源类型、命名空间、文件扩展名、路径前缀、文档 ID、章节路径、知识类型、TopK。

### 输出

- 匹配的代码片段。
- 匹配的历史会话片段。
- 匹配的用户偏好、经验总结和项目事实。
- 匹配的文档知识片段、章节摘要、文档摘要、API 说明、规则、示例和排障内容。
- 相似度分数、领域判断、排序信息和证据来源。
- 代码位置，如相对路径、起止行号、语言和扩展信息。
- 记忆来源信息，如工具名、会话 ID、消息 ID、时间、标签。
- 文档来源信息，如 source ID、document ID、标题路径、章节 ID、版本和引用位置。
- 统一上下文包，包含代码上下文、历史经验、用户偏好、项目事实和需要参考的文档知识。

### 非目标

- 不直接训练大模型。
- 不修改大模型权重。
- 不负责代码生成，只负责上下文检索、长期记忆检索和上下文组装。
- 当前阶段暂不处理长期记忆的安全和隐私策略，后续需要作为独立能力补齐。

## 3. 总体架构

```text
CLI / MCP Server / Session Hook
      |
      v
Application Service
      |
      +--> SourceReader        读取不同来源的数据
      |       +--> CodebaseSource
      |       +--> ConversationSource
      |       +--> ToolHistorySource
      |       +--> ManualMemorySource
      |       +--> DocumentSource
      |       +--> BasicMemorySource
      |
      +--> Normalizer          统一内容和元数据模型
      +--> Splitter            切分代码、会话和解析后的文档节点
      |       +--> CodeSplitter
      |       +--> DocumentChunker
      |       +--> ConversationSplitter
      |
      +--> DocumentParser      解析不同格式文档结构
      |       +--> MarkdownParser
      |       +--> PDFParser
      |       +--> DOCXParser
      |
      +--> DomainResolver      多信号判断任务领域、经验领域和知识领域
      +--> MemoryExtractor     从历史会话中提取偏好、经验和事实
      +--> KnowledgeExtractor  从文档节点中提取摘要、章节、规则、API 和示例
      +--> Embedder            文本转向量
      +--> ContextStore        存储向量和元数据
      |       +--> VectorStore
      |       +--> Snapshot
      |       +--> SourceCatalog
      |
      +--> ContextSearcher     查询、过滤、去重和重排
      +--> ContextAssembler    合并代码、记忆和文档知识，生成上下文包
      +--> SessionContext      会话启动和提示词级上下文构建
```

现有的 `Scanner`、`Indexer`、`Searcher` 可以先作为代码库场景实现，后续逐步收敛到通用 `SourceReader`、`ContextIndexer` 和 `ContextSearcher`。

## 4. 核心流程

### 4.1 代码库索引流程

```text
IndexCodebase(path)
  -> 校验目录
  -> 计算 codebase scope / namespace
  -> 扫描支持的文件
  -> 读取文件内容
  -> 切分为 code chunk
  -> 归一化为 ContextDocument
  -> 批量生成 embedding
  -> 写入 ContextStore
  -> 保存 snapshot
```

### 4.2 历史会话索引流程

```text
IndexConversation(user, tool, conversation)
  -> 读取会话消息
  -> 生成 conversation scope / source id
  -> 清洗和归一化消息结构
  -> 切分长消息或多轮对话片段
  -> 提取用户偏好、经验、项目事实和决策记录
  -> 将原始片段和提取后的记忆转换为 ContextDocument
  -> 批量生成 embedding
  -> 写入 ContextStore
  -> 保存 source catalog 和 snapshot
```

### 4.3 文档知识索引流程

```text
IndexDocumentSource(scope, source)
  -> 读取 Markdown / PDF / DOCX / 文档源文件
  -> 生成 document scope / source id / document id
  -> 根据文件类型选择 DocumentParser
  -> Parser 解析标题、摘要、正文、页码、章节、表格、代码块和来源引用
  -> 统一转换为 title / summary / body 等文档节点
  -> DocumentChunker 将文档节点切成可向量化 chunk，保留 heading_path
  -> 生成 document_summary / chapter_summary / section_summary / chunk 多层节点
  -> 识别知识类型，如 rule / api / example / troubleshooting / concept
  -> 识别 domain_path、版本、关键词、符号和引用位置
  -> 批量生成 embedding
  -> 写入 ContextStore
  -> 保存 source catalog 和 snapshot
```

文档知识入库不能只做普通行级切块，也不能把 Markdown 当成唯一文档模型。Markdown、PDF、DOCX 等都应该先通过对应 Parser 解析为统一的文档节点，由 Parser 判断哪些内容是标题、摘要、正文、表格、代码块或页码引用，再由通用 DocumentChunker 负责切块和写入存储。书籍、技术手册和 SDK 文档需要保留文档层级，否则检索命中单个 chunk 时容易丢失章节语义。推荐至少保留以下层级：

```text
source
  -> document
    -> chapter
      -> section
        -> subsection
          -> chunk
```

### 4.4 搜索流程

```text
SearchContext(scope, query, filters)
  -> 解析用户提示词，生成 RetrievalPlan
  -> query 生成 embedding
  -> 识别 query 所属任务领域、搜索意图和所需知识类型
  -> 并行搜索代码库、长期记忆、工具历史和文档知识库
  -> 对不同来源按 scope、source_type、domain_path、experience_kind、document_id、heading_path 和 knowledge_kind 过滤
  -> 根据领域匹配情况分配通用经验、专业经验和文档知识的 TopK / token budget
  -> 执行向量召回、关键词召回、metadata 过滤召回和章节层级召回
  -> 对召回结果去重、冲突检测、排序和重排
  -> 返回一次性合并后的代码片段、会话片段、经验记忆和文档知识
```

搜索时文档知识库和现有记忆历史应并存，不互相替代。一次用户提示词触发的上下文构建应同时检索：

1. 当前工作区和代码库上下文。
2. 用户全局长期记忆，如偏好、事实和通用经验。
3. 同工具或同项目历史会话经验。
4. 领域专业经验。
5. 文档知识库，如书籍、规范、SDK 文档、项目文档和 Basic Memory 外部 source。

### 4.5 提示词级上下文构建流程

```text
BuildPromptContext(session, prompt)
  -> 收集当前工作区、当前文件、工具名、用户输入和会话阶段
  -> 解析 RetrievalPlan：领域、意图、来源范围、知识类型、预算和过滤条件
  -> 并行搜索 codebase / memory / tool_history / document_knowledge
  -> 对文档命中结果补充父级 section summary、相邻 chunk 和 heading_path
  -> 提取稳定偏好、项目事实、历史决策、领域经验和文档依据
  -> 检测来源冲突和版本冲突，按优先级选择最终依据
  -> 按 token budget 压缩并组装统一上下文包
  -> 返回给 AI 助手作为本轮提示词的补充上下文
```

上下文包建议按 AI 使用顺序组织：

1. 当前任务判断、领域和检索计划摘要。
2. 用户和项目强规则。
3. 当前代码相关上下文。
4. 历史偏好、项目事实和经验。
5. 文档知识结论、API 细节、示例和排障内容。
6. 风险、冲突和版本说明。
7. 来源引用。

### 4.6 清理流程

```text
Clear(scope)
  -> 删除 ContextStore 中对应 scope 的向量和元数据
  -> 删除本地 snapshot
  -> 更新 source catalog
```

## 5. 存储设计

### 5.1 ContextDocument

`ContextDocument` 是面向代码库和长期记忆的统一文档模型。当前 `VectorDocument` 可以作为其早期实现，后续需要逐步补齐通用字段：

```text
id                唯一 ID
namespace         检索命名空间
scope_type        作用域类型，如 user / workspace / codebase / tool
scope_id          作用域 ID
source_type       来源类型，如 codebase / conversation / tool_history / preference / experience / fact / document
source_id         来源 ID，如代码库路径哈希、会话 ID、工具记录 ID、文档源 ID
chunk_type        片段类型，如 code / message / summary / preference / experience / fact / document_chunk / section_summary
vector            embedding 向量
content           可检索正文
summary           可选摘要
relative_path     代码或文档相对路径
start_line        起始行，适用于代码和文本类文档
end_line          结束行，适用于代码和文本类文档
file_extension    文件扩展名
language          语言或文档格式
tool_name         工具名，如 openclaw
conversation_id   历史会话 ID
message_id        消息 ID
role              消息角色，如 user / assistant / summary
importance        记忆重要性
document_id       文档 ID
section_id        章节 ID
heading_path      标题路径，如 Errors > Wrapping errors
knowledge_kind    知识类型，如 rule / api / example / troubleshooting / concept
version           文档版本或来源版本
keywords          关键词
symbols           API、函数名、配置项、错误码等精确符号
created_at        原始内容创建时间
updated_at        索引更新时间
tags              标签
experience_kind   经验类型，如 general / domain / project / tool
domain_path       动态树形领域路径，如 programming/go / database/milvus
domain_confidence 领域识别置信度
metadata          扩展元数据
```

### 5.2 Scope 与 Namespace

现有 namespace 基于代码库绝对路径生成，只适合代码库隔离。长期记忆场景需要引入通用 scope：

```text
scope_type=user       用户全局长期记忆
scope_type=workspace  工作区级记忆
scope_type=codebase   单代码库索引
scope_type=tool       某个工具的历史记录
```

推荐 namespace 生成规则：

```text
namespace = context_<scope_type>_<hash(scope_id)>
```

其中 `scope_id` 可以是用户 ID、工作区路径、代码库绝对路径、工具名和用户 ID 的组合。

### 5.3 Snapshot

本地记录索引状态：

```text
scope_type      作用域类型
scope_id        作用域 ID
source_type     来源类型
source_id       来源 ID
namespace       索引命名空间
status          indexed / indexing / failed
indexed_items   已索引来源条目数
total_chunks    chunk 数
updated_at      更新时间
error_message   失败原因
```

### 5.4 SourceCatalog

`SourceCatalog` 记录已接入的数据源，支持增量索引和会话启动自动检索：

```text
source_type      来源类型
source_id        来源 ID
scope_type       作用域类型
scope_id         作用域 ID
tool_name        工具名
version          来源版本或更新时间
indexed_at       最近索引时间
checksum         内容校验摘要
status           indexed / indexing / failed
metadata         扩展元数据
```

## 6. 当前 MVP 范围

当前先实现最小闭环：

- CLI 工具。
- 本地代码库扫描。
- 通用行级代码切块。
- 哈希 embedder。
- OpenAI-compatible embedder。
- 本地 JSON vector store。
- Milvus vector store。
- 状态 snapshot。
- Markdown 文档解析、文档源读取和通用文档节点切块。
- `import knowledge` 文档知识导入入口，按 source namespace 写入 Markdown 文档 source。
- `import memory` 长期记忆导入入口，可导入 JSON / JSONL 格式的历史会话、经验、偏好、工具历史和事实 source。
- `source list` / `source clear` source 管理入口，可查看和清理已导入 source。
- `search` 统一搜索入口，默认返回代码、知识文档、历史会话、经验和用户偏好等全部类型的 JSON 结果，也可通过参数只搜索指定类型；支持传入或自动生成 `session_id`，并基于会话历史过滤已返回结果。

当前 MVP 以代码库索引、Markdown 文档知识 source 和 JSON/JSONL 长期记忆 source 为主。后续需要继续完善 `SessionContext` 自动上下文组装和 MCP 工具入口。

## 7. 长期记忆扩展策略

### 7.1 数据源类型

优先支持以下数据源：

1. `codebase`：本地代码库。
2. `conversation`：历史会话原文。
3. `tool_history`：openclaw 等工具的调用历史和产物。
4. `preference`：用户稳定偏好。
5. `experience`：历史问题解决经验。
6. `fact`：项目事实、架构决策和环境信息。
7. `document`：书籍、说明文档、技术文档、SDK 文档和项目规范。
8. `external_knowledge`：Basic Memory 等外部知识库导入的 source。

### 7.2 记忆提取策略

历史会话不应只按原文做向量索引，还应提取结构化记忆：

- 用户偏好：例如日志风格、代码审查偏好、测试习惯。
- 通用经验：例如排查顺序、日志记录习惯、代码审查原则、任务拆解方法。
- 领域专业经验：例如 Go 并发排查、Milvus collection 维护、Kubernetes 发布问题、前端构建优化。
- 项目事实：例如仓库结构、服务配置、部署环境。
- 问题经验：例如某类报错的解决方案。
- 决策记录：例如为何选择某种架构或依赖。
- 待办和约束：例如后续必须兼容的接口或限制。

### 7.3 经验维度与领域检索策略

该需求可以实现，且适合作为本项目和通用 Markdown 知识库的差异化能力。实现上不需要额外训练模型，先通过元数据标注、规则识别、关键词分类和 embedding 检索组合完成；后续可接入 LLM 或轻量分类器提升领域识别质量。

经验记忆需要增加以下维度：

```text
experience_kind = general | domain | project | tool
domain          = golang | milvus | kubernetes | frontend | database | testing | deployment | ...
domain_confidence = 0.0 ~ 1.0
```

领域模型：

1. 领域不是固定枚举，而是动态创建的树形路径，类似标签。
2. 当前实现只要求支持两级领域，例如 `programming/go`、`database/milvus`、`medicine/cardiology`。
3. 一级领域表示大类，如 `programming`、`database`、`medicine`。
4. 二级领域表示细分专业方向，如 `go`、`milvus`、`cardiology`。
5. 后续可将路径扩展为三级或四级，例如 `programming/backend/go`。

检索策略：

1. 对 query、当前工作区、当前文件扩展名、工具名和 source metadata 做领域候选识别。
2. 同时识别一级领域和二级领域，二级领域命中时自动包含其父级领域。
3. 如果无法判断领域，则按通用经验和全局经验检索。
4. 如果只判断出一级领域，则增加该一级领域下所有经验的召回，但详细度低于二级精确命中。
5. 如果判断出二级领域，则提高该二级领域专业经验的召回数量和 token budget，同时召回父级领域经验。
6. 同时保留少量通用经验，避免专业经验过拟合。
7. 对领域专业经验按层级匹配度、`domain_confidence`、相似度、时间新鲜度和成功次数重排。
8. 会话启动上下文中标明经验来源是通用经验还是领域专业经验，并展示领域路径。

推荐默认预算：

```text
无明确领域：general 70% + project/tool 30%
明确领域：domain 50% + project/tool 30% + general 20%
强领域任务：domain 65% + project/tool 25% + general 10%
```

### 7.4 会话启动与提示词级检索策略

每次会话开始时按以下顺序构建基础上下文：

1. 根据当前工作区搜索 `codebase` 上下文。
2. 根据工具名搜索同工具历史经验，如 `openclaw`。
3. 根据用户输入搜索用户全局 `preference`、`experience` 和 `fact`。
4. 根据领域识别结果提高匹配领域专业经验的检索详细度。
5. 搜索与当前任务相关的 `document` 和 `external_knowledge` source。
6. 对结果去重、重排和压缩。
7. 输出稳定偏好、通用经验、领域专业经验、文档知识和当前代码上下文。

每次用户发送提示词时，还应基于提示词重新构建一份更贴近当前任务的上下文包。该上下文包不是只搜索历史记忆，也不是只搜索文档知识库，而是并行搜索二者并一次返回。推荐流程：

```text
Prompt
  -> RetrievalPlan
  -> SearchCodebase
  -> SearchMemory
  -> SearchToolHistory
  -> SearchDocumentKnowledge
  -> Rerank / ConflictCheck / Compress
  -> UnifiedPromptContext
```

本地检索不要求所有记忆写入一个大文件。`ContextSearcher` 应先通过 `SourceCatalog` 找到候选 source shard，再并发检索多个 `vectors.json`，最后执行全局合并和重排。文档知识库 source shard 与历史记忆 source shard 共享统一检索入口，但通过 `source_type`、`knowledge_kind`、`document_id`、`heading_path` 和 `domain_path` 做精确过滤。

### 7.5 文档知识库检索与上下文组织策略

文档知识库的目标不是简单地把长文档切成若干向量块，而是根据本轮提示词补充正确、足量且可追溯的知识上下文。搜索前应先生成 `RetrievalPlan`：

```text
RetrievalPlan
  query              原始用户提示词或改写后的检索 query
  intent             coding / design / debugging / explanation / review
  domain_filters    领域路径过滤，如 programming/go
  source_filters    候选 source 范围
  document_filters  候选 document 范围
  kind_filters      rule / api / example / troubleshooting / concept
  required_keywords API、函数名、配置项、错误码等精确词
  budget            各来源 token 和 TopK 预算
```

文档检索应采用多阶段策略：

1. 先用 `DomainResolver` 判断任务领域，并确定是否需要强过滤到具体领域或父领域。
2. 使用向量检索召回语义相关内容。
3. 使用关键词或 BM25 召回 API 名、函数名、配置项、错误码等精确符号。
4. 使用 metadata 过滤限定 `source_id`、`document_id`、`knowledge_kind`、`heading_path`、`version` 和 `domain_path`。
5. 命中 chunk 后，补充其父级 `section_summary`、必要的 `chapter_summary`、前后相邻 chunk 和引用路径。
6. 对结果进行 rerank、去重、版本选择和冲突检测。
7. 在 token budget 内压缩为统一上下文包。

长文档入库时推荐同时存储四类节点：

```text
document_summary  整本文档摘要
chapter_summary   章节摘要
section_summary   小节摘要
chunk             原文细节片段
```

提示词级上下文组织应遵循 AI 使用顺序，而不是简单拼接搜索结果：

1. 当前任务判断和领域路径。
2. 用户偏好、项目规范和强约束。
3. 当前代码库相关上下文。
4. 历史经验、项目事实和类似问题处理记录。
5. 文档知识结论、API 细节、规则、示例和排障说明。
6. 冲突或版本差异说明。
7. 来源引用，如 `source_id`、`document_id`、`heading_path` 和版本。

冲突处理优先级建议为：

```text
用户显式指定 > 当前项目规则 > 用户长期偏好 > 官方新版本文档 > 普通知识文档 > 历史会话
```

如果领域强过滤结果不足，应按顺序回退到父领域、同 source 文档范围、最后再放宽到全局高相关结果，但需要降低置信度并在上下文包中标明。

### 7.6 与 claude-context 和 Basic Memory 的差异

相比 `claude-context` 偏代码库索引，本项目的长期目标是 Agent/Coding 上下文记忆引擎：

- `claude-context` 的核心对象是代码库。
- `Agent-Memory` 的核心对象应升级为 context document 和 source shard。
- 代码库是数据源之一，不是唯一数据源。
- 搜索结果不仅返回代码位置，也返回历史经验、偏好、工具轨迹和来源引用。
- MCP 不仅提供代码搜索，也提供会话启动上下文构建。

相比 `Basic Memory` 偏 Markdown 笔记和轻量知识图谱，本项目不优先实现笔记编辑器能力，而是优先服务 Agent/Coding 工作流：

- `Basic Memory` 的主源是 Markdown 文件。
- `Agent-Memory` 的主源是代码库、历史会话、工具轨迹和 source shard。
- `Basic Memory` 适合人机共写知识库。
- `Agent-Memory` 适合自动沉淀调试经验、工具调用记录、领域专业经验和会话启动上下文。
- 后续可以实现 `BasicMemorySource`，将 Basic Memory 的 Markdown 项目作为外部 source 导入。

## 8. 后续演进

1. 抽象 `ContextDocument`，兼容当前 `VectorDocument`。
2. 抽象 `Scope`、`Source` 和 source-sharded namespace 生成逻辑。
3. 抽象 `SourceCatalog` 和本地 source manifest。
4. 抽象 `SourceReader`，将代码库扫描迁移为 `CodebaseSource`。
5. 实现 `DocumentSource`，支持书籍、说明文档、SDK 文档和项目规范入库。
6. 扩展 `DocumentParser` 体系，支持 PDF、DOCX 等非 Markdown 文档格式。
7. 将已实现的通用 `DocumentChunker` 接入正式 `DocumentSource` 索引流程，把不同 Parser 输出的标题、摘要和正文节点统一切块并写入存储。
8. 实现 `KnowledgeExtractor`，生成 document / chapter / section summary 以及更准确的 rule / api / example / troubleshooting / concept 等知识类型。
9. 实现 source bundle 导出和导入。
10. 实现 MCP Server。
11. 实现历史会话导入和 `ConversationSource`。
12. 实现 `DomainResolver`，区分通用经验、领域专业经验和文档知识领域。
13. 实现 `BasicMemorySource`，将 Basic Memory Markdown 项目作为外部 source 导入。
14. 实现 `MemoryExtractor`，提取偏好、通用经验、领域专业经验、事实和决策记录。
15. 实现 `ContextAssembler`，并行搜索代码库、长期记忆、工具历史和文档知识库，一次返回统一上下文包。
16. 实现 `SessionContextBuilder`，支持会话开始和提示词到达时自动检索长期记忆与文档知识。
17. 实现 Go AST splitter。
18. 实现多语言 tree-sitter splitter。
19. 实现增量索引和后台同步。
20. 实现 dense + BM25 hybrid search、rerank 和上下文压缩。
