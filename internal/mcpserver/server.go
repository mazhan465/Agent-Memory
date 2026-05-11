// 文件说明：提供 Agent-Memory 的 MCP stdio Server 基础实现。
// 实现原理：通过 Content-Length 帧读写 JSON-RPC 2.0 消息，注册代码索引、搜索、清理和状态查询工具。
// 使用方式：cmd/code-context-mcp 创建 Server 后调用 Serve，MCP Client 可调用 index_codebase、search_code 等工具。
// 注意事项：当前只实现 tools 能力，不提供 resources/prompts；stdout 仅输出 MCP 协议消息。
// 交互模块：cmd/code-context-mcp、internal/config、internal/indexer、internal/searcher、internal/snapshot、internal/vectorstore。

// Package mcpserver 提供 MCP stdio Server 能力。
package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mazhan465/Agent-Memory/internal/config"
	"github.com/mazhan465/Agent-Memory/internal/embed"
	"github.com/mazhan465/Agent-Memory/internal/indexer"
	"github.com/mazhan465/Agent-Memory/internal/scanner"
	"github.com/mazhan465/Agent-Memory/internal/searcher"
	"github.com/mazhan465/Agent-Memory/internal/snapshot"
	"github.com/mazhan465/Agent-Memory/internal/splitter"
	"github.com/mazhan465/Agent-Memory/internal/vectorstore"
)

const (
	jsonRPCVersion       = "2.0"
	mcpProtocolVersion   = "2024-11-05"
	mcpServerName        = "code-context-mcp"
	mcpServerVersion     = "0.1.0"
	contentLengthHeader  = "content-length"
	jsonRPCParseError    = -32700
	jsonRPCInvalidParams = -32602
	jsonRPCMethodMissing = -32601
)

// Server 提供 MCP stdio 工具服务。
type Server struct {
	config        config.Config
	embedder      embed.Embedder
	vectorStore   vectorstore.VectorStore
	snapshotStore *snapshot.Store
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initializeResponse struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    serverCapabilities `json:"capabilities"`
	ServerInfo      serverInfo         `json:"serverInfo"`
}

type serverCapabilities struct {
	Tools map[string]any `json:"tools"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type toolsListResponse struct {
	Tools []toolDefinition `json:"tools"`
}

type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type callToolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type indexCodebaseArgs struct {
	Path string `json:"path"`
}

type searchCodeArgs struct {
	Path  string `json:"path"`
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type clearIndexArgs struct {
	Path string `json:"path"`
}

type indexingStatusArgs struct {
	Path string `json:"path"`
}

type indexCodebaseResponse struct {
	Path          string `json:"path"`
	Namespace     string `json:"namespace"`
	IndexedFiles  int    `json:"indexed_files"`
	TotalChunks   int    `json:"total_chunks"`
	AddedFiles    int    `json:"added_files"`
	ModifiedFiles int    `json:"modified_files"`
	RemovedFiles  int    `json:"removed_files"`
	FullReindex   bool   `json:"full_reindex"`
}

type searchCodeResponse struct {
	Path        string            `json:"path"`
	Namespace   string            `json:"namespace"`
	Query       string            `json:"query"`
	Limit       int               `json:"limit"`
	ResultCount int               `json:"result_count"`
	Results     []searchCodeMatch `json:"results"`
}

type searchCodeMatch struct {
	Rank          int               `json:"rank"`
	Score         float64           `json:"score"`
	RelativePath  string            `json:"relative_path"`
	StartLine     int               `json:"start_line"`
	EndLine       int               `json:"end_line"`
	FileExtension string            `json:"file_extension"`
	Language      string            `json:"language"`
	Content       string            `json:"content"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type clearIndexResponse struct {
	Path      string `json:"path"`
	Namespace string `json:"namespace"`
	Cleared   bool   `json:"cleared"`
}

type indexingStatusResponse struct {
	Path         string `json:"path"`
	Namespace    string `json:"namespace"`
	Status       string `json:"status"`
	IndexedFiles int    `json:"indexed_files,omitempty"`
	TotalChunks  int    `json:"total_chunks,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// NewServer 创建 MCP Server。
func NewServer(ctx context.Context, cfg config.Config) (*Server, error) {
	embedderInstance, err := newEmbedder(cfg)
	if err != nil {
		return nil, err
	}
	vectorStoreInstance, err := newVectorStore(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Server{
		config:        cfg,
		embedder:      embedderInstance,
		vectorStore:   vectorStoreInstance,
		snapshotStore: snapshot.NewStore(cfg.StorageDir),
	}, nil
}

// ToolNames 返回暴露给 MCP Client 的工具名称。
func ToolNames() []string {
	return []string{
		"index_codebase",
		"search_code",
		"clear_index",
		"get_indexing_status",
	}
}

// Serve 通过 stdio 读写 MCP JSON-RPC 消息。
func (s *Server) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	reader := bufio.NewReader(input)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		payload, err := readProtocolMessage(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		response := s.handlePayload(ctx, payload)
		if response == nil {
			continue
		}
		if err := writeProtocolMessage(output, response); err != nil {
			return err
		}
	}
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
	case "ollama":
		return embed.NewOllamaEmbedder(embed.OllamaOptions{
			Host:  cfg.OllamaHost,
			Model: cfg.OllamaEmbeddingModel,
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

func (s *Server) handlePayload(ctx context.Context, payload []byte) *jsonRPCResponse {
	var request jsonRPCRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return newErrorResponse(nil, jsonRPCParseError, "parse error")
	}
	if len(request.ID) == 0 {
		return nil
	}
	return s.handleRequest(ctx, request)
}

func (s *Server) handleRequest(ctx context.Context, request jsonRPCRequest) *jsonRPCResponse {
	switch request.Method {
	case "initialize":
		return newResultResponse(request.ID, initializeResponse{
			ProtocolVersion: mcpProtocolVersion,
			Capabilities:    serverCapabilities{Tools: map[string]any{}},
			ServerInfo:      serverInfo{Name: mcpServerName, Version: mcpServerVersion},
		})
	case "ping":
		return newResultResponse(request.ID, map[string]any{})
	case "tools/list":
		return newResultResponse(request.ID, toolsListResponse{Tools: toolDefinitions()})
	case "tools/call":
		result, err := s.handleToolsCall(ctx, request.Params)
		if err != nil {
			return newErrorResponse(request.ID, jsonRPCInvalidParams, err.Error())
		}
		return newResultResponse(request.ID, result)
	default:
		return newErrorResponse(request.ID, jsonRPCMethodMissing, fmt.Sprintf("method not found: %s", request.Method))
	}
}

func (s *Server) handleToolsCall(ctx context.Context, params json.RawMessage) (callToolResult, error) {
	var request toolCallParams
	if err := json.Unmarshal(params, &request); err != nil {
		return callToolResult{}, err
	}
	result, err := s.callTool(ctx, request.Name, request.Arguments)
	if err != nil {
		return errorToolResult(err), nil
	}
	return result, nil
}

func (s *Server) callTool(ctx context.Context, name string, arguments json.RawMessage) (callToolResult, error) {
	switch name {
	case "index_codebase":
		return s.callIndexCodebase(ctx, arguments)
	case "search_code":
		return s.callSearchCode(ctx, arguments)
	case "clear_index":
		return s.callClearIndex(ctx, arguments)
	case "get_indexing_status":
		return s.callGetIndexingStatus(arguments)
	default:
		return callToolResult{}, fmt.Errorf("unknown tool %q", name)
	}
}

func (s *Server) callIndexCodebase(ctx context.Context, rawArgs json.RawMessage) (callToolResult, error) {
	var args indexCodebaseArgs
	if err := decodeToolArguments(rawArgs, &args); err != nil {
		return callToolResult{}, err
	}
	if strings.TrimSpace(args.Path) == "" {
		return callToolResult{}, errors.New("path is required")
	}
	stats, err := s.indexPath(ctx, args.Path)
	if err != nil {
		return callToolResult{}, err
	}
	return jsonToolResult(indexCodebaseResponse{
		Path:          stats.Path,
		Namespace:     stats.Namespace,
		IndexedFiles:  stats.IndexedFiles,
		TotalChunks:   stats.TotalChunks,
		AddedFiles:    stats.AddedFiles,
		ModifiedFiles: stats.ModifiedFiles,
		RemovedFiles:  stats.RemovedFiles,
		FullReindex:   stats.FullReindex,
	})
}

func (s *Server) callSearchCode(ctx context.Context, rawArgs json.RawMessage) (callToolResult, error) {
	var args searchCodeArgs
	if err := decodeToolArguments(rawArgs, &args); err != nil {
		return callToolResult{}, err
	}
	if strings.TrimSpace(args.Path) == "" {
		return callToolResult{}, errors.New("path is required")
	}
	if strings.TrimSpace(args.Query) == "" {
		return callToolResult{}, errors.New("query is required")
	}
	limit := args.Limit
	if limit <= 0 {
		limit = s.config.SearchLimit
	}
	strategy := s.config.SearchStrategy(config.SearchStrategyCode)
	searcherInstance := searcher.New(s.embedder, s.vectorStore)
	matches, err := searcherInstance.Search(ctx, args.Path, args.Query, vectorstore.SearchOptions{
		Limit:          limit,
		Query:          args.Query,
		SemanticWeight: strategy.SemanticWeight,
		KeywordWeight:  strategy.KeywordWeight,
	})
	if err != nil {
		if os.IsNotExist(err) {
			return callToolResult{}, fmt.Errorf("path is not indexed: %s", args.Path)
		}
		return callToolResult{}, err
	}
	namespace, absolutePath, err := indexer.NamespaceForPath(args.Path)
	if err != nil {
		return callToolResult{}, err
	}
	response := searchCodeResponse{
		Path:      absolutePath,
		Namespace: namespace,
		Query:     args.Query,
		Limit:     limit,
		Results:   makeSearchCodeMatches(matches),
	}
	response.ResultCount = len(response.Results)
	return jsonToolResult(response)
}

func (s *Server) callClearIndex(ctx context.Context, rawArgs json.RawMessage) (callToolResult, error) {
	var args clearIndexArgs
	if err := decodeToolArguments(rawArgs, &args); err != nil {
		return callToolResult{}, err
	}
	if strings.TrimSpace(args.Path) == "" {
		return callToolResult{}, errors.New("path is required")
	}
	namespace, absolutePath, err := indexer.NamespaceForPath(args.Path)
	if err != nil {
		return callToolResult{}, err
	}
	if err := s.vectorStore.Clear(ctx, namespace); err != nil {
		return callToolResult{}, err
	}
	if err := s.snapshotStore.Delete(namespace); err != nil {
		return callToolResult{}, err
	}
	return jsonToolResult(clearIndexResponse{Path: absolutePath, Namespace: namespace, Cleared: true})
}

func (s *Server) callGetIndexingStatus(rawArgs json.RawMessage) (callToolResult, error) {
	var args indexingStatusArgs
	if err := decodeToolArguments(rawArgs, &args); err != nil {
		return callToolResult{}, err
	}
	if strings.TrimSpace(args.Path) == "" {
		return callToolResult{}, errors.New("path is required")
	}
	namespace, absolutePath, err := indexer.NamespaceForPath(args.Path)
	if err != nil {
		return callToolResult{}, err
	}
	info, err := s.snapshotStore.Get(namespace)
	if err != nil {
		if os.IsNotExist(err) {
			return jsonToolResult(indexingStatusResponse{
				Path:      absolutePath,
				Namespace: namespace,
				Status:    "not_indexed",
			})
		}
		return callToolResult{}, err
	}
	return jsonToolResult(indexingStatusResponse{
		Path:         info.Path,
		Namespace:    info.Namespace,
		Status:       string(info.Status),
		IndexedFiles: info.IndexedFiles,
		TotalChunks:  info.TotalChunks,
		UpdatedAt:    info.UpdatedAt.Format("2006-01-02 15:04:05"),
		ErrorMessage: info.ErrorMessage,
	})
}

func (s *Server) indexPath(ctx context.Context, rootPath string) (indexer.Stats, error) {
	scannerInstance := scanner.NewWithPatterns(s.config.SupportedExts, s.config.IgnoreNames, s.config.IgnorePatterns)
	lineSplitter := splitter.NewLineSplitter(s.config.MaxChunkLines, s.config.ChunkOverlapLines)
	splitterInstance := splitter.NewTreeSitterSplitter(s.config.MaxChunkLines, s.config.ChunkOverlapLines, lineSplitter)
	indexerInstance := indexer.New(scannerInstance, splitterInstance, s.embedder, s.vectorStore, s.snapshotStore)
	return indexerInstance.Index(ctx, rootPath)
}

func makeSearchCodeMatches(matches []vectorstore.SearchResult) []searchCodeMatch {
	results := make([]searchCodeMatch, 0, len(matches))
	for index, match := range matches {
		document := match.Document
		results = append(results, searchCodeMatch{
			Rank:          index + 1,
			Score:         match.Score,
			RelativePath:  document.RelativePath,
			StartLine:     document.StartLine,
			EndLine:       document.EndLine,
			FileExtension: document.FileExtension,
			Language:      document.Language,
			Content:       document.Content,
			Metadata:      document.Metadata,
		})
	}
	return results
}

func decodeToolArguments(rawArgs json.RawMessage, target any) error {
	if len(rawArgs) == 0 || bytes.Equal(bytes.TrimSpace(rawArgs), []byte("null")) {
		rawArgs = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(rawArgs))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func jsonToolResult(value any) (callToolResult, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return callToolResult{}, err
	}
	return textToolResult(string(data), false), nil
}

func errorToolResult(err error) callToolResult {
	return textToolResult(err.Error(), true)
}

func textToolResult(text string, isError bool) callToolResult {
	return callToolResult{
		Content: []toolContent{{Type: "text", Text: text}},
		IsError: isError,
	}
}

func toolDefinitions() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "index_codebase",
			Description: "Index or incrementally refresh a local codebase path.",
			InputSchema: objectSchema(map[string]any{
				"path": stringProperty("Local codebase path to index."),
			}, []string{"path"}),
		},
		{
			Name:        "search_code",
			Description: "Search indexed code chunks by semantic and keyword relevance.",
			InputSchema: objectSchema(map[string]any{
				"path":  stringProperty("Indexed local codebase path."),
				"query": stringProperty("Search query."),
				"limit": map[string]any{"type": "integer", "description": "Maximum number of results."},
			}, []string{"path", "query"}),
		},
		{
			Name:        "clear_index",
			Description: "Clear vector data and snapshot state for a codebase path.",
			InputSchema: objectSchema(map[string]any{
				"path": stringProperty("Indexed local codebase path to clear."),
			}, []string{"path"}),
		},
		{
			Name:        "get_indexing_status",
			Description: "Get indexing snapshot status for a codebase path.",
			InputSchema: objectSchema(map[string]any{
				"path": stringProperty("Local codebase path to inspect."),
			}, []string{"path"}),
		},
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func stringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func readProtocolMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && line == "" {
				return nil, io.EOF
			}
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(name), contentLengthHeader) {
			continue
		}
		parsedLength, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || parsedLength < 0 {
			return nil, errors.New("invalid Content-Length header")
		}
		contentLength = parsedLength
	}
	if contentLength < 0 {
		return nil, errors.New("missing Content-Length header")
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeProtocolMessage(output io.Writer, response *jsonRPCResponse) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	_, err = output.Write(payload)
	return err
}

func newResultResponse(id json.RawMessage, result any) *jsonRPCResponse {
	return &jsonRPCResponse{JSONRPC: jsonRPCVersion, ID: id, Result: result}
}

func newErrorResponse(id json.RawMessage, code int, message string) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error:   &jsonRPCError{Code: code, Message: message},
	}
}
