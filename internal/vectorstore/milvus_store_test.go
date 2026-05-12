// 文件说明：测试 Milvus 向量存储的表达式构建和辅助函数。
// 实现原理：直接调用表达式构建 helper，验证过滤表达式、安全校验和 UTF-8 截断逻辑。
// 使用方式：执行 go test ./internal/vectorstore 或 go test ./...。
// 注意事项：测试不连接真实 Milvus 服务。
// 交互模块：internal/vectorstore。

package vectorstore

import (
	"testing"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

func TestBuildMilvusExpr(t *testing.T) {
	expr, err := buildMilvusExpr("code_chunks_abc123", SearchOptions{ExtensionFilters: []string{".go", ".md"}})
	if err != nil {
		t.Fatalf("buildMilvusExpr() error = %v", err)
	}
	want := `namespace == "code_chunks_abc123" and file_extension in [".go", ".md"]`
	if expr != want {
		t.Fatalf("expr = %s, want %s", expr, want)
	}
}

func TestBuildMilvusExprWithDomainFilter(t *testing.T) {
	expr, err := buildMilvusExpr("code_chunks_abc123", SearchOptions{DomainFilters: []string{"database/milvus", "database"}})
	if err != nil {
		t.Fatalf("buildMilvusExpr() error = %v", err)
	}
	want := `namespace == "code_chunks_abc123" and domain_path in ["database/milvus", "database"]`
	if expr != want {
		t.Fatalf("expr = %s, want %s", expr, want)
	}
}

func TestBuildMilvusExprWithDocumentFilters(t *testing.T) {
	expr, err := buildMilvusExpr("code_chunks_abc123", SearchOptions{
		DocumentFilters:      []string{"go-guide"},
		SectionFilters:       []string{"guide/api"},
		HeadingFilters:       []string{"Guide > API"},
		KnowledgeKindFilters: []string{"api"},
		NodeKindFilters:      []string{"body"},
		VersionFilters:       []string{"v1"},
	})
	if err != nil {
		t.Fatalf("buildMilvusExpr() error = %v", err)
	}
	want := `namespace == "code_chunks_abc123" and document_id in ["go-guide"] and section_id in ["guide/api"] and heading_path in ["Guide > API"] and knowledge_kind in ["api"] and node_kind in ["body"] and version in ["v1"]`
	if expr != want {
		t.Fatalf("expr = %s, want %s", expr, want)
	}
}

func TestBuildMilvusExprRejectsUnsafeValue(t *testing.T) {
	_, err := buildMilvusExpr(`code" or true`, SearchOptions{ExtensionFilters: []string{".go"}})
	if err == nil {
		t.Fatal("buildMilvusExpr() error = nil, want unsafe namespace error")
	}

	_, err = buildMilvusExpr("code_chunks_abc123", SearchOptions{ExtensionFilters: []string{`.go"] or true or file_extension in [".md`}})
	if err == nil {
		t.Fatal("buildMilvusExpr() error = nil, want unsafe extension error")
	}

	_, err = buildMilvusExpr("code_chunks_abc123", SearchOptions{DocumentFilters: []string{`guide"] or true`}})
	if err == nil {
		t.Fatal("buildMilvusExpr() error = nil, want unsafe document filter error")
	}
}

func TestEnsureMilvusSchemaFieldsRejectsMissingField(t *testing.T) {
	schema := entity.NewSchema().WithField(entity.NewField().WithName(milvusFieldDocID))
	if err := ensureMilvusSchemaFields(schema); err == nil {
		t.Fatal("ensureMilvusSchemaFields() error = nil, want missing field error")
	}
}

func TestTruncateUTF8(t *testing.T) {
	value := truncateUTF8("你好abc", 7)
	if value != "你好a" {
		t.Fatalf("truncateUTF8() = %q, want %q", value, "你好a")
	}
}

func TestDocumentDimension(t *testing.T) {
	_, err := documentDimension([]Document{{Vector: []float32{1}}, {Vector: []float32{1, 2}}})
	if err == nil {
		t.Fatal("documentDimension() error = nil, want dimension mismatch error")
	}
}

func TestMilvusSearchLimitExpandsForRerank(t *testing.T) {
	limit := milvusSearchLimit(5, SearchOptions{Query: "OrderWriter", FusionMode: SearchFusionRRF})
	if limit != 15 {
		t.Fatalf("milvusSearchLimit() = %d, want 15", limit)
	}

	limit = milvusSearchLimit(5, SearchOptions{})
	if limit != 5 {
		t.Fatalf("milvusSearchLimit() without query = %d, want 5", limit)
	}
}

func TestDocumentColumnsPreserveSearchMetadata(t *testing.T) {
	columns := columnsByName(documentColumns([]Document{
		{
			ID:            "doc-1",
			Namespace:     "code_chunks_abc123",
			Vector:        []float32{1, 0},
			Content:       "content",
			RelativePath:  "internal/order/writer.go",
			FileExtension: ".go",
			Language:      "go",
			Metadata: map[string]string{
				MetadataExperienceKind: "domain",
				metadataSymbolName:     "OrderWriter",
				metadataSymbolKind:     "struct",
				metadataChunkKind:      "syntax_node",
				metadataRole:           "assistant",
				metadataToolName:       "go test",
				metadataCommand:        "go test ./...",
				metadataStatus:         "success",
				metadataTags:           "go,test",
			},
		},
	}))

	for field, want := range map[string]string{
		milvusFieldExperienceKind: "domain",
		milvusFieldSymbolName:     "OrderWriter",
		milvusFieldSymbolKind:     "struct",
		milvusFieldChunkKind:      "syntax_node",
		milvusFieldRole:           "assistant",
		milvusFieldToolName:       "go test",
		milvusFieldCommand:        "go test ./...",
		milvusFieldStatus:         "success",
		milvusFieldTags:           "go,test",
	} {
		got, err := columnString(columns, field, 0)
		if err != nil {
			t.Fatalf("columnString(%s) error = %v", field, err)
		}
		if got != want {
			t.Fatalf("columnString(%s) = %q, want %q", field, got, want)
		}
	}
}

func TestMilvusSearchResultsRejectsScoreCountMismatch(t *testing.T) {
	_, err := milvusSearchResults([]client.SearchResult{{ResultCount: 1, Scores: nil}})
	if err == nil {
		t.Fatal("milvusSearchResults() error = nil, want score count mismatch")
	}
}
