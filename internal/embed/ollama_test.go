// 文件说明：测试 Ollama embedding 客户端。
// 实现原理：使用 httptest 模拟 Ollama /api/embed 响应，验证请求体、批量向量顺序、维度记录和错误解析。
// 使用方式：执行 go test ./internal/embed 或 go test ./...。
// 注意事项：测试不连接真实 Ollama 服务。
// 交互模块：internal/embed。

package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOllamaEmbedder_EmbedBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/api/embed" {
			t.Fatalf("path = %s, want /api/embed", r.URL.Path)
		}
		var request ollamaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Model != "test-model" {
			t.Fatalf("model = %s, want test-model", request.Model)
		}
		if strings.Join(request.Input, ",") != "first,second" {
			t.Fatalf("input = %v, want [first second]", request.Input)
		}
		if request.Dimensions != 384 {
			t.Fatalf("dimensions = %d, want 384", request.Dimensions)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ollamaEmbedResponse{
			Embeddings: [][]float32{
				make([]float32, 384),
				make([]float32, 384),
			},
		})
	}))
	defer server.Close()

	embedder, err := NewOllamaEmbedder(OllamaOptions{
		Host:       server.URL,
		Model:      "test-model",
		Dimensions: 384,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewOllamaEmbedder() error = %v", err)
	}
	vectors, err := embedder.EmbedBatch(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("EmbedBatch() error = %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("len(vectors) = %d, want 2", len(vectors))
	}
	if len(vectors[0]) != 384 || len(vectors[1]) != 384 {
		t.Fatalf("vector dimensions = %d/%d, want 384/384", len(vectors[0]), len(vectors[1]))
	}
	if embedder.Dimension() != 384 {
		t.Fatalf("Dimension() = %d, want 384", embedder.Dimension())
	}
}

func TestOllamaEmbedder_EmbedBatchReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(ollamaErrorResponse{Error: "missing model"})
	}))
	defer server.Close()

	embedder, err := NewOllamaEmbedder(OllamaOptions{Host: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("NewOllamaEmbedder() error = %v", err)
	}
	_, err = embedder.EmbedBatch(context.Background(), []string{"text"})
	if err == nil || !strings.Contains(err.Error(), "missing model") {
		t.Fatalf("EmbedBatch() error = %v, want missing model", err)
	}
}
