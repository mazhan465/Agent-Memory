# 开发过程记录

## 2026-05-07 初始化设计

### 目标

创建 `go-code-context` 项目，完成整体设计和模块设计，为后续 Go 实现代码库语义检索能力打基础。

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
