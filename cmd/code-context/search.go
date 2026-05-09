// 文件说明：提供统一 JSON 搜索入口。
// 实现原理：根据查询一次检索代码 namespace 和 SourceCatalog 中符合类型条件的 source namespace，合并重排后输出 JSON。
// 使用方式：runSearch 由 CLI search 命令调用，支持 types 参数过滤代码、知识、经验、偏好等数据。
// 注意事项：搜索返回结构面向 Agent 消费，默认返回全部可检索类型。
// 交互模块：internal/catalog、internal/contextdoc、internal/domain、internal/indexer、internal/vectorstore。

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mazhan465/Agent-Memory/internal/catalog"
	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
	"github.com/mazhan465/Agent-Memory/internal/domain"
	"github.com/mazhan465/Agent-Memory/internal/indexer"
	"github.com/mazhan465/Agent-Memory/internal/vectorstore"
)

const (
	searchCategoryCode              = "code"
	searchCategoryKnowledgeDocument = "knowledge_document"
	searchCategoryExperience        = "experience"
	searchCategoryUserPreference    = "user_preference"
	searchCategoryConversation      = "conversation"
	searchCategoryToolHistory       = "tool_history"
	searchCategoryFact              = "fact"
)

type searchJSONResponse struct {
	Query              string                `json:"query"`
	RootPath           string                `json:"root_path"`
	Limit              int                   `json:"limit"`
	SearchTypes        []string              `json:"search_types"`
	ResultCount        int                   `json:"result_count"`
	SearchedNamespaces []searchJSONNamespace `json:"searched_namespaces"`
	Results            []searchJSONResult    `json:"results"`
}

type searchJSONNamespace struct {
	Category   string `json:"category"`
	SourceType string `json:"source_type"`
	SourceID   string `json:"source_id,omitempty"`
	Namespace  string `json:"namespace"`
}

type searchJSONResult struct {
	Rank           int                 `json:"rank"`
	Category       string              `json:"category"`
	SourceType     string              `json:"source_type"`
	SourceID       string              `json:"source_id,omitempty"`
	Namespace      string              `json:"namespace"`
	Score          float64             `json:"score"`
	Location       searchJSONLocation  `json:"location"`
	Document       *searchJSONDocument `json:"document,omitempty"`
	ExperienceKind string              `json:"experience_kind,omitempty"`
	DomainPath     string              `json:"domain_path,omitempty"`
	Content        string              `json:"content"`
	Metadata       map[string]string   `json:"metadata,omitempty"`
}

type searchJSONLocation struct {
	RelativePath  string `json:"relative_path"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
	FileExtension string `json:"file_extension"`
	Language      string `json:"language"`
}

type searchJSONDocument struct {
	DocumentID    string `json:"document_id,omitempty"`
	SectionID     string `json:"section_id,omitempty"`
	HeadingPath   string `json:"heading_path,omitempty"`
	KnowledgeKind string `json:"knowledge_kind,omitempty"`
	NodeKind      string `json:"node_kind,omitempty"`
	Version       string `json:"version,omitempty"`
}

type categorizedSearchResult struct {
	Result     vectorstore.SearchResult
	Category   string
	SourceType string
	SourceID   string
	Namespace  string
}

type searchTypeSelection struct {
	All         bool
	Code        bool
	SourceTypes map[contextdoc.SourceType]struct{}
	Labels      []string
}

func (a *app) runSearch(ctx context.Context, args []string) error {
	rootPath, query, limit, selection, err := a.parseSearchArgs(args)
	if err != nil {
		return err
	}
	response, err := a.searchAll(ctx, rootPath, query, limit, selection)
	if err != nil {
		return err
	}
	return printJSON(response)
}

func (a *app) parseSearchArgs(args []string) (string, string, int, searchTypeSelection, error) {
	if len(args) < 2 || len(args) > 4 {
		return "", "", 0, searchTypeSelection{}, errors.New("usage: code-context search <path> <query> [limit] [types]")
	}
	limit := a.config.SearchLimit
	typeArg := ""
	if len(args) >= 3 {
		parsedLimit, err := strconv.Atoi(args[2])
		if err == nil {
			if parsedLimit <= 0 {
				return "", "", 0, searchTypeSelection{}, errors.New("limit must be a positive integer")
			}
			limit = parsedLimit
		} else {
			typeArg = args[2]
		}
	}
	if len(args) == 4 {
		if typeArg != "" {
			return "", "", 0, searchTypeSelection{}, errors.New("usage: code-context search <path> <query> [limit] [types]")
		}
		typeArg = args[3]
	}
	selection, err := newSearchTypeSelection(typeArg)
	if err != nil {
		return "", "", 0, searchTypeSelection{}, err
	}
	return args[0], args[1], limit, selection, nil
}

func newSearchTypeSelection(typeArg string) (searchTypeSelection, error) {
	values := splitSearchTypeArg(typeArg)
	selection := searchTypeSelection{SourceTypes: make(map[contextdoc.SourceType]struct{})}
	if len(values) == 0 || slices.Contains(values, "all") {
		selection.All = true
		selection.Code = true
		selection.Labels = []string{"all"}
		return selection, nil
	}
	labels := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := selection.addSearchType(value, labels); err != nil {
			return searchTypeSelection{}, err
		}
	}
	selection.Labels = sortedKeys(labels)
	return selection, nil
}

func (s *searchTypeSelection) addSearchType(value string, labels map[string]struct{}) error {
	switch value {
	case "code":
		s.Code = true
		labels[searchCategoryCode] = struct{}{}
	case "knowledge", "knowledge_document", "document", "documents", "docs":
		s.SourceTypes[contextdoc.SourceTypeDocument] = struct{}{}
		s.SourceTypes[contextdoc.SourceTypeExternalKnowledge] = struct{}{}
		labels["knowledge"] = struct{}{}
	case "conversation", "history", "chat", "historical_conversation":
		s.SourceTypes[contextdoc.SourceTypeConversation] = struct{}{}
		labels[searchCategoryConversation] = struct{}{}
	case "experience", "experiences":
		s.SourceTypes[contextdoc.SourceTypeExperience] = struct{}{}
		labels[searchCategoryExperience] = struct{}{}
	case "preference", "preferences", "user_preference", "user_preferences":
		s.SourceTypes[contextdoc.SourceTypePreference] = struct{}{}
		labels[searchCategoryUserPreference] = struct{}{}
	case "tool", "tool_history":
		s.SourceTypes[contextdoc.SourceTypeToolHistory] = struct{}{}
		labels[searchCategoryToolHistory] = struct{}{}
	case "fact", "facts":
		s.SourceTypes[contextdoc.SourceTypeFact] = struct{}{}
		labels[searchCategoryFact] = struct{}{}
	default:
		return fmt.Errorf("unsupported search type %q", value)
	}
	return nil
}

func (s searchTypeSelection) includeSourceType(sourceType contextdoc.SourceType) bool {
	if s.All {
		return true
	}
	_, ok := s.SourceTypes[sourceType]
	return ok
}

func splitSearchTypeArg(typeArg string) []string {
	if strings.TrimSpace(typeArg) == "" {
		return nil
	}
	parts := strings.FieldsFunc(typeArg, func(value rune) bool {
		return value == ',' || value == '|'
	})
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.ToLower(strings.TrimSpace(part))
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (a *app) searchAll(
	ctx context.Context,
	rootPath string,
	query string,
	limit int,
	selection searchTypeSelection,
) (searchJSONResponse, error) {
	namespace, absolutePath, err := indexer.NamespaceForPath(rootPath)
	if err != nil {
		return searchJSONResponse{}, err
	}
	queryVector, err := a.embedder.Embed(ctx, query)
	if err != nil {
		return searchJSONResponse{}, err
	}
	options, err := searchOptions(ctx, query, limit)
	if err != nil {
		return searchJSONResponse{}, err
	}

	response := searchJSONResponse{Query: query, RootPath: absolutePath, Limit: limit, SearchTypes: selection.Labels}
	results := make([]categorizedSearchResult, 0)
	if selection.Code {
		results, err = a.appendNamespaceResults(ctx, results, namespace, queryVector, options, searchJSONNamespace{
			Category:   searchCategoryCode,
			SourceType: string(contextdoc.SourceTypeCodebase),
			Namespace:  namespace,
		}, &response)
		if err != nil {
			return searchJSONResponse{}, err
		}
	}
	if err := a.appendCatalogResults(ctx, &results, queryVector, options, selection, &response); err != nil {
		return searchJSONResponse{}, err
	}

	sort.SliceStable(results, func(i int, j int) bool {
		return results[i].Result.Score > results[j].Result.Score
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	response.Results = makeSearchJSONResults(results)
	response.ResultCount = len(response.Results)
	return response, nil
}

func searchOptions(ctx context.Context, query string, limit int) (vectorstore.SearchOptions, error) {
	options := vectorstore.SearchOptions{Limit: limit}
	decision, err := domain.NewDefaultDomainResolver().Resolve(ctx, domain.Input{Query: query})
	if err != nil {
		return vectorstore.SearchOptions{}, err
	}
	if decision.Domain != "" {
		options.DomainFilters = append(options.DomainFilters, string(decision.Domain))
		if decision.ParentDomain != "" {
			options.DomainFilters = append(options.DomainFilters, string(decision.ParentDomain))
		}
	}
	return options, nil
}

func (a *app) appendCatalogResults(
	ctx context.Context,
	results *[]categorizedSearchResult,
	queryVector []float32,
	options vectorstore.SearchOptions,
	selection searchTypeSelection,
	response *searchJSONResponse,
) error {
	entries, err := a.catalogStore.List(ctx, catalog.Filter{Status: catalog.StatusIndexed})
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !selection.includeSourceType(entry.Source.Type) {
			continue
		}
		namespace, err := contextdoc.NamespaceForSource(entry.Scope, entry.Source)
		if err != nil {
			return err
		}
		*results, err = a.appendNamespaceResults(ctx, *results, namespace, queryVector, options, searchJSONNamespace{
			Category:   categoryForSourceType(entry.Source.Type),
			SourceType: string(entry.Source.Type),
			SourceID:   entry.Source.ID,
			Namespace:  namespace,
		}, response)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *app) appendNamespaceResults(
	ctx context.Context,
	results []categorizedSearchResult,
	namespace string,
	queryVector []float32,
	options vectorstore.SearchOptions,
	searchNamespace searchJSONNamespace,
	response *searchJSONResponse,
) ([]categorizedSearchResult, error) {
	response.SearchedNamespaces = append(response.SearchedNamespaces, searchNamespace)
	matches, err := a.vectorStore.Search(ctx, namespace, queryVector, options)
	if err != nil {
		if os.IsNotExist(err) {
			return results, nil
		}
		return nil, err
	}
	for _, match := range matches {
		results = append(results, categorizedSearchResult{
			Result:     match,
			Category:   searchNamespace.Category,
			SourceType: searchNamespace.SourceType,
			SourceID:   searchNamespace.SourceID,
			Namespace:  namespace,
		})
	}
	return results, nil
}

func makeSearchJSONResults(results []categorizedSearchResult) []searchJSONResult {
	items := make([]searchJSONResult, 0, len(results))
	for index, result := range results {
		items = append(items, makeSearchJSONResult(index+1, result))
	}
	return items
}

func makeSearchJSONResult(rank int, result categorizedSearchResult) searchJSONResult {
	document := result.Result.Document
	metadata := document.Metadata
	return searchJSONResult{
		Rank:       rank,
		Category:   result.Category,
		SourceType: result.SourceType,
		SourceID:   firstNonEmpty(result.SourceID, metadata["source_id"]),
		Namespace:  result.Namespace,
		Score:      result.Result.Score,
		Location: searchJSONLocation{
			RelativePath:  document.RelativePath,
			StartLine:     document.StartLine,
			EndLine:       document.EndLine,
			FileExtension: document.FileExtension,
			Language:      document.Language,
		},
		Document:       documentMetadata(metadata),
		ExperienceKind: metadata[vectorstore.MetadataExperienceKind],
		DomainPath:     metadata[vectorstore.MetadataDomainPath],
		Content:        document.Content,
		Metadata:       metadata,
	}
}

func documentMetadata(metadata map[string]string) *searchJSONDocument {
	documentID := metadata[vectorstore.MetadataDocumentID]
	sectionID := metadata[vectorstore.MetadataSectionID]
	headingPath := metadata[vectorstore.MetadataHeadingPath]
	knowledgeKind := metadata[vectorstore.MetadataKnowledgeKind]
	nodeKind := metadata[vectorstore.MetadataNodeKind]
	version := metadata[vectorstore.MetadataVersion]
	if documentID == "" && sectionID == "" && headingPath == "" && knowledgeKind == "" && nodeKind == "" && version == "" {
		return nil
	}
	return &searchJSONDocument{
		DocumentID:    documentID,
		SectionID:     sectionID,
		HeadingPath:   headingPath,
		KnowledgeKind: knowledgeKind,
		NodeKind:      nodeKind,
		Version:       version,
	}
}

func categoryForSourceType(sourceType contextdoc.SourceType) string {
	switch sourceType {
	case contextdoc.SourceTypeDocument, contextdoc.SourceTypeExternalKnowledge:
		return searchCategoryKnowledgeDocument
	case contextdoc.SourceTypeExperience:
		return searchCategoryExperience
	case contextdoc.SourceTypePreference:
		return searchCategoryUserPreference
	case contextdoc.SourceTypeConversation:
		return searchCategoryConversation
	case contextdoc.SourceTypeToolHistory:
		return searchCategoryToolHistory
	case contextdoc.SourceTypeFact:
		return searchCategoryFact
	default:
		return string(sourceType)
	}
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
