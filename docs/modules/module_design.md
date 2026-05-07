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
- 只扫描受支持的扩展名。
- 返回绝对路径和相对路径。

## 3. `internal/splitter`

### 职责

把源文件切分成适合 embedding 的代码片段。

### MVP 实现

- 行级切块。
- 支持 `MaxLines` 和 `OverlapLines`。
- 保留起止行号。

### 后续扩展

- Go AST splitter。
- tree-sitter 多语言 AST splitter。

## 4. `internal/embed`

### 职责

将文本转换为向量。

### MVP 实现

- `HashEmbedder`：本地哈希向量，用于验证完整流程。

### 后续扩展

- OpenAI-compatible embedding。
- Ollama embedding。
- Gemini / VoyageAI embedding。

## 5. `internal/vectorstore`

### 职责

抽象向量存储能力。

### MVP 实现

- `LocalStore`：JSON 文件持久化。
- 余弦相似度 TopK 检索。

### 后续扩展

- Milvus dense vector store。
- Milvus hybrid vector store。

## 6. `internal/indexer`

### 职责

编排索引流程。

### 交互模块

- `scanner`
- `splitter`
- `embed`
- `vectorstore`
- `snapshot`

## 7. `internal/searcher`

### 职责

编排搜索流程。

### 交互模块

- `embed`
- `vectorstore`
- `snapshot`

## 8. `internal/snapshot`

### 职责

保存和读取索引状态。

### MVP 存储

- 默认目录：`~/.go-code-context/snapshots`
- 每个代码库一个 JSON 文件

## 9. `internal/mcpserver`

### 职责

后续提供 MCP Server 封装。

### 计划工具

- `index_codebase`
- `search_code`
- `clear_index`
- `get_indexing_status`

## 10. `cmd/code-context`

### 职责

提供 CLI 入口。

### 命令

- `index <path>`
- `search <path> <query>`
- `status <path>`
- `clear <path>`
