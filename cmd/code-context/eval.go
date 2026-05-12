// 文件说明：提供召回质量评估命令。
// 实现原理：读取 JSON/JSONL/SWE-bench 风格评估集，复用统一搜索入口执行检索，并根据期望结果计算 precision、recall、F1、MRR 和 nDCG。
// 使用方式：runEval 由 CLI eval 命令调用，当前支持 eval recall <path> <cases-json-or-jsonl> [limit] [types]。
// 注意事项：评估搜索会关闭会话去重，避免历史搜索状态影响指标。
// 交互模块：cmd/code-context/search.go、internal/vectorstore。

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const evalRecordLineBufferSize = 1024 * 1024

var patchOracleFileRegexp = regexp.MustCompile(`(?m)^--- a/(.+)$`)

type recallEvalRequest struct {
	RootPath  string
	CasesPath string
	Limit     int
	Types     string
}

type recallEvalDataset struct {
	Instances []recallEvalCase `json:"instances"`
	Cases     []recallEvalCase `json:"cases"`
}

type recallEvalCase struct {
	ID               string                 `json:"id,omitempty"`
	InstanceID       string                 `json:"instance_id,omitempty"`
	Query            string                 `json:"query,omitempty"`
	ProblemStatement string                 `json:"problem_statement,omitempty"`
	Expected         []recallExpectedResult `json:"expected,omitempty"`
	ExpectedResults  []recallExpectedResult `json:"expected_results,omitempty"`
	Oracles          []string               `json:"oracles,omitempty"`
	OracleFiles      []string               `json:"oracle_files,omitempty"`
	Patch            string                 `json:"patch,omitempty"`
	Limit            int                    `json:"limit,omitempty"`
	Types            string                 `json:"types,omitempty"`
	SearchTypes      []string               `json:"search_types,omitempty"`
}

type recallExpectedResult struct {
	ResultID        string            `json:"result_id,omitempty"`
	ContentHash     string            `json:"content_hash,omitempty"`
	Category        string            `json:"category,omitempty"`
	SourceType      string            `json:"source_type,omitempty"`
	SourceID        string            `json:"source_id,omitempty"`
	Namespace       string            `json:"namespace,omitempty"`
	RelativePath    string            `json:"relative_path,omitempty"`
	StartLine       int               `json:"start_line,omitempty"`
	EndLine         int               `json:"end_line,omitempty"`
	DocumentID      string            `json:"document_id,omitempty"`
	SectionID       string            `json:"section_id,omitempty"`
	HeadingPath     string            `json:"heading_path,omitempty"`
	KnowledgeKind   string            `json:"knowledge_kind,omitempty"`
	NodeKind        string            `json:"node_kind,omitempty"`
	Version         string            `json:"version,omitempty"`
	DomainPath      string            `json:"domain_path,omitempty"`
	ContentContains string            `json:"content_contains,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type recallEvalResponse struct {
	RootPath                   string                 `json:"root_path"`
	CasesPath                  string                 `json:"cases_path"`
	Limit                      int                    `json:"limit"`
	Types                      string                 `json:"types"`
	CaseCount                  int                    `json:"case_count"`
	PassedCount                int                    `json:"passed_count"`
	FailedCount                int                    `json:"failed_count"`
	HitRate                    float64                `json:"hit_rate"`
	MeanPrecision              float64                `json:"mean_precision"`
	MeanRecall                 float64                `json:"mean_recall"`
	MeanF1                     float64                `json:"mean_f1"`
	MRR                        float64                `json:"mrr"`
	MeanNDCG                   float64                `json:"mean_ndcg"`
	FileMetricCases            int                    `json:"file_metric_cases,omitempty"`
	MeanFilePrecision          float64                `json:"mean_file_precision,omitempty"`
	MeanFileRecall             float64                `json:"mean_file_recall,omitempty"`
	MeanFileF1                 float64                `json:"mean_file_f1,omitempty"`
	AverageHitRank             float64                `json:"average_hit_rank,omitempty"`
	TotalLatencyMS             float64                `json:"total_latency_ms"`
	MeanLatencyMS              float64                `json:"mean_latency_ms"`
	P95LatencyMS               float64                `json:"p95_latency_ms"`
	TotalEstimatedResultTokens int                    `json:"total_estimated_result_tokens"`
	MeanEstimatedResultTokens  float64                `json:"mean_estimated_result_tokens"`
	TotalResultCharacters      int                    `json:"total_result_characters"`
	MeanResultCharacters       float64                `json:"mean_result_characters"`
	TotalResultCount           int                    `json:"total_result_count"`
	MeanResultCount            float64                `json:"mean_result_count"`
	TotalDedupedCount          int                    `json:"total_deduped_count"`
	MeanSearchedNamespaces     float64                `json:"mean_searched_namespaces"`
	Cases                      []recallEvalCaseResult `json:"cases"`
}

type recallEvalCaseResult struct {
	ID                     string                `json:"id,omitempty"`
	Query                  string                `json:"query"`
	Limit                  int                   `json:"limit"`
	Types                  string                `json:"types"`
	ExpectedCount          int                   `json:"expected_count"`
	MatchedExpected        int                   `json:"matched_expected"`
	MatchedResults         int                   `json:"matched_results"`
	Matched                bool                  `json:"matched"`
	Precision              float64               `json:"precision"`
	Recall                 float64               `json:"recall"`
	F1                     float64               `json:"f1"`
	NDCG                   float64               `json:"ndcg"`
	FilePrecision          float64               `json:"file_precision,omitempty"`
	FileRecall             float64               `json:"file_recall,omitempty"`
	FileF1                 float64               `json:"file_f1,omitempty"`
	FirstHitRank           int                   `json:"first_hit_rank,omitempty"`
	ReciprocalRank         float64               `json:"reciprocal_rank"`
	ResultCount            int                   `json:"result_count"`
	DedupedCount           int                   `json:"deduped_count"`
	LatencyMS              float64               `json:"latency_ms"`
	EstimatedResultTokens  int                   `json:"estimated_result_tokens"`
	ResultCharacters       int                   `json:"result_characters"`
	SearchedNamespace      []searchJSONNamespace `json:"searched_namespaces"`
	SearchedNamespaceCount int                   `json:"searched_namespace_count"`
	TopResults             []recallEvalTopResult `json:"top_results"`
}

type recallEvalTopResult struct {
	Rank            int     `json:"rank"`
	ResultID        string  `json:"result_id"`
	Category        string  `json:"category"`
	SourceType      string  `json:"source_type"`
	SourceID        string  `json:"source_id,omitempty"`
	RelativePath    string  `json:"relative_path,omitempty"`
	Score           float64 `json:"score"`
	ContentChars    int     `json:"content_chars"`
	EstimatedTokens int     `json:"estimated_tokens"`
}

func (a *app) runEval(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: code-context eval recall <path> <cases-json-or-jsonl> [limit] [types]")
	}
	switch args[0] {
	case "recall":
		return a.runEvalRecall(ctx, args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown eval command %q", args[0])
	}
}

func (a *app) runEvalRecall(ctx context.Context, args []string) error {
	request, err := a.parseRecallEvalArgs(args)
	if err != nil {
		return err
	}
	response, err := a.evaluateRecall(ctx, request)
	if err != nil {
		return err
	}
	return printJSON(response)
}

func (a *app) parseRecallEvalArgs(args []string) (recallEvalRequest, error) {
	if len(args) < 2 || len(args) > 4 {
		return recallEvalRequest{}, errors.New("usage: code-context eval recall <path> <cases-json-or-jsonl> [limit] [types]")
	}
	limit := a.config.SearchLimit
	if limit <= 0 {
		limit = 8
	}
	types := ""
	if len(args) >= 3 {
		parsedLimit, err := strconv.Atoi(args[2])
		if err == nil {
			if parsedLimit <= 0 {
				return recallEvalRequest{}, errors.New("limit must be a positive integer")
			}
			limit = parsedLimit
		} else {
			types = args[2]
		}
	}
	if len(args) == 4 {
		if types != "" {
			return recallEvalRequest{}, errors.New("usage: code-context eval recall <path> <cases-json-or-jsonl> [limit] [types]")
		}
		parsedLimit, err := parsePositiveInt(args[2])
		if err != nil {
			return recallEvalRequest{}, errors.New("limit must be a positive integer")
		}
		limit = parsedLimit
		types = args[3]
	}
	absoluteCasesPath, err := filepath.Abs(args[1])
	if err != nil {
		return recallEvalRequest{}, err
	}
	return recallEvalRequest{
		RootPath:  args[0],
		CasesPath: filepath.ToSlash(filepath.Clean(absoluteCasesPath)),
		Limit:     limit,
		Types:     types,
	}, nil
}

func (a *app) evaluateRecall(ctx context.Context, request recallEvalRequest) (recallEvalResponse, error) {
	cases, err := readRecallEvalCases(request.CasesPath)
	if err != nil {
		return recallEvalResponse{}, err
	}
	absoluteRoot, err := filepath.Abs(request.RootPath)
	if err != nil {
		return recallEvalResponse{}, err
	}
	response := recallEvalResponse{
		RootPath:  filepath.ToSlash(filepath.Clean(absoluteRoot)),
		CasesPath: request.CasesPath,
		Limit:     request.Limit,
		Types:     request.typeArg(a.config.DefaultSearchTypes),
		CaseCount: len(cases),
		Cases:     make([]recallEvalCaseResult, 0, len(cases)),
	}
	var precisionSum float64
	var recallSum float64
	var f1Sum float64
	var reciprocalRankSum float64
	var ndcgSum float64
	var filePrecisionSum float64
	var fileRecallSum float64
	var fileF1Sum float64
	var latencySum float64
	var estimatedTokenSum int
	var resultCharacterSum int
	var resultCountSum int
	var dedupedCountSum int
	var searchedNamespaceSum int
	var hitRankSum int
	latencies := make([]float64, 0, len(cases))
	for _, testCase := range cases {
		caseResult, err := a.evaluateRecallCase(ctx, request, testCase)
		if err != nil {
			return recallEvalResponse{}, err
		}
		response.Cases = append(response.Cases, caseResult)
		precisionSum += caseResult.Precision
		recallSum += caseResult.Recall
		f1Sum += caseResult.F1
		reciprocalRankSum += caseResult.ReciprocalRank
		ndcgSum += caseResult.NDCG
		latencySum += caseResult.LatencyMS
		latencies = append(latencies, caseResult.LatencyMS)
		estimatedTokenSum += caseResult.EstimatedResultTokens
		resultCharacterSum += caseResult.ResultCharacters
		resultCountSum += caseResult.ResultCount
		dedupedCountSum += caseResult.DedupedCount
		searchedNamespaceSum += caseResult.SearchedNamespaceCount
		if hasFileExpectations(testCase.expectedResults()) {
			response.FileMetricCases++
			filePrecisionSum += caseResult.FilePrecision
			fileRecallSum += caseResult.FileRecall
			fileF1Sum += caseResult.FileF1
		}
		if caseResult.Matched {
			response.PassedCount++
			hitRankSum += caseResult.FirstHitRank
		}
	}
	response.FailedCount = response.CaseCount - response.PassedCount
	if response.CaseCount > 0 {
		response.HitRate = float64(response.PassedCount) / float64(response.CaseCount)
		response.MeanPrecision = precisionSum / float64(response.CaseCount)
		response.MeanRecall = recallSum / float64(response.CaseCount)
		response.MeanF1 = f1Sum / float64(response.CaseCount)
		response.MRR = reciprocalRankSum / float64(response.CaseCount)
		response.MeanNDCG = ndcgSum / float64(response.CaseCount)
	}
	if response.FileMetricCases > 0 {
		response.MeanFilePrecision = filePrecisionSum / float64(response.FileMetricCases)
		response.MeanFileRecall = fileRecallSum / float64(response.FileMetricCases)
		response.MeanFileF1 = fileF1Sum / float64(response.FileMetricCases)
	}
	if response.PassedCount > 0 {
		response.AverageHitRank = float64(hitRankSum) / float64(response.PassedCount)
	}
	if response.CaseCount > 0 {
		response.TotalLatencyMS = latencySum
		response.MeanLatencyMS = latencySum / float64(response.CaseCount)
		response.P95LatencyMS = percentileFloat64(latencies, 0.95)
		response.TotalEstimatedResultTokens = estimatedTokenSum
		response.MeanEstimatedResultTokens = float64(estimatedTokenSum) / float64(response.CaseCount)
		response.TotalResultCharacters = resultCharacterSum
		response.MeanResultCharacters = float64(resultCharacterSum) / float64(response.CaseCount)
		response.TotalResultCount = resultCountSum
		response.MeanResultCount = float64(resultCountSum) / float64(response.CaseCount)
		response.TotalDedupedCount = dedupedCountSum
		response.MeanSearchedNamespaces = float64(searchedNamespaceSum) / float64(response.CaseCount)
	}
	return response, nil
}

func (a *app) evaluateRecallCase(
	ctx context.Context,
	request recallEvalRequest,
	testCase recallEvalCase,
) (recallEvalCaseResult, error) {
	expected := testCase.expectedResults()
	limit := firstPositive(testCase.Limit, request.Limit)
	types := testCase.typeArg(request.typeArg(a.config.DefaultSearchTypes))
	query := testCase.queryText()
	selection, err := newSearchTypeSelection(types)
	if err != nil {
		return recallEvalCaseResult{}, err
	}
	startedAt := time.Now()
	searchResponse, err := a.searchAll(ctx, searchRequestOptions{
		RootPath:       request.RootPath,
		Query:          query,
		Limit:          limit,
		Selection:      selection,
		DisableSession: true,
	})
	latencyMS := float64(time.Since(startedAt)) / float64(time.Millisecond)
	if err != nil {
		return recallEvalCaseResult{}, err
	}
	return evaluateRecallCaseResult(testCase, expected, query, limit, types, searchResponse, latencyMS), nil
}

func evaluateRecallCaseResult(
	testCase recallEvalCase,
	expected []recallExpectedResult,
	query string,
	limit int,
	types string,
	searchResponse searchJSONResponse,
	latencyMS float64,
) recallEvalCaseResult {
	matchedExpected := make(map[int]struct{}, len(expected))
	matchedResults := make(map[int]struct{}, len(searchResponse.Results))
	firstHitRank := 0
	relevantAtRank := make([]bool, len(searchResponse.Results))
	for resultIndex, result := range searchResponse.Results {
		matchedCurrentResult := false
		for expectedIndex, expectedResult := range expected {
			if !matchesExpectedResult(result, expectedResult) {
				continue
			}
			if _, ok := matchedExpected[expectedIndex]; !ok {
				relevantAtRank[resultIndex] = true
			}
			matchedExpected[expectedIndex] = struct{}{}
			matchedCurrentResult = true
			if firstHitRank == 0 {
				firstHitRank = result.Rank
			}
		}
		if matchedCurrentResult {
			matchedResults[result.Rank] = struct{}{}
		}
	}
	caseResult := recallEvalCaseResult{
		ID:                     testCase.caseID(),
		Query:                  query,
		Limit:                  limit,
		Types:                  types,
		ExpectedCount:          len(expected),
		MatchedExpected:        len(matchedExpected),
		MatchedResults:         len(matchedResults),
		Matched:                firstHitRank > 0,
		FirstHitRank:           firstHitRank,
		ResultCount:            searchResponse.ResultCount,
		DedupedCount:           searchResponse.DedupedCount,
		LatencyMS:              latencyMS,
		EstimatedResultTokens:  estimatedSearchResultTokens(searchResponse.Results),
		ResultCharacters:       searchResultCharacters(searchResponse.Results),
		SearchedNamespace:      searchResponse.SearchedNamespaces,
		SearchedNamespaceCount: len(searchResponse.SearchedNamespaces),
		TopResults:             topRecallEvalResults(searchResponse.Results),
	}
	if len(searchResponse.Results) > 0 {
		caseResult.Precision = float64(caseResult.MatchedResults) / float64(len(searchResponse.Results))
	}
	if len(expected) > 0 {
		caseResult.Recall = float64(caseResult.MatchedExpected) / float64(len(expected))
		caseResult.NDCG = normalizedDiscountedCumulativeGain(relevantAtRank, len(expected))
	}
	caseResult.F1 = f1Score(caseResult.Precision, caseResult.Recall)
	caseResult.FilePrecision, caseResult.FileRecall, caseResult.FileF1 = fileLevelMetrics(searchResponse.Results, expected)
	if firstHitRank > 0 {
		caseResult.ReciprocalRank = 1 / float64(firstHitRank)
	}
	return caseResult
}

func normalizedDiscountedCumulativeGain(relevantAtRank []bool, expectedCount int) float64 {
	if expectedCount <= 0 || len(relevantAtRank) == 0 {
		return 0
	}
	dcg := 0.0
	for index, relevant := range relevantAtRank {
		if relevant {
			dcg += 1 / math.Log2(float64(index+2))
		}
	}
	idealRelevantCount := expectedCount
	if idealRelevantCount > len(relevantAtRank) {
		idealRelevantCount = len(relevantAtRank)
	}
	idcg := 0.0
	for index := range idealRelevantCount {
		idcg += 1 / math.Log2(float64(index+2))
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

func f1Score(precision float64, recall float64) float64 {
	if precision+recall == 0 {
		return 0
	}
	return 2 * precision * recall / (precision + recall)
}

func fileLevelMetrics(results []searchJSONResult, expected []recallExpectedResult) (float64, float64, float64) {
	hitFiles := make(map[string]struct{})
	for _, result := range results {
		if result.Location.RelativePath != "" {
			hitFiles[normalizeEvalFilePath(result.Location.RelativePath)] = struct{}{}
		}
	}
	oracleFiles := expectedFileSet(expected)
	if len(hitFiles) == 0 && len(oracleFiles) == 0 {
		return 0, 0, 0
	}
	matched := 0
	for filePath := range hitFiles {
		if _, ok := oracleFiles[filePath]; ok {
			matched++
		}
	}
	precision := 0.0
	if len(hitFiles) > 0 {
		precision = float64(matched) / float64(len(hitFiles))
	}
	recall := 0.0
	if len(oracleFiles) > 0 {
		recall = float64(matched) / float64(len(oracleFiles))
	}
	return precision, recall, f1Score(precision, recall)
}

func expectedFileSet(expected []recallExpectedResult) map[string]struct{} {
	files := make(map[string]struct{})
	for _, item := range expected {
		if item.RelativePath != "" {
			files[normalizeEvalFilePath(item.RelativePath)] = struct{}{}
		}
	}
	return files
}

func hasFileExpectations(expected []recallExpectedResult) bool {
	return len(expectedFileSet(expected)) > 0
}

func normalizeEvalFilePath(filePath string) string {
	return strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(filePath)), "/")
}

func matchesExpectedResult(result searchJSONResult, expected recallExpectedResult) bool {
	if expected.ResultID != "" && result.ResultID != expected.ResultID {
		return false
	}
	if expected.ContentHash != "" && result.ContentHash != expected.ContentHash {
		return false
	}
	if expected.Category != "" && result.Category != expected.Category {
		return false
	}
	if expected.SourceType != "" && result.SourceType != expected.SourceType {
		return false
	}
	if expected.SourceID != "" && result.SourceID != expected.SourceID {
		return false
	}
	if expected.Namespace != "" && result.Namespace != expected.Namespace {
		return false
	}
	if expected.RelativePath != "" {
		resultPath := normalizeEvalFilePath(result.Location.RelativePath)
		expectedPath := normalizeEvalFilePath(expected.RelativePath)
		if resultPath != expectedPath {
			return false
		}
	}
	if !lineExpectationMatches(result.Location, expected) {
		return false
	}
	if expected.DomainPath != "" && result.DomainPath != expected.DomainPath {
		return false
	}
	if expected.ContentContains != "" && !strings.Contains(result.Content, expected.ContentContains) {
		return false
	}
	if !documentExpectationMatches(result.Document, expected) {
		return false
	}
	return metadataExpectationMatches(result.Metadata, expected.Metadata)
}

func lineExpectationMatches(location searchJSONLocation, expected recallExpectedResult) bool {
	if expected.StartLine <= 0 && expected.EndLine <= 0 {
		return true
	}
	resultStart := location.StartLine
	resultEnd := location.EndLine
	if resultStart <= 0 && resultEnd <= 0 {
		return false
	}
	if resultStart <= 0 {
		resultStart = resultEnd
	}
	if resultEnd <= 0 {
		resultEnd = resultStart
	}
	expectedStart := expected.StartLine
	expectedEnd := expected.EndLine
	if expectedStart <= 0 {
		expectedStart = expectedEnd
	}
	if expectedEnd <= 0 {
		expectedEnd = expectedStart
	}
	return expectedStart <= resultEnd && resultStart <= expectedEnd
}

func documentExpectationMatches(document *searchJSONDocument, expected recallExpectedResult) bool {
	if expected.DocumentID == "" && expected.SectionID == "" && expected.HeadingPath == "" &&
		expected.KnowledgeKind == "" && expected.NodeKind == "" && expected.Version == "" {
		return true
	}
	if document == nil {
		return false
	}
	if expected.DocumentID != "" && document.DocumentID != expected.DocumentID {
		return false
	}
	if expected.SectionID != "" && document.SectionID != expected.SectionID {
		return false
	}
	if expected.HeadingPath != "" && document.HeadingPath != expected.HeadingPath {
		return false
	}
	if expected.KnowledgeKind != "" && document.KnowledgeKind != expected.KnowledgeKind {
		return false
	}
	if expected.NodeKind != "" && document.NodeKind != expected.NodeKind {
		return false
	}
	return expected.Version == "" || document.Version == expected.Version
}

func metadataExpectationMatches(metadata map[string]string, expected map[string]string) bool {
	for key, value := range expected {
		if metadata[key] != value {
			return false
		}
	}
	return true
}

func topRecallEvalResults(results []searchJSONResult) []recallEvalTopResult {
	items := make([]recallEvalTopResult, 0, len(results))
	for _, result := range results {
		items = append(items, recallEvalTopResult{
			Rank:            result.Rank,
			ResultID:        result.ResultID,
			Category:        result.Category,
			SourceType:      result.SourceType,
			SourceID:        result.SourceID,
			RelativePath:    result.Location.RelativePath,
			Score:           result.Score,
			ContentChars:    len([]rune(result.Content)),
			EstimatedTokens: estimateTextTokens(result.Content),
		})
	}
	return items
}

func readRecallEvalCases(path string) ([]recallEvalCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("recall eval cases file is empty")
	}
	switch trimmed[0] {
	case '[':
		var cases []recallEvalCase
		if err := json.Unmarshal(trimmed, &cases); err != nil {
			return nil, err
		}
		return validateRecallEvalCases(cases)
	case '{':
		var dataset recallEvalDataset
		if err := json.Unmarshal(trimmed, &dataset); err != nil {
			return readRecallEvalJSONL(trimmed)
		}
		if len(dataset.Instances) > 0 {
			return validateRecallEvalCases(dataset.Instances)
		}
		if len(dataset.Cases) > 0 {
			return validateRecallEvalCases(dataset.Cases)
		}
		return readRecallEvalJSONL(trimmed)
	default:
		return readRecallEvalJSONL(trimmed)
	}
}

func readRecallEvalJSONL(data []byte) ([]recallEvalCase, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), evalRecordLineBufferSize)
	cases := make([]recallEvalCase, 0)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var testCase recallEvalCase
		if err := json.Unmarshal(line, &testCase); err != nil {
			return nil, err
		}
		cases = append(cases, testCase)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return validateRecallEvalCases(cases)
}

func validateRecallEvalCases(cases []recallEvalCase) ([]recallEvalCase, error) {
	if len(cases) == 0 {
		return nil, errors.New("recall eval cases are empty")
	}
	for index, testCase := range cases {
		if strings.TrimSpace(testCase.queryText()) == "" {
			return nil, fmt.Errorf("recall eval case %d query is empty", index+1)
		}
		if len(testCase.expectedResults()) == 0 {
			return nil, fmt.Errorf("recall eval case %d expected results are empty", index+1)
		}
		if testCase.Limit < 0 {
			return nil, fmt.Errorf("recall eval case %d limit is invalid", index+1)
		}
	}
	return cases, nil
}

func (c recallEvalCase) expectedResults() []recallExpectedResult {
	if len(c.Expected) > 0 {
		return c.Expected
	}
	if len(c.ExpectedResults) > 0 {
		return c.ExpectedResults
	}
	oracleFiles := c.Oracles
	if len(oracleFiles) == 0 {
		oracleFiles = c.OracleFiles
	}
	if len(oracleFiles) == 0 {
		oracleFiles = extractOracleFilesFromPatch(c.Patch)
	}
	expected := make([]recallExpectedResult, 0, len(oracleFiles))
	for _, filePath := range oracleFiles {
		cleanPath := normalizeEvalFilePath(filePath)
		if cleanPath != "" {
			expected = append(expected, recallExpectedResult{RelativePath: cleanPath})
		}
	}
	return expected
}

func (c recallEvalCase) queryText() string {
	if strings.TrimSpace(c.Query) != "" {
		return c.Query
	}
	return c.ProblemStatement
}

func (c recallEvalCase) caseID() string {
	if strings.TrimSpace(c.ID) != "" {
		return c.ID
	}
	return c.InstanceID
}

func extractOracleFilesFromPatch(patch string) []string {
	if strings.TrimSpace(patch) == "" {
		return nil
	}
	matches := patchOracleFileRegexp.FindAllStringSubmatch(patch, -1)
	seen := make(map[string]struct{}, len(matches))
	files := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		filePath := normalizeEvalFilePath(match[1])
		if filePath == "" {
			continue
		}
		if _, ok := seen[filePath]; ok {
			continue
		}
		seen[filePath] = struct{}{}
		files = append(files, filePath)
	}
	return files
}

func (c recallEvalCase) typeArg(fallback string) string {
	if len(c.SearchTypes) > 0 {
		return strings.Join(c.SearchTypes, ",")
	}
	if strings.TrimSpace(c.Types) != "" {
		return c.Types
	}
	return fallback
}

func (r recallEvalRequest) typeArg(defaultTypes []string) string {
	if strings.TrimSpace(r.Types) != "" {
		return r.Types
	}
	if len(defaultTypes) == 0 {
		return "all"
	}
	return strings.Join(defaultTypes, ",")
}

func parsePositiveInt(value string) (int, error) {
	result, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	if result <= 0 {
		return 0, errors.New("value must be positive")
	}
	return result, nil
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
