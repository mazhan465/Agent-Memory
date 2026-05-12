// 文件说明：测试本地向量存储过滤能力。
// 实现原理：写入临时 JSON 向量文件，验证领域和文档知识元数据过滤。
// 使用方式：执行 go test ./internal/vectorstore 或 go test ./...。
// 注意事项：测试不依赖外部 Milvus 服务。
// 交互模块：internal/vectorstore。

package vectorstore

import (
	"context"
	"testing"
)

func TestLocalStoreSearchFiltersByDomain(t *testing.T) {
	ctx := context.Background()
	store := NewLocalStore(t.TempDir())
	namespace := "test_namespace"
	documents := []Document{
		{
			ID:        "go-doc",
			Namespace: namespace,
			Vector:    []float32{1, 0},
			Content:   "go goroutine channel",
			Metadata: map[string]string{
				MetadataDomainPath: "programming/go",
			},
		},
		{
			ID:        "milvus-doc",
			Namespace: namespace,
			Vector:    []float32{1, 0},
			Content:   "milvus collection search",
			Metadata: map[string]string{
				MetadataDomainPath: "database/milvus",
			},
		},
	}
	if err := store.Put(ctx, namespace, documents); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	results, err := store.Search(ctx, namespace, []float32{1, 0}, SearchOptions{DomainFilters: []string{"database/milvus"}})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Document.ID != "milvus-doc" {
		t.Fatalf("result ID = %s, want milvus-doc", results[0].Document.ID)
	}
}

func TestLocalStoreSearchUsesKeywordScore(t *testing.T) {
	ctx := context.Background()
	store := NewLocalStore(t.TempDir())
	namespace := "test_namespace"
	documents := []Document{
		{
			ID:           "semantic-only-doc",
			Namespace:    namespace,
			Vector:       []float32{1, 0},
			Content:      "generic repository code",
			RelativePath: "internal/repository/generic.go",
		},
		{
			ID:           "keyword-doc",
			Namespace:    namespace,
			Vector:       []float32{1, 0},
			Content:      "order service implementation",
			RelativePath: "internal/order/repository.go",
			Metadata: map[string]string{
				"symbol_name": "OrderRepository",
				"symbol_kind": "struct",
			},
		},
	}
	if err := store.Put(ctx, namespace, documents); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	results, err := store.Search(ctx, namespace, []float32{1, 0}, SearchOptions{Query: "OrderRepository"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].Document.ID != "keyword-doc" {
		t.Fatalf("first result ID = %s, want keyword-doc", results[0].Document.ID)
	}
	if results[0].Score <= results[1].Score {
		t.Fatalf("keyword score did not improve ranking: first=%f second=%f", results[0].Score, results[1].Score)
	}
}

func TestLocalStoreSearchFiltersByDocumentMetadata(t *testing.T) {
	ctx := context.Background()
	store := NewLocalStore(t.TempDir())
	namespace := "test_namespace"
	documents := []Document{
		{
			ID:        "summary-doc",
			Namespace: namespace,
			Vector:    []float32{1, 0},
			Content:   "summary",
			Metadata: map[string]string{
				MetadataDocumentID:    "go-guide",
				MetadataKnowledgeKind: "concept",
				MetadataNodeKind:      "summary",
			},
		},
		{
			ID:        "api-doc",
			Namespace: namespace,
			Vector:    []float32{1, 0},
			Content:   "api usage",
			Metadata: map[string]string{
				MetadataDocumentID:    "go-guide",
				MetadataSectionID:     "guide/api",
				MetadataHeadingPath:   "Guide > API",
				MetadataKnowledgeKind: "api",
				MetadataNodeKind:      "body",
				MetadataVersion:       "v1",
			},
		},
	}
	if err := store.Put(ctx, namespace, documents); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	results, err := store.Search(ctx, namespace, []float32{1, 0}, SearchOptions{
		DocumentFilters:      []string{"go-guide"},
		SectionFilters:       []string{"guide/api"},
		HeadingFilters:       []string{"Guide > API"},
		KnowledgeKindFilters: []string{"api"},
		NodeKindFilters:      []string{"body"},
		VersionFilters:       []string{"v1"},
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Document.ID != "api-doc" {
		t.Fatalf("result ID = %s, want api-doc", results[0].Document.ID)
	}
}

func TestLocalStoreCodeSearchUsesRRFAndSymbolProfile(t *testing.T) {
	ctx := context.Background()
	store := NewLocalStore(t.TempDir())
	namespace := "test_namespace"
	documents := []Document{
		{
			ID:           "semantic-doc",
			Namespace:    namespace,
			Vector:       []float32{1, 0},
			Content:      "generic repository implementation",
			RelativePath: "internal/repository/generic.go",
		},
		{
			ID:           "symbol-doc",
			Namespace:    namespace,
			Vector:       []float32{0, 1},
			Content:      "order writer implementation",
			RelativePath: "internal/order/writer.go",
			Metadata: map[string]string{
				metadataSymbolName: "OrderWriter",
				metadataSymbolKind: "struct",
			},
		},
	}
	if err := store.Put(ctx, namespace, documents); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	results, err := store.Search(ctx, namespace, []float32{1, 0}, SearchOptions{
		Query:          "OrderWriter",
		SemanticWeight: 0.6,
		KeywordWeight:  0.4,
		KeywordProfile: KeywordProfileCode,
		FusionMode:     SearchFusionRRF,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].Document.ID != "symbol-doc" {
		t.Fatalf("first result ID = %s, want symbol-doc", results[0].Document.ID)
	}
}

func TestLocalStoreConversationSearchKeepsSemanticDominant(t *testing.T) {
	ctx := context.Background()
	store := NewLocalStore(t.TempDir())
	namespace := "test_namespace"
	documents := []Document{
		{
			ID:        "semantic-conversation",
			Namespace: namespace,
			Vector:    []float32{1, 0},
			Content:   "previous discussion about repository design",
		},
		{
			ID:        "keyword-conversation",
			Namespace: namespace,
			Vector:    []float32{0, 1},
			Content:   "OrderRepository OrderRepository OrderRepository",
			Metadata: map[string]string{
				metadataRole: "assistant",
			},
		},
	}
	if err := store.Put(ctx, namespace, documents); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	results, err := store.Search(ctx, namespace, []float32{1, 0}, SearchOptions{
		Query:          "OrderRepository",
		SemanticWeight: 0.85,
		KeywordWeight:  0.15,
		KeywordProfile: KeywordProfileConversation,
		FusionMode:     SearchFusionLinear,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].Document.ID != "semantic-conversation" {
		t.Fatalf("first result ID = %s, want semantic-conversation", results[0].Document.ID)
	}
}
