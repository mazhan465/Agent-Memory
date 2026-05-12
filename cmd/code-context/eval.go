// 文件说明：提供召回质量评估命令。
// 实现原理：读取 JSON/JSONL 评估集，复用统一搜索入口执行检索，并根据期望结果计算 hit rate、MRR 和 recall。
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
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const evalRecordLineBufferSize = 1024 * 1024

type recallEvalRequest struct {
	RootPath  string
	CasesPath string
	Limit     int
	Types     string
}

type recallEvalCase struct {
	ID              string                 `json:"id,omitempty"`
	Query           string                 `json:"query"`
	Expected        []recallExpectedResult `json:"expected,omitempty"`
	ExpectedResults []recallExpectedResult `json:"expected_results,omitempty"`
	Limit           int                    `json:"limit,omitempty"`
	Types           string                 `json:"types,omitempty"`
	SearchTypes     []string               `json:"search_types,omitempty"`
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
	RootPath       string                 `json:"root_path"`
	CasesPath      string                 `json:"cases_path"`
	Limit          int                    `json:"limit"`
	Types          string                 `json:"types"`
	CaseCount      int                    `json:"case_count"`
	PassedCount    int                    `json:"passed_count"`
	FailedCount    int                    `json:"failed_count"`
	HitRate        float64                `json:"hit_rate"`
	MeanRecall     float64                `json:"mean_recall"`
	MRR            float64                `json:"mrr"`
	AverageHitRank float64                `json:"average_hit_rank,omitempty"`
	Cases          []recallEvalCaseResult `json:"cases"`
}

type recallEvalCaseResult struct {
	ID                string                `json:"id,omitempty"`
	Query             string                `json:"query"`
	Limit             int                   `json:"limit"`
	Types             string                `json:"types"`
	ExpectedCount     int                   `json:"expected_count"`
	MatchedExpected   int                   `json:"matched_expected"`
	Matched           bool                  `json:"matched"`
	Recall            float64               `json:"recall"`
	FirstHitRank      int                   `json:"first_hit_rank,omitempty"`
	ReciprocalRank    float64               `json:"reciprocal_rank"`
	ResultCount       int                   `json:"result_count"`
	SearchedNamespace []searchJSONNamespace `json:"searched_namespaces"`
	TopResults        []recallEvalTopResult `json:"top_results"`
}

type recallEvalTopResult struct {
	Rank         int     `json:"rank"`
	ResultID     string  `json:"result_id"`
	Category     string  `json:"category"`
	SourceType   string  `json:"source_type"`
	SourceID     string  `json:"source_id,omitempty"`
	RelativePath string  `json:"relative_path,omitempty"`
	Score        float64 `json:"score"`
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
	var recallSum float64
	var reciprocalRankSum float64
	var hitRankSum int
	for _, testCase := range cases {
		caseResult, err := a.evaluateRecallCase(ctx, request, testCase)
		if err != nil {
			return recallEvalResponse{}, err
		}
		response.Cases = append(response.Cases, caseResult)
		recallSum += caseResult.Recall
		reciprocalRankSum += caseResult.ReciprocalRank
		if caseResult.Matched {
			response.PassedCount++
			hitRankSum += caseResult.FirstHitRank
		}
	}
	response.FailedCount = response.CaseCount - response.PassedCount
	if response.CaseCount > 0 {
		response.HitRate = float64(response.PassedCount) / float64(response.CaseCount)
		response.MeanRecall = recallSum / float64(response.CaseCount)
		response.MRR = reciprocalRankSum / float64(response.CaseCount)
	}
	if response.PassedCount > 0 {
		response.AverageHitRank = float64(hitRankSum) / float64(response.PassedCount)
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
	selection, err := newSearchTypeSelection(types)
	if err != nil {
		return recallEvalCaseResult{}, err
	}
	searchResponse, err := a.searchAll(ctx, searchRequestOptions{
		RootPath:       request.RootPath,
		Query:          testCase.Query,
		Limit:          limit,
		Selection:      selection,
		DisableSession: true,
	})
	if err != nil {
		return recallEvalCaseResult{}, err
	}
	return evaluateRecallCaseResult(testCase, expected, limit, types, searchResponse), nil
}

func evaluateRecallCaseResult(
	testCase recallEvalCase,
	expected []recallExpectedResult,
	limit int,
	types string,
	searchResponse searchJSONResponse,
) recallEvalCaseResult {
	matchedExpected := make(map[int]struct{}, len(expected))
	firstHitRank := 0
	for _, result := range searchResponse.Results {
		for expectedIndex, expectedResult := range expected {
			if !matchesExpectedResult(result, expectedResult) {
				continue
			}
			matchedExpected[expectedIndex] = struct{}{}
			if firstHitRank == 0 {
				firstHitRank = result.Rank
			}
		}
	}
	caseResult := recallEvalCaseResult{
		ID:                testCase.ID,
		Query:             testCase.Query,
		Limit:             limit,
		Types:             types,
		ExpectedCount:     len(expected),
		MatchedExpected:   len(matchedExpected),
		Matched:           firstHitRank > 0,
		FirstHitRank:      firstHitRank,
		ResultCount:       searchResponse.ResultCount,
		SearchedNamespace: searchResponse.SearchedNamespaces,
		TopResults:        topRecallEvalResults(searchResponse.Results),
	}
	if len(expected) > 0 {
		caseResult.Recall = float64(caseResult.MatchedExpected) / float64(len(expected))
	}
	if firstHitRank > 0 {
		caseResult.ReciprocalRank = 1 / float64(firstHitRank)
	}
	return caseResult
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
	if expected.RelativePath != "" && result.Location.RelativePath != filepath.ToSlash(expected.RelativePath) {
		return false
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
			Rank:         result.Rank,
			ResultID:     result.ResultID,
			Category:     result.Category,
			SourceType:   result.SourceType,
			SourceID:     result.SourceID,
			RelativePath: result.Location.RelativePath,
			Score:        result.Score,
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
	if trimmed[0] == '[' {
		var cases []recallEvalCase
		if err := json.Unmarshal(trimmed, &cases); err != nil {
			return nil, err
		}
		return validateRecallEvalCases(cases)
	}
	return readRecallEvalJSONL(trimmed)
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
		if strings.TrimSpace(testCase.Query) == "" {
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
	return c.ExpectedResults
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
