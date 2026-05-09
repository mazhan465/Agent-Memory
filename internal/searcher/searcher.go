// 文件说明：编排代码库语义搜索流程。
// 实现原理：根据代码库路径定位 namespace，将查询文本向量化后调用 VectorStore 检索并返回排序结果。
// 使用方式：CLI search 命令创建 Searcher 后调用 Search 方法。
// 注意事项：当前搜索质量依赖 HashEmbedder，主要用于验证链路；接入真实 embedding 后语义效果会明显提升。
// 交互模块：internal/embed、internal/indexer、internal/vectorstore、internal/snapshot。

// Package searcher 编排代码库搜索流程。
package searcher

import (
	"context"

	"github.com/mazhan465/Agent-Memory/internal/domain"
	"github.com/mazhan465/Agent-Memory/internal/embed"
	"github.com/mazhan465/Agent-Memory/internal/indexer"
	"github.com/mazhan465/Agent-Memory/internal/vectorstore"
)

// Searcher 负责执行语义检索。
type Searcher struct {
	embedder       embed.Embedder
	vectorStore    vectorstore.VectorStore
	domainResolver *domain.DomainResolver
}

// New 创建搜索器。
func New(embedder embed.Embedder, vectorStore vectorstore.VectorStore) *Searcher {
	return NewWithDomainResolver(embedder, vectorStore, domain.NewDefaultDomainResolver())
}

// NewWithDomainResolver 创建带自定义领域解析器的搜索器。
func NewWithDomainResolver(embedder embed.Embedder, vectorStore vectorstore.VectorStore, domainResolver *domain.DomainResolver) *Searcher {
	return &Searcher{embedder: embedder, vectorStore: vectorStore, domainResolver: domainResolver}
}

// Search 在指定代码库索引中搜索相关代码片段。
func (s *Searcher) Search(ctx context.Context, rootPath string, query string, options vectorstore.SearchOptions) ([]vectorstore.SearchResult, error) {
	namespace, _, err := indexer.NamespaceForPath(rootPath)
	if err != nil {
		return nil, err
	}

	queryVector, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(options.DomainFilters) == 0 && s.domainResolver != nil {
		decision, err := s.domainResolver.Resolve(ctx, domain.Input{Query: query})
		if err != nil {
			return nil, err
		}
		if decision.Domain != "" {
			options.DomainFilters = append(options.DomainFilters, string(decision.Domain))
			if decision.ParentDomain != "" {
				options.DomainFilters = append(options.DomainFilters, string(decision.ParentDomain))
			}
		}
	}
	return s.vectorStore.Search(ctx, namespace, queryVector, options)
}
