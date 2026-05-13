package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mazhan465/Agent-Memory/internal/config"
	"github.com/mazhan465/Agent-Memory/internal/snapshot"
	"github.com/mazhan465/Agent-Memory/internal/vectorstore"
)

func TestServerServeInitializeAndListTools(t *testing.T) {
	server := &Server{}
	input := frameMessage(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`) +
		frameMessage(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	var output bytes.Buffer

	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	responses := readFramedResponses(t, output.String())
	if len(responses) != 2 {
		t.Fatalf("response count = %d, want 2", len(responses))
	}
	if responses[0].Error != nil {
		t.Fatalf("initialize error = %+v", responses[0].Error)
	}
	listResult := decodeResult[toolsListResponse](t, responses[1])
	toolNames := make([]string, 0, len(listResult.Tools))
	for _, tool := range listResult.Tools {
		toolNames = append(toolNames, tool.Name)
	}
	for _, want := range ToolNames() {
		if !slices.Contains(toolNames, want) {
			t.Fatalf("tools/list names = %v, want %s", toolNames, want)
		}
	}
}

func TestServerToolCallsIndexSearchStatusAndClear(t *testing.T) {
	ctx := context.Background()
	server, err := NewServer(ctx, testConfig(t))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	repo := t.TempDir()
	writeTestCode(t, repo)

	indexResult, err := server.callTool(ctx, "index_codebase", rawArgs(t, map[string]any{"path": repo}))
	if err != nil {
		t.Fatalf("index_codebase error = %v", err)
	}
	if indexResult.IsError {
		t.Fatalf("index_codebase tool error = %s", indexResult.Content[0].Text)
	}

	searchResult, err := server.callTool(ctx, "search_code", rawArgs(t, map[string]any{
		"path":  repo,
		"query": "authenticate user token",
		"limit": 3,
	}))
	if err != nil {
		t.Fatalf("search_code error = %v", err)
	}
	if searchResult.IsError {
		t.Fatalf("search_code tool error = %s", searchResult.Content[0].Text)
	}
	searchResponse := decodeToolText[searchCodeResponse](t, searchResult)
	if searchResponse.ResultCount == 0 {
		t.Fatalf("ResultCount = 0, want positive")
	}
	if searchResponse.Results[0].RelativePath != "auth.go" {
		t.Fatalf("top RelativePath = %s, want auth.go", searchResponse.Results[0].RelativePath)
	}

	statusResult, err := server.callTool(ctx, "get_indexing_status", rawArgs(t, map[string]any{"path": repo}))
	if err != nil {
		t.Fatalf("get_indexing_status error = %v", err)
	}
	statusResponse := decodeToolText[indexingStatusResponse](t, statusResult)
	if statusResponse.Status != "indexed" {
		t.Fatalf("status = %s, want indexed", statusResponse.Status)
	}

	clearResult, err := server.callTool(ctx, "clear_index", rawArgs(t, map[string]any{"path": repo}))
	if err != nil {
		t.Fatalf("clear_index error = %v", err)
	}
	if clearResult.IsError {
		t.Fatalf("clear_index tool error = %s", clearResult.Content[0].Text)
	}
	statusResult, err = server.callTool(ctx, "get_indexing_status", rawArgs(t, map[string]any{"path": repo}))
	if err != nil {
		t.Fatalf("get_indexing_status after clear error = %v", err)
	}
	statusResponse = decodeToolText[indexingStatusResponse](t, statusResult)
	if statusResponse.Status != "not_indexed" {
		t.Fatalf("status after clear = %s, want not_indexed", statusResponse.Status)
	}
}

func TestServerToolsCallReturnsIndexErrorResult(t *testing.T) {
	ctx := context.Background()
	storagePath := t.TempDir()
	cfg := testConfig(t)
	cfg.StorageDir = storagePath
	server := &Server{
		config:        cfg,
		embedder:      &failingMCPEmbedder{err: errors.New("embedding service unavailable")},
		vectorStore:   vectorstore.NewLocalStore(storagePath),
		snapshotStore: snapshot.NewStore(storagePath),
	}
	repo := t.TempDir()
	writeTestCode(t, repo)

	result, err := server.handleToolsCall(ctx, rawArgs(t, map[string]any{
		"name":      "index_codebase",
		"arguments": map[string]any{"path": repo},
	}))
	if err != nil {
		t.Fatalf("handleToolsCall() error = %v", err)
	}
	if !result.IsError {
		t.Fatal("IsError = false, want true")
	}
	if len(result.Content) != 1 || !strings.Contains(result.Content[0].Text, "embedding service unavailable") {
		t.Fatalf("tool error content = %+v, want embedding error", result.Content)
	}
}

func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		StorageDir:           t.TempDir(),
		EmbeddingProvider:    "hash",
		VectorStoreProvider:  "local",
		EmbeddingDimension:   64,
		MaxChunkLines:        80,
		ChunkOverlapLines:    10,
		SearchLimit:          5,
		SupportedExts:        []string{".go"},
		IgnoreNames:          []string{".git"},
		OpenAIBaseURL:        "https://api.openai.com/v1",
		OpenAIEmbeddingModel: "text-embedding-3-small",
		OllamaHost:           "http://127.0.0.1:11434",
		OllamaEmbeddingModel: "embeddinggemma",
		MilvusAddress:        "localhost:19530",
		MilvusCollection:     "agent_memory_chunks",
	}
}

func writeTestCode(t *testing.T, repo string) {
	t.Helper()
	content := []byte(`package auth

func authenticateUser(token string) bool {
	return token == "test-token-placeholder"
}
`)
	if err := os.WriteFile(filepath.Join(repo, "auth.go"), content, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

type failingMCPEmbedder struct {
	err error
}

func (e *failingMCPEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, e.err
}

func (e *failingMCPEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, e.err
}

func (e *failingMCPEmbedder) Dimension() int {
	return 0
}

func (e *failingMCPEmbedder) Provider() string {
	return "failing"
}

func frameMessage(payload string) string {
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(payload), payload)
}

func readFramedResponses(t *testing.T, output string) []jsonRPCResponse {
	t.Helper()
	reader := bufio.NewReader(strings.NewReader(output))
	responses := make([]jsonRPCResponse, 0)
	for {
		payload, err := readProtocolMessage(reader)
		if err != nil {
			break
		}
		var response jsonRPCResponse
		if err := json.Unmarshal(payload, &response); err != nil {
			t.Fatalf("Unmarshal response error = %v", err)
		}
		responses = append(responses, response)
	}
	return responses
}

func decodeResult[T any](t *testing.T, response jsonRPCResponse) T {
	t.Helper()
	var result T
	data, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatalf("Marshal result error = %v", err)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal result error = %v", err)
	}
	return result
}

func rawArgs(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal args error = %v", err)
	}
	return data
}

func decodeToolText[T any](t *testing.T, result callToolResult) T {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("content length = %d, want 1", len(result.Content))
	}
	var value T
	if err := json.Unmarshal([]byte(result.Content[0].Text), &value); err != nil {
		t.Fatalf("Unmarshal tool text error = %v", err)
	}
	return value
}
