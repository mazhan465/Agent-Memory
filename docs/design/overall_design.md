# go-code-context 整体设计

## 1. 项目目标

`go-code-context` 目标是用 Go 实现一套代码库语义上下文系统，能力参考 `claude-context`，为大模型和 AI 编程助手提供高质量代码检索上下文。

核心目标：

1. 将大型代码库转换为可检索的语义索引。
2. 支持自然语言查询相关代码片段。
3. 通过 CLI 和 MCP Server 暴露能力。
4. 底层支持本地存储和 Milvus 等向量数据库。
5. 后续支持增量索引、混合检索、AST 切块、多 embedding provider。

## 2. 系统边界

### 输入

- 本地代码库路径
- 自然语言查询
- 可选过滤条件，如文件扩展名、路径前缀、TopK

### 输出

- 匹配的代码片段
- 相对路径
- 起止行号
- 相似度分数
- 语言和扩展信息

### 非目标

- 不直接训练大模型。
- 不修改大模型权重。
- 不负责代码生成，只负责上下文检索。

## 3. 总体架构

```text
CLI / MCP Server
      |
      v
Application Service
      |
      +--> Scanner       扫描代码文件
      +--> Splitter      切分代码片段
      +--> Embedder      文本转向量
      +--> VectorStore   存储与检索向量
      +--> Snapshot      记录索引状态
      +--> Searcher      查询与结果整理
```

## 4. 核心流程

### 4.1 索引流程

```text
Index(path)
  -> 校验目录
  -> 计算 collection / namespace
  -> 扫描支持的文件
  -> 读取文件内容
  -> 切分为 chunk
  -> 批量生成 embedding
  -> 写入 VectorStore
  -> 保存 snapshot
```

### 4.2 搜索流程

```text
Search(path, query)
  -> 定位索引 namespace
  -> query 生成 embedding
  -> VectorStore TopK 检索
  -> 过滤、去重、排序
  -> 返回代码片段和位置
```

### 4.3 清理流程

```text
Clear(path)
  -> 删除 VectorStore 中对应 namespace
  -> 删除本地 snapshot
```

## 5. 存储设计

### 5.1 VectorDocument

每个代码片段对应一条向量文档：

```text
id              唯一 ID
namespace       代码库命名空间
vector          embedding 向量
content         代码片段正文
relative_path   相对路径
start_line      起始行
end_line        结束行
file_extension  文件扩展名
language        语言
metadata        扩展元数据
```

### 5.2 Snapshot

本地记录索引状态：

```text
path            代码库绝对路径
namespace       索引命名空间
status          indexed / indexing / failed
indexed_files   已索引文件数
total_chunks    chunk 数
updated_at      更新时间
error_message   失败原因
```

## 6. 当前 MVP 范围

当前先实现最小闭环：

- CLI 工具
- 本地扫描
- 行级 splitter
- 哈希 embedder
- 本地 JSON vector store
- 状态 snapshot

Milvus、OpenAI、MCP Server 会通过接口预留，后续逐步补齐。

## 7. 后续演进

1. 接入 OpenAI / Ollama embedding。
2. 接入 Milvus Go SDK。
3. 实现 MCP Server。
4. 实现 Go AST splitter。
5. 实现多语言 tree-sitter splitter。
6. 实现增量索引和后台同步。
7. 实现 dense + BM25 hybrid search。
