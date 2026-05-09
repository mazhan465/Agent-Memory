// 文件说明：测试通用上下文文档模型的校验和稳定命名逻辑。
// 实现原理：构造 Scope、Source 和 Manifest，验证 namespace、source shard namespace 与稳定 ID 的确定性。
// 使用方式：执行 go test ./internal/contextdoc 或 go test ./...。
// 注意事项：测试只覆盖纯模型逻辑，不依赖外部存储和网络。
// 交互模块：internal/contextdoc。

package contextdoc

import "testing"

func TestNamespaceForScope(t *testing.T) {
	scope := Scope{Type: ScopeTypeUser, ID: "user-1"}

	first, err := NamespaceForScope(scope)
	if err != nil {
		t.Fatalf("NamespaceForScope() error = %v", err)
	}
	second, err := NamespaceForScope(scope)
	if err != nil {
		t.Fatalf("NamespaceForScope() second error = %v", err)
	}
	if first != second {
		t.Fatalf("NamespaceForScope() is not stable: %s != %s", first, second)
	}
	if first == "" || first[:13] != "context_user_" {
		t.Fatalf("NamespaceForScope() = %s, want context_user_ prefix", first)
	}
}

func TestNamespaceForSource(t *testing.T) {
	scope := Scope{Type: ScopeTypeUser, ID: "user-1"}
	conversation := Source{Type: SourceTypeConversation, ID: "conversation-1", ToolName: "openclaw"}
	preference := Source{Type: SourceTypePreference, ID: "global"}

	conversationNamespace, err := NamespaceForSource(scope, conversation)
	if err != nil {
		t.Fatalf("NamespaceForSource() error = %v", err)
	}
	preferenceNamespace, err := NamespaceForSource(scope, preference)
	if err != nil {
		t.Fatalf("NamespaceForSource() preference error = %v", err)
	}
	if conversationNamespace == preferenceNamespace {
		t.Fatalf("NamespaceForSource() namespaces should differ: %s", conversationNamespace)
	}
}

func TestValidateRejectsInvalidValues(t *testing.T) {
	if err := (Scope{Type: ScopeType("unknown"), ID: "user-1"}).Validate(); err == nil {
		t.Fatal("Scope.Validate() error = nil, want unsupported type error")
	}
	if err := (Scope{Type: ScopeTypeUser}).Validate(); err == nil {
		t.Fatal("Scope.Validate() error = nil, want empty id error")
	}
	if err := (Source{Type: SourceType("unknown"), ID: "source-1"}).Validate(); err == nil {
		t.Fatal("Source.Validate() error = nil, want unsupported type error")
	}
	if err := (Source{Type: SourceTypeConversation}).Validate(); err == nil {
		t.Fatal("Source.Validate() error = nil, want empty id error")
	}
}

func TestManifestValidate(t *testing.T) {
	manifest := Manifest{
		Scope:              Scope{Type: ScopeTypeUser, ID: "user-1"},
		Source:             Source{Type: SourceTypeConversation, ID: "conversation-1"},
		Version:            1,
		EmbeddingDimension: 1536,
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Manifest.Validate() error = %v", err)
	}

	manifest.Version = -1
	if err := manifest.Validate(); err == nil {
		t.Fatal("Manifest.Validate() error = nil, want invalid version error")
	}

	manifest.Version = 1
	manifest.KnowledgeCount = -1
	if err := manifest.Validate(); err == nil {
		t.Fatal("Manifest.Validate() error = nil, want invalid knowledge count error")
	}
}

func TestStableID(t *testing.T) {
	first := StableID("memory", "source-1", "message-1")
	second := StableID("memory", "source-1", "message-1")
	third := StableID("memory", "source-1", "message-2")

	if first != second {
		t.Fatalf("StableID() is not stable: %s != %s", first, second)
	}
	if first == third {
		t.Fatalf("StableID() should differ for different inputs: %s", first)
	}
}

func TestContextDocumentExperienceFields(t *testing.T) {
	document := ContextDocument{
		ID:               "doc-1",
		ExperienceKind:   ExperienceKindDomain,
		Domain:           Domain("programming/go"),
		DomainConfidence: 0.8,
	}

	if document.ID != "doc-1" {
		t.Fatalf("ID = %s, want doc-1", document.ID)
	}
	if document.ExperienceKind != ExperienceKindDomain {
		t.Fatalf("ExperienceKind = %s, want %s", document.ExperienceKind, ExperienceKindDomain)
	}
	wantDomain := Domain("programming/go")
	if document.Domain != wantDomain {
		t.Fatalf("Domain = %s, want %s", document.Domain, wantDomain)
	}
	if document.DomainConfidence != 0.8 {
		t.Fatalf("DomainConfidence = %f, want 0.8", document.DomainConfidence)
	}
}

func TestContextDocumentDocumentLocation(t *testing.T) {
	document := ContextDocument{
		ID:        "doc-1",
		ChunkType: ChunkTypeDocumentChunk,
		Source:    Source{Type: SourceTypeDocument, ID: "go-guide"},
		DocumentLocation: &DocumentLocation{
			DocumentID:    "go-guide",
			SectionID:     "errors/wrapping",
			HeadingPath:   "Errors > Wrapping",
			KnowledgeKind: KnowledgeKindRule,
			Version:       "2026-05",
			Keywords:      []string{"error", "wrapping"},
			Symbols:       []string{"fmt.Errorf", "%w"},
		},
	}

	if document.ID != "doc-1" {
		t.Fatalf("ID = %s, want doc-1", document.ID)
	}
	if document.Source.Type != SourceTypeDocument {
		t.Fatalf("Source.Type = %s, want %s", document.Source.Type, SourceTypeDocument)
	}
	if document.ChunkType != ChunkTypeDocumentChunk {
		t.Fatalf("ChunkType = %s, want %s", document.ChunkType, ChunkTypeDocumentChunk)
	}
	if document.DocumentLocation.KnowledgeKind != KnowledgeKindRule {
		t.Fatalf("KnowledgeKind = %s, want %s", document.DocumentLocation.KnowledgeKind, KnowledgeKindRule)
	}
}

func TestDomainPathHelpers(t *testing.T) {
	domain, err := NormalizeDomain(" Programming / Go ")
	if err != nil {
		t.Fatalf("NormalizeDomain() error = %v", err)
	}
	if domain != Domain("programming/go") {
		t.Fatalf("NormalizeDomain() = %s, want programming/go", domain)
	}
	if domain.Depth() != 2 {
		t.Fatalf("Depth() = %d, want 2", domain.Depth())
	}
	if domain.Parent() != Domain("programming") {
		t.Fatalf("Parent() = %s, want programming", domain.Parent())
	}
	if !domain.Match(Domain("programming")) {
		t.Fatal("Match(programming) = false, want true")
	}
	if domain.Match(Domain("database")) {
		t.Fatal("Match(database) = true, want false")
	}
}

func TestNormalizeDomainRejectsInvalidDepth(t *testing.T) {
	_, err := NormalizeDomain("programming/backend/go")
	if err == nil {
		t.Fatal("NormalizeDomain() error = nil, want depth error")
	}
}
