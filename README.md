# go-code-context

`go-code-context` 是一个使用 Go 实现的代码库语义索引与检索项目，目标能力参考 `claude-context`：扫描代码库、切分代码片段、生成向量、存储到向量存储层，并通过 CLI / MCP 工具为大模型提供代码上下文检索能力。

## 当前阶段

当前版本先实现可运行的 MVP：

- 本地代码库扫描
- 通用行级代码切块
- 哈希向量 Embedder（本地可运行，便于验证流程）
- 本地 JSON 向量存储（便于无 Milvus 环境下开发测试）
- CLI：`index`、`search`、`clear`、`status`
- 模块化接口：后续可替换为 OpenAI/Ollama Embedder 和 Milvus VectorStore

## 项目铁则

详见 `docs/development_rules.md`。核心要求：

1. 所有开发过程必须落到文档中。
2. 每完成一块完整需求，自动提交一次 Git 记录。
3. 所有 Go 文件头部必须有文件说明，包含：简要说明、实现原理、如何使用、注意事项、交互模块。

## 快速使用

```bash
# 编译
make build

# 索引当前项目
./bin/code-context index /path/to/repo

# 搜索代码
./bin/code-context search /path/to/repo "where is authentication handled"

# 查看状态
./bin/code-context status /path/to/repo

# 清理索引
./bin/code-context clear /path/to/repo
```

## 文档

- `docs/design/overall_design.md`：整体设计
- `docs/modules/module_design.md`：模块细化设计
- `docs/development_rules.md`：开发铁则
- `docs/development_log.md`：开发过程记录
