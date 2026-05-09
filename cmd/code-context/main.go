// 文件说明：提供 Agent-Memory 的 CLI 入口。
// 实现原理：解析 index、search、status、clear 子命令，组装配置、索引器、搜索器和本地向量存储完成操作。
// 使用方式：执行 code-context index/search/status/clear 操作指定代码库索引。
// 注意事项：默认使用本地 HashEmbedder 和 LocalStore，配置环境变量后可切换 OpenAI-compatible Embedder 和 Milvus VectorStore。
// 交互模块：internal/config、internal/scanner、internal/splitter、internal/embed、internal/vectorstore、internal/indexer、internal/searcher、internal/snapshot。

// Package main 提供 code-context 命令入口。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mazhan465/Agent-Memory/internal/catalog"
	"github.com/mazhan465/Agent-Memory/internal/config"
	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
	"github.com/mazhan465/Agent-Memory/internal/document"
	"github.com/mazhan465/Agent-Memory/internal/domain"
	"github.com/mazhan465/Agent-Memory/internal/embed"
	"github.com/mazhan465/Agent-Memory/internal/indexer"
	"github.com/mazhan465/Agent-Memory/internal/scanner"
	"github.com/mazhan465/Agent-Memory/internal/snapshot"
	"github.com/mazhan465/Agent-Memory/internal/source"
	"github.com/mazhan465/Agent-Memory/internal/splitter"
	"github.com/mazhan465/Agent-Memory/internal/vectorstore"
)

const (
	exitCodeOK    = 0
	exitCodeError = 1

	searchCategoryCode              = "code"
	searchCategoryKnowledgeDocument = "knowledge_document"
	searchCategoryExperience        = "experience"
	searchCategoryUserPreference    = "user_preference"
	searchCategoryConversation      = "conversation"
	searchCategoryToolHistory       = "tool_history"
	searchCategoryFact              = "fact"
)

type app struct {
	config        config.Config
	embedder      embed.Embedder
	vectorStore   vectorstore.VectorStore
	snapshotStore *snapshot.Store
	catalogStore  *catalog.Store
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitCodeError)
	}
	os.Exit(exitCodeOK)
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	application, err := newApp(ctx, cfg)
	if err != nil {
		return err
	}

	switch args[0] {
	case "index":
		return application.runIndex(ctx, args[1:])
	case "search":
		return application.runSearch(ctx, args[1:])
	case "status":
		return application.runStatus(args[1:])
	case "clear":
		return application.runClear(ctx, args[1:])
	case "import":
		return application.runImport(ctx, args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func newApp(ctx context.Context, cfg config.Config) (*app, error) {
	embedderInstance, err := newEmbedder(cfg)
	if err != nil {
		return nil, err
	}
	vectorStoreInstance, err := newVectorStore(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &app{
		config:        cfg,
		embedder:      embedderInstance,
		vectorStore:   vectorStoreInstance,
		snapshotStore: snapshot.NewStore(cfg.StorageDir),
		catalogStore:  catalog.NewStore(cfg.StorageDir),
	}, nil
}

func newEmbedder(cfg config.Config) (embed.Embedder, error) {
	switch strings.ToLower(cfg.EmbeddingProvider) {
	case "", "hash":
		return embed.NewHashEmbedder(cfg.EmbeddingDimension), nil
	case "openai", "openai-compatible":
		return embed.NewOpenAIEmbedder(embed.OpenAIOptions{
			BaseURL: cfg.OpenAIBaseURL,
			APIKey:  cfg.OpenAIAPIKey,
			Model:   cfg.OpenAIEmbeddingModel,
		})
	default:
		return nil, fmt.Errorf("unsupported embedding provider %q", cfg.EmbeddingProvider)
	}
}

func newVectorStore(ctx context.Context, cfg config.Config) (vectorstore.VectorStore, error) {
	switch strings.ToLower(cfg.VectorStoreProvider) {
	case "", "local":
		return vectorstore.NewLocalStore(cfg.StorageDir), nil
	case "milvus":
		return vectorstore.NewMilvusStore(ctx, vectorstore.MilvusOptions{
			Address:        cfg.MilvusAddress,
			Username:       cfg.MilvusUsername,
			Password:       cfg.MilvusPassword,
			CollectionName: cfg.MilvusCollection,
		})
	default:
		return nil, fmt.Errorf("unsupported vector store %q", cfg.VectorStoreProvider)
	}
}

func (a *app) runIndex(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: code-context index <path>")
	}

	scannerInstance := scanner.New(a.config.SupportedExts, a.config.IgnoreNames)
	splitterInstance := splitter.NewLineSplitter(a.config.MaxChunkLines, a.config.ChunkOverlapLines)
	indexerInstance := indexer.New(scannerInstance, splitterInstance, a.embedder, a.vectorStore, a.snapshotStore)
	stats, err := indexerInstance.Index(ctx, args[0])
	if err != nil {
		return err
	}

	fmt.Printf("indexed path=%s namespace=%s files=%d chunks=%d\n", stats.Path, stats.Namespace, stats.IndexedFiles, stats.TotalChunks)
	return nil
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

func (a *app) runStatus(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: code-context status <path>")
	}

	namespace, absolutePath, err := indexer.NamespaceForPath(args[0])
	if err != nil {
		return err
	}
	info, err := a.snapshotStore.Get(namespace)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("path=%s namespace=%s status=not_indexed\n", absolutePath, namespace)
			return nil
		}
		return err
	}

	fmt.Printf("path=%s namespace=%s status=%s files=%d chunks=%d updated_at=%s\n",
		info.Path,
		info.Namespace,
		info.Status,
		info.IndexedFiles,
		info.TotalChunks,
		info.UpdatedAt.Format("2006-01-02 15:04:05"),
	)
	if info.ErrorMessage != "" {
		fmt.Printf("error=%s\n", info.ErrorMessage)
	}
	return nil
}

func (a *app) runClear(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: code-context clear <path>")
	}

	namespace, absolutePath, err := indexer.NamespaceForPath(args[0])
	if err != nil {
		return err
	}
	if err := a.vectorStore.Clear(ctx, namespace); err != nil {
		return err
	}
	if err := a.snapshotStore.Delete(namespace); err != nil {
		return err
	}
	fmt.Printf("cleared path=%s namespace=%s\n", absolutePath, namespace)
	return nil
}

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

func newSearchTypeSelection(typeArg string) (searchTypeSelection, error) {
	values := splitSearchTypeArg(typeArg)
	selection := searchTypeSelection{SourceTypes: make(map[contextdoc.SourceType]struct{})}
	if len(values) == 0 || containsSearchType(values, "all") {
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

func containsSearchType(values []string, want string) bool {
	return slices.Contains(values, want)
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
	options, err := knowledgeSearchOptions(ctx, query, limit)
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

func (a *app) runImport(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: code-context import <knowledge> ...")
	}
	switch args[0] {
	case "knowledge", "document", "documents":
		return a.runImportKnowledge(ctx, args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown import type %q", args[0])
	}
}

func (a *app) runImportKnowledge(ctx context.Context, args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: code-context import knowledge <path> [source-id]")
	}
	sourceID := ""
	if len(args) == 2 {
		sourceID = args[1]
	}
	scope, documentSource, absolutePath, err := documentKnowledgeSource(args[0], sourceID)
	if err != nil {
		return err
	}
	reader := source.NewMarkdownDocumentSource(absolutePath, scope, documentSource)
	items, err := reader.Read(ctx)
	if err != nil {
		return err
	}
	namespace, err := contextdoc.NamespaceForSource(scope, documentSource)
	if err != nil {
		return err
	}
	documents, err := a.buildKnowledgeDocuments(ctx, namespace, items)
	if err != nil {
		return err
	}
	if err := a.vectorStore.Put(ctx, namespace, documents); err != nil {
		return err
	}
	manifest := a.knowledgeManifest(scope, documentSource, namespace, len(items), documents)
	if err := a.catalogStore.UpsertManifest(ctx, manifest, catalog.StatusIndexed); err != nil {
		return err
	}
	fmt.Printf("imported knowledge path=%s source_id=%s namespace=%s documents=%d chunks=%d\n",
		absolutePath, documentSource.ID, namespace, len(items), len(documents))
	return nil
}

type knowledgeChunk struct {
	item  source.DocumentItem
	chunk splitter.Chunk
}

func (a *app) buildKnowledgeDocuments(
	ctx context.Context,
	namespace string,
	items []source.DocumentItem,
) ([]vectorstore.Document, error) {
	chunker := splitter.NewDocumentChunker(
		a.config.MaxChunkLines,
		a.config.ChunkOverlapLines,
		document.LanguageMarkdown,
		nil,
	)
	chunks := make([]knowledgeChunk, 0)
	texts := make([]string, 0)
	for _, item := range items {
		itemChunks := chunker.Chunks(item.RelativePath, item.Nodes)
		for _, chunk := range itemChunks {
			chunks = append(chunks, knowledgeChunk{item: item, chunk: chunk})
			texts = append(texts, chunk.Content)
		}
	}
	if len(chunks) == 0 {
		return nil, nil
	}
	vectors, err := a.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(chunks) {
		return nil, fmt.Errorf("embedding count mismatch: got %d, want %d", len(vectors), len(chunks))
	}
	resolver := domain.NewDefaultDomainResolver()
	documents := make([]vectorstore.Document, 0, len(chunks))
	for index, entry := range chunks {
		document, err := knowledgeVectorDocument(ctx, namespace, entry, vectors[index], resolver)
		if err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	return documents, nil
}

func knowledgeVectorDocument(
	ctx context.Context,
	namespace string,
	entry knowledgeChunk,
	vector []float32,
	resolver *domain.DomainResolver,
) (vectorstore.Document, error) {
	extension := strings.ToLower(filepath.Ext(entry.item.RelativePath))
	metadata := map[string]string{
		"absolute_path": entry.item.AbsolutePath,
		"scope_type":    string(entry.item.Scope.Type),
		"scope_id":      entry.item.Scope.ID,
		"source_type":   string(entry.item.Source.Type),
		"source_id":     entry.item.Source.ID,
	}
	maps.Copy(metadata, entry.chunk.Metadata)
	for key, value := range entry.item.Source.Metadata {
		if strings.TrimSpace(value) != "" {
			metadata["source_"+key] = value
		}
	}
	decision, err := resolver.Resolve(ctx, domain.Input{
		Query:          entry.chunk.Content,
		FileExtensions: []string{extension},
		Metadata:       metadata,
	})
	if err != nil {
		return vectorstore.Document{}, err
	}
	if decision.Domain != "" {
		metadata[vectorstore.MetadataDomainPath] = string(decision.Domain)
	}
	if decision.ExperienceKind != "" {
		metadata[vectorstore.MetadataExperienceKind] = string(decision.ExperienceKind)
	}
	return vectorstore.Document{
		ID: contextdoc.StableID(
			"knowledge",
			namespace,
			entry.item.RelativePath,
			strconv.Itoa(entry.chunk.StartLine),
			entry.chunk.Content,
		),
		Namespace:     namespace,
		Vector:        vector,
		Content:       entry.chunk.Content,
		RelativePath:  entry.item.RelativePath,
		StartLine:     entry.chunk.StartLine,
		EndLine:       entry.chunk.EndLine,
		FileExtension: extension,
		Language:      entry.chunk.Language,
		Metadata:      metadata,
	}, nil
}

func knowledgeSearchOptions(ctx context.Context, query string, limit int) (vectorstore.SearchOptions, error) {
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

func documentKnowledgeSource(rootPath string, sourceID string) (contextdoc.Scope, contextdoc.Source, string, error) {
	absolutePath, err := filepath.Abs(rootPath)
	if err != nil {
		return contextdoc.Scope{}, contextdoc.Source{}, "", err
	}
	absolutePath = filepath.Clean(absolutePath)
	cleanSourceID := strings.TrimSpace(sourceID)
	if cleanSourceID == "" {
		cleanSourceID = defaultDocumentSourceID(absolutePath)
	}
	scope := contextdoc.Scope{Type: contextdoc.ScopeTypeWorkspace, ID: filepath.ToSlash(absolutePath)}
	documentSource := contextdoc.Source{
		Type: contextdoc.SourceTypeDocument,
		ID:   cleanSourceID,
		Metadata: map[string]string{
			"root_path": filepath.ToSlash(absolutePath),
		},
	}
	return scope, documentSource, filepath.ToSlash(absolutePath), nil
}

func defaultDocumentSourceID(absolutePath string) string {
	base := filepath.Base(absolutePath)
	if extension := filepath.Ext(base); extension != "" {
		base = strings.TrimSuffix(base, extension)
	}
	base = strings.TrimSpace(base)
	if base == "" || base == string(filepath.Separator) || base == "." {
		return contextdoc.StableID("document_source", absolutePath)
	}
	return base
}

func (a *app) knowledgeManifest(
	scope contextdoc.Scope,
	documentSource contextdoc.Source,
	namespace string,
	documentCount int,
	documents []vectorstore.Document,
) contextdoc.Manifest {
	embeddingDimension := a.config.EmbeddingDimension
	if len(documents) > 0 {
		embeddingDimension = len(documents[0].Vector)
	}
	return contextdoc.Manifest{
		Scope:              scope,
		Source:             documentSource,
		Version:            time.Now().Unix(),
		Checksum:           checksumVectorDocuments(documents),
		EmbeddingProvider:  a.config.EmbeddingProvider,
		EmbeddingModel:     a.embeddingModelName(),
		EmbeddingDimension: embeddingDimension,
		IndexedAt:          time.Now(),
		DocumentCount:      documentCount,
		KnowledgeCount:     len(documents),
		Metadata: map[string]string{
			"namespace": namespace,
		},
	}
}

func (a *app) embeddingModelName() string {
	switch strings.ToLower(a.config.EmbeddingProvider) {
	case "", "hash":
		return fmt.Sprintf("hash-%d", a.config.EmbeddingDimension)
	default:
		return a.config.OpenAIEmbeddingModel
	}
}

func checksumVectorDocuments(documents []vectorstore.Document) string {
	hash := sha256.New()
	for _, document := range documents {
		hash.Write([]byte(document.ID))
		hash.Write([]byte("\x00"))
		hash.Write([]byte(document.RelativePath))
		hash.Write([]byte("\x00"))
		hash.Write([]byte(document.Content))
		hash.Write([]byte("\x00"))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func printUsage() {
	fmt.Println(`Usage:
  code-context index <path>
  code-context search <path> <query> [limit] [types]
  code-context import knowledge <path> [source-id]
  code-context status <path>
  code-context clear <path>

Search types:
  all, code, knowledge, conversation, experience, preference, tool_history, fact

Examples:
  code-context index .
  code-context search . "vector database operations" 5
  code-context search . "project rules" knowledge
  code-context search . "previous fix" 5 conversation,experience
  code-context import knowledge ./docs project-docs`)
}
