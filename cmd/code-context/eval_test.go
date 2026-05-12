// 文件说明：测试召回质量评估命令的解析和指标计算。
// 实现原理：构造临时评估集和本地向量数据，验证 JSONL 读取、期望结果匹配和汇总指标。
// 使用方式：执行 go test ./cmd/code-context 或 go test ./...。
// 注意事项：测试只读写临时目录，不依赖真实索引或外部向量库。
// 交互模块：cmd/code-context/eval.go、cmd/code-context/search.go、internal/vectorstore。

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mazhan465/Agent-Memory/internal/catalog"
	"github.com/mazhan465/Agent-Memory/internal/config"
	"github.com/mazhan465/Agent-Memory/internal/embed"
	"github.com/mazhan465/Agent-Memory/internal/indexer"
	"github.com/mazhan465/Agent-Memory/internal/vectorstore"
)

func TestReadRecallEvalCasesJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "eval.jsonl")
	content := "{\"id\":\"one\",\"query\":\"OrderRepository\",\"expected\":[{\"relative_path\":\"internal/order/repository.go\"}]}\n" +
		"{\"id\":\"two\",\"query\":\"cache\",\"expected_results\":[{\"content_contains\":\"cache hit\"}]}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cases, err := readRecallEvalCases(path)
	if err != nil {
		t.Fatalf("readRecallEvalCases() error = %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("len(cases) = %d, want 2", len(cases))
	}
	if cases[1].expectedResults()[0].ContentContains != "cache hit" {
		t.Fatalf("second expected = %+v, want content_contains", cases[1].expectedResults())
	}
}

func TestReadRecallEvalCasesSingleJSONLObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "single.jsonl")
	content := `{"id":"one","query":"OrderRepository","expected":[{"relative_path":"internal/order/repository.go"}]}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cases, err := readRecallEvalCases(path)
	if err != nil {
		t.Fatalf("readRecallEvalCases() error = %v", err)
	}
	if len(cases) != 1 || cases[0].caseID() != "one" {
		t.Fatalf("cases = %+v, want one JSONL case", cases)
	}
}

func TestReadRecallEvalCasesSupportsSWEBenchDataset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swe.json")
	content := `{
		"instances":[
			{
				"instance_id":"django__django-14170",
				"problem_statement":"YearLookup breaks iso_year filtering",
				"patch":"diff --git a/django/db/models/lookups.py b/django/db/models/lookups.py\n--- a/django/db/models/lookups.py\n+++ b/django/db/models/lookups.py\n@@ -1 +1 @@\n"
			}
		]
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cases, err := readRecallEvalCases(path)
	if err != nil {
		t.Fatalf("readRecallEvalCases() error = %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("len(cases) = %d, want 1", len(cases))
	}
	if cases[0].caseID() != "django__django-14170" || cases[0].queryText() == "" {
		t.Fatalf("case = %+v, want instance id and problem statement", cases[0])
	}
	if got := cases[0].expectedResults()[0].RelativePath; got != "django/db/models/lookups.py" {
		t.Fatalf("oracle file = %s, want django/db/models/lookups.py", got)
	}
}

func TestEvaluateRecallUsesSearchResults(t *testing.T) {
	ctx := context.Background()
	storageDir := t.TempDir()
	repoPath := t.TempDir()
	store := vectorstore.NewLocalStore(storageDir)
	namespace, _, err := indexer.NamespaceForPath(repoPath)
	if err != nil {
		t.Fatalf("NamespaceForPath() error = %v", err)
	}
	documents := []vectorstore.Document{
		{
			ID:           "generic-doc",
			Namespace:    namespace,
			Vector:       make([]float32, 8),
			Content:      "generic repository implementation",
			RelativePath: "internal/repository/generic.go",
			Metadata: map[string]string{
				"symbol_name": "GenericRepository",
			},
		},
		{
			ID:           "order-doc",
			Namespace:    namespace,
			Vector:       make([]float32, 8),
			Content:      "order service implementation",
			RelativePath: "internal/order/repository.go",
			Metadata: map[string]string{
				"symbol_name": "OrderRepository",
			},
		},
	}
	if err := store.Put(ctx, namespace, documents); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	casesPath := filepath.Join(t.TempDir(), "eval.json")
	casesContent := `[
		{
			"id":"order-repository",
			"query":"OrderRepository",
			"expected":[{"relative_path":"internal/order/repository.go"}]
		}
	]`
	if err := os.WriteFile(casesPath, []byte(casesContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	application := &app{
		config: config.Config{
			StorageDir:         storageDir,
			SearchLimit:        5,
			DefaultSearchTypes: []string{"code"},
			SearchStrategies: map[string]config.SearchStrategy{
				"default": {SemanticWeight: 0, KeywordWeight: 1},
				"code":    {SemanticWeight: 0, KeywordWeight: 1},
			},
		},
		embedder:     embed.NewHashEmbedder(8),
		vectorStore:  store,
		catalogStore: catalog.NewStore(storageDir),
	}

	response, err := application.evaluateRecall(ctx, recallEvalRequest{
		RootPath:  repoPath,
		CasesPath: casesPath,
		Limit:     2,
		Types:     "code",
	})
	if err != nil {
		t.Fatalf("evaluateRecall() error = %v", err)
	}
	if response.CaseCount != 1 || response.PassedCount != 1 || response.FailedCount != 0 {
		t.Fatalf("response counts = %+v, want one passed case", response)
	}
	if response.HitRate != 1 || response.MRR != 1 || response.MeanRecall != 1 || response.MeanNDCG != 1 {
		t.Fatalf("metrics = hit=%f mrr=%f recall=%f ndcg=%f, want all 1",
			response.HitRate, response.MRR, response.MeanRecall, response.MeanNDCG)
	}
	if response.MeanPrecision != 0.5 || response.MeanFilePrecision != 0.5 || response.MeanFileRecall != 1 {
		t.Fatalf("precision metrics = %+v, want generic/file precision 0.5 and file recall 1", response)
	}
	if response.TotalResultCount != 2 || response.MeanResultCount != 2 || response.TotalDedupedCount != 0 {
		t.Fatalf("result efficiency metrics = %+v, want two results and no dedupe", response)
	}
	if response.TotalResultCharacters != 61 || response.TotalEstimatedResultTokens != 16 {
		t.Fatalf("context budget metrics chars=%d tokens=%d, want 61/16",
			response.TotalResultCharacters, response.TotalEstimatedResultTokens)
	}
	if response.MeanSearchedNamespaces != 1 || response.TotalLatencyMS < 0 || response.P95LatencyMS < 0 {
		t.Fatalf("runtime metrics = %+v, want one namespace and non-negative latency", response)
	}
	if response.Cases[0].FirstHitRank != 1 || response.Cases[0].TopResults[0].RelativePath != "internal/order/repository.go" {
		t.Fatalf("case result = %+v, want order repository at rank 1", response.Cases[0])
	}
	if response.Cases[0].ResultCharacters != 61 || response.Cases[0].EstimatedResultTokens != 16 ||
		response.Cases[0].SearchedNamespaceCount != 1 || response.Cases[0].TopResults[0].EstimatedTokens != 7 {
		t.Fatalf("case efficiency metrics = %+v, want chars/tokens/namespaces populated", response.Cases[0])
	}
	if _, err := os.Stat(filepath.Join(storageDir, "sessions")); !os.IsNotExist(err) {
		t.Fatalf("sessions dir stat error = %v, want not exist because eval disables sessions", err)
	}
}

func TestMatchesExpectedResultUsesLineOverlapAndMetadata(t *testing.T) {
	result := searchJSONResult{
		Location: searchJSONLocation{RelativePath: "internal/order/repository.go", StartLine: 10, EndLine: 20},
		Metadata: map[string]string{"symbol_name": "OrderRepository"},
	}
	expected := recallExpectedResult{
		RelativePath: "internal/order/repository.go",
		StartLine:    15,
		EndLine:      16,
		Metadata:     map[string]string{"symbol_name": "OrderRepository"},
	}
	if !matchesExpectedResult(result, expected) {
		t.Fatal("matchesExpectedResult() = false, want true")
	}
	expected.Metadata["symbol_name"] = "OtherRepository"
	if matchesExpectedResult(result, expected) {
		t.Fatal("matchesExpectedResult() = true, want false for metadata mismatch")
	}
}
