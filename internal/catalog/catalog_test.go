// 文件说明：测试 SourceCatalog 本地持久化能力。
// 实现原理：使用临时目录写入 catalog/sources.json，验证 upsert、覆盖、过滤、读取和删除行为。
// 使用方式：执行 go test ./internal/catalog 或 go test ./...。
// 注意事项：测试不依赖外部服务，不读写用户真实目录。
// 交互模块：internal/catalog、internal/contextdoc。

package catalog

import (
	"context"
	"testing"
	"time"

	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
)

func TestStoreUpsertGetListDelete(t *testing.T) {
	ctx := context.Background()
	store := NewStore(t.TempDir())
	entry := testEntry("user-1", contextdoc.SourceTypeConversation, "conversation-1", "openclaw")

	if err := store.Upsert(ctx, entry); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	got, ok, err := store.Get(ctx, entry.Scope, entry.Source)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if got.Source.ID != entry.Source.ID || got.DocumentCount != entry.DocumentCount {
		t.Fatalf("Get() = %+v, want %+v", got, entry)
	}

	entries, err := store.List(ctx, Filter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("List() len = %d, want 1", len(entries))
	}

	if err := store.Delete(ctx, entry.Scope, entry.Source); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	_, ok, err = store.Get(ctx, entry.Scope, entry.Source)
	if err != nil {
		t.Fatalf("Get() after delete error = %v", err)
	}
	if ok {
		t.Fatal("Get() after delete ok = true, want false")
	}
}

func TestStoreUpsertReplacesEntry(t *testing.T) {
	ctx := context.Background()
	store := NewStore(t.TempDir())
	entry := testEntry("user-1", contextdoc.SourceTypeConversation, "conversation-1", "openclaw")
	if err := store.Upsert(ctx, entry); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	entry.Version = 2
	entry.Checksum = "sha256:updated"
	entry.DocumentCount = 9
	if err := store.Upsert(ctx, entry); err != nil {
		t.Fatalf("Upsert() replace error = %v", err)
	}
	entries, err := store.List(ctx, Filter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("List() len = %d, want 1", len(entries))
	}
	if entries[0].Version != 2 || entries[0].DocumentCount != 9 {
		t.Fatalf("entry was not replaced: %+v", entries[0])
	}
}

func TestStoreListFilters(t *testing.T) {
	ctx := context.Background()
	store := NewStore(t.TempDir())
	entries := []Entry{
		testEntry("user-1", contextdoc.SourceTypeConversation, "conversation-1", "openclaw"),
		testEntry("user-1", contextdoc.SourceTypePreference, "global", ""),
		testEntry("user-2", contextdoc.SourceTypeConversation, "conversation-2", "openclaw"),
	}
	for _, entry := range entries {
		if err := store.Upsert(ctx, entry); err != nil {
			t.Fatalf("Upsert() error = %v", err)
		}
	}

	got, err := store.List(ctx, Filter{Scope: contextdoc.Scope{Type: contextdoc.ScopeTypeUser, ID: "user-1"}})
	if err != nil {
		t.Fatalf("List() scope filter error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("scope filtered len = %d, want 2", len(got))
	}

	got, err = store.List(ctx, Filter{SourceType: contextdoc.SourceTypePreference})
	if err != nil {
		t.Fatalf("List() source type filter error = %v", err)
	}
	if len(got) != 1 || got[0].Source.ID != "global" {
		t.Fatalf("source type filtered entries = %+v", got)
	}

	got, err = store.List(ctx, Filter{ToolName: "openclaw"})
	if err != nil {
		t.Fatalf("List() tool filter error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("tool filtered len = %d, want 2", len(got))
	}
}

func TestEntryFromManifest(t *testing.T) {
	manifest := contextdoc.Manifest{
		Scope:              contextdoc.Scope{Type: contextdoc.ScopeTypeUser, ID: "user-1"},
		Source:             contextdoc.Source{Type: contextdoc.SourceTypeConversation, ID: "conversation-1"},
		Version:            1,
		Checksum:           "sha256:abc",
		EmbeddingProvider:  "openai",
		EmbeddingModel:     "text-embedding-3-small",
		EmbeddingDimension: 1536,
		DocumentCount:      7,
		MemoryCount:        3,
		KnowledgeCount:     5,
	}

	entry, err := EntryFromManifest(manifest, "")
	if err != nil {
		t.Fatalf("EntryFromManifest() error = %v", err)
	}
	if entry.Status != StatusIndexed {
		t.Fatalf("status = %s, want %s", entry.Status, StatusIndexed)
	}
	if entry.IndexedAt.IsZero() {
		t.Fatal("IndexedAt is zero")
	}
	if entry.KnowledgeCount != 5 {
		t.Fatalf("KnowledgeCount = %d, want 5", entry.KnowledgeCount)
	}
}

func TestEntryValidateRejectsInvalidValues(t *testing.T) {
	entry := testEntry("user-1", contextdoc.SourceTypeConversation, "conversation-1", "openclaw")
	entry.Status = Status("unknown")
	if err := entry.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want unsupported status error")
	}

	entry = testEntry("user-1", contextdoc.SourceTypeConversation, "conversation-1", "openclaw")
	entry.DocumentCount = -1
	if err := entry.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid document count error")
	}

	entry = testEntry("user-1", contextdoc.SourceTypeDocument, "go-guide", "")
	entry.KnowledgeCount = -1
	if err := entry.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid knowledge count error")
	}
}

func testEntry(scopeID string, sourceType contextdoc.SourceType, sourceID string, toolName string) Entry {
	return Entry{
		Scope:         contextdoc.Scope{Type: contextdoc.ScopeTypeUser, ID: scopeID},
		Source:        contextdoc.Source{Type: sourceType, ID: sourceID, ToolName: toolName},
		Version:       1,
		Checksum:      "sha256:abc",
		IndexedAt:     time.Date(2026, 5, 8, 15, 0, 0, 0, time.UTC),
		Status:        StatusIndexed,
		DocumentCount: 3,
		MemoryCount:   1,
	}
}
