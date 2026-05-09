// 文件说明：测试搜索会话级去重逻辑。
// 实现原理：构造搜索结果和临时 session store，验证 result key、content hash 和自动 session id 行为。
// 使用方式：执行 go test ./cmd/code-context 或 go test ./...。
// 注意事项：测试只读写临时目录。
// 交互模块：cmd/code-context/session.go、cmd/code-context/search.go。

package main

import (
	"testing"

	"github.com/mazhan465/Agent-Memory/internal/vectorstore"
)

func TestFilterSessionResultsDedupesReturnedData(t *testing.T) {
	state := newSearchSessionState("session-test")
	results := []categorizedSearchResult{
		testCategorizedResult("namespace-a", "doc-1", "first content"),
		testCategorizedResult("namespace-a", "doc-2", "second content"),
	}

	filtered, deduped := filterSessionResults(&state, results, 10)
	if deduped != 0 || len(filtered) != 2 {
		t.Fatalf("first filter len=%d deduped=%d, want len=2 deduped=0", len(filtered), deduped)
	}
	filtered, deduped = filterSessionResults(&state, results, 10)
	if deduped != 2 || len(filtered) != 0 {
		t.Fatalf("second filter len=%d deduped=%d, want len=0 deduped=2", len(filtered), deduped)
	}
}

func TestFilterSessionResultsDedupesSameContentHash(t *testing.T) {
	state := newSearchSessionState("session-test")
	results := []categorizedSearchResult{
		testCategorizedResult("namespace-a", "doc-1", "same content"),
		testCategorizedResult("namespace-b", "doc-2", " same\ncontent "),
	}

	filtered, deduped := filterSessionResults(&state, results, 10)
	if len(filtered) != 1 || deduped != 1 {
		t.Fatalf("filter len=%d deduped=%d, want len=1 deduped=1", len(filtered), deduped)
	}
}

func TestFilterSessionResultsOnlyRecordsReturnedResults(t *testing.T) {
	state := newSearchSessionState("session-test")
	results := []categorizedSearchResult{
		testCategorizedResult("namespace-a", "doc-1", "first content"),
		testCategorizedResult("namespace-a", "doc-2", "second content"),
	}

	filtered, deduped := filterSessionResults(&state, results, 1)
	if len(filtered) != 1 || deduped != 0 {
		t.Fatalf("filter len=%d deduped=%d, want len=1 deduped=0", len(filtered), deduped)
	}
	if len(state.ReturnedResultKeys) != 1 {
		t.Fatalf("len(ReturnedResultKeys) = %d, want 1", len(state.ReturnedResultKeys))
	}
}

func TestSearchSessionStoreGeneratesAndPersistsSession(t *testing.T) {
	store := newSearchSessionStore(t.TempDir())
	state, err := store.LoadOrCreate("")
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}
	if state.ID == "" {
		t.Fatal("generated session ID is empty")
	}
	state.ReturnedResultKeys = append(state.ReturnedResultKeys, "result-1")
	if err := store.Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := store.LoadOrCreate(state.ID)
	if err != nil {
		t.Fatalf("LoadOrCreate(existing) error = %v", err)
	}
	if len(loaded.ReturnedResultKeys) != 1 || loaded.ReturnedResultKeys[0] != "result-1" {
		t.Fatalf("loaded keys = %+v, want [result-1]", loaded.ReturnedResultKeys)
	}
}

func TestParseSearchArgsAcceptsSessionID(t *testing.T) {
	application := &app{}
	application.config.SearchLimit = 8
	options, err := application.parseSearchArgs([]string{".", "query", "5", "knowledge", "session-1"})
	if err != nil {
		t.Fatalf("parseSearchArgs() error = %v", err)
	}
	if options.SessionID != "session-1" || options.Limit != 5 {
		t.Fatalf("options = %+v, want session-1 limit=5", options)
	}
	options, err = application.parseSearchArgs([]string{".", "query", "5", "session-2"})
	if err != nil {
		t.Fatalf("parseSearchArgs(positional session) error = %v", err)
	}
	if options.SessionID != "session-2" || options.SearchTypes()[0] != "all" {
		t.Fatalf("options = %+v, want session-2 all", options)
	}
}

func (o searchRequestOptions) SearchTypes() []string {
	return o.Selection.Labels
}

func testCategorizedResult(namespace string, id string, content string) categorizedSearchResult {
	return categorizedSearchResult{
		Result: vectorstore.SearchResult{
			Document: vectorstore.Document{ID: id, Content: content},
		},
		Namespace: namespace,
	}
}
