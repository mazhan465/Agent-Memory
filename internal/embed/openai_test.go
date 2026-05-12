package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIEmbedder_EmbedBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("path = %s, want /v1/embeddings", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-api-key-placeholder" {
			t.Fatalf("authorization header is not set correctly")
		}

		var request openAIEmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Model != "test-model" {
			t.Fatalf("model = %s, want test-model", request.Model)
		}
		if strings.Join(request.Input, ",") != "first,second" {
			t.Fatalf("input = %v, want [first second]", request.Input)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIEmbeddingResponse{Data: []openAIEmbeddingData{
			{Index: 1, Embedding: []float32{0, 1}},
			{Index: 0, Embedding: []float32{1, 0}},
		}})
	}))
	defer server.Close()

	embedder, err := NewOpenAIEmbedder(OpenAIOptions{
		BaseURL:    server.URL + "/v1",
		APIKey:     "test-api-key-placeholder",
		Model:      "test-model",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewOpenAIEmbedder() error = %v", err)
	}

	vectors, err := embedder.EmbedBatch(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("EmbedBatch() error = %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("len(vectors) = %d, want 2", len(vectors))
	}
	if vectors[0][0] != 1 || vectors[0][1] != 0 || vectors[1][0] != 0 || vectors[1][1] != 1 {
		t.Fatalf("vectors = %v, want ordered embeddings", vectors)
	}
	if embedder.Dimension() != 2 {
		t.Fatalf("Dimension() = %d, want 2", embedder.Dimension())
	}
}

func TestOpenAIEmbedder_EmbedBatchReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(openAIErrorResponse{Error: openAIError{Message: "bad request"}})
	}))
	defer server.Close()

	embedder, err := NewOpenAIEmbedder(OpenAIOptions{
		BaseURL:    server.URL,
		APIKey:     "test-api-key-placeholder",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewOpenAIEmbedder() error = %v", err)
	}

	_, err = embedder.EmbedBatch(context.Background(), []string{"text"})
	if err == nil || !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("EmbedBatch() error = %v, want bad request", err)
	}
}
