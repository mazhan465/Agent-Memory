// 文件说明：定义向量存储接口、文档模型和搜索结果模型。
// 实现原理：通过 VectorStore 抽象屏蔽本地 JSON 存储和未来 Milvus 存储的差异。
// 使用方式：索引器调用 Put 写入向量文档，搜索器调用 Search 执行 TopK 检索。
// 注意事项：接口中的 namespace 用于隔离不同代码库索引，不等同于权限隔离。
// 交互模块：internal/indexer、internal/searcher、internal/snapshot。

// Package vectorstore 提供向量存储抽象和本地实现。
package vectorstore

import "context"

// Document 表示一条已向量化的代码片段。
type Document struct {
	ID            string            `json:"id"`
	Namespace     string            `json:"namespace"`
	Vector        []float32         `json:"vector"`
	Content       string            `json:"content"`
	RelativePath  string            `json:"relative_path"`
	StartLine     int               `json:"start_line"`
	EndLine       int               `json:"end_line"`
	FileExtension string            `json:"file_extension"`
	Language      string            `json:"language"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// SearchOptions 表示向量检索参数。
type SearchOptions struct {
	Limit            int
	ExtensionFilters []string
}

// SearchResult 表示向量检索结果。
type SearchResult struct {
	Document Document
	Score    float64
}

// VectorStore 定义向量存储接口。
type VectorStore interface {
	Put(ctx context.Context, namespace string, documents []Document) error
	Search(ctx context.Context, namespace string, queryVector []float32, options SearchOptions) ([]SearchResult, error)
	Clear(ctx context.Context, namespace string) error
	Count(ctx context.Context, namespace string) (int, error)
}
