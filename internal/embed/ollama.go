// 文件说明：实现 Ollama embedding 客户端。
// 实现原理：通过 Ollama /api/embed HTTP API 批量生成向量，并保持返回顺序与输入文本一致。
// 使用方式：配置 AGENT_MEMORY_EMBEDDING_PROVIDER=ollama 后由 CLI 自动创建。
// 注意事项：Ollama 返回的向量通常已归一化；本实现不额外归一化，避免改变模型输出分布。
// 交互模块：internal/config、cmd/code-context、internal/indexer、internal/searcher。

package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultOllamaHost           = "http://127.0.0.1:11434"
	defaultOllamaEmbeddingModel = "embeddinggemma"
	defaultOllamaHTTPTimeout    = 120 * time.Second
	maxOllamaResponseBytes      = 64 << 20
	maxOllamaErrorPreviewBytes  = 512
)

// OllamaOptions 表示 Ollama embedding 客户端配置。
type OllamaOptions struct {
	Host       string
	Model      string
	Dimensions int
	HTTPClient *http.Client
}

// OllamaEmbedder 通过 Ollama /api/embed 生成文本向量。
type OllamaEmbedder struct {
	host                string
	model               string
	requestedDimensions int
	httpClient          *http.Client
	dimension           int
	mu                  sync.RWMutex
}

// NewOllamaEmbedder 创建 Ollama embedding 客户端。
func NewOllamaEmbedder(options OllamaOptions) (*OllamaEmbedder, error) {
	host := strings.TrimSpace(options.Host)
	if host == "" {
		host = defaultOllamaHost
	}
	model := strings.TrimSpace(options.Model)
	if model == "" {
		model = defaultOllamaEmbeddingModel
	}
	if model == "" {
		return nil, errors.New("ollama embedding model is required")
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultOllamaHTTPTimeout}
	}
	return &OllamaEmbedder{
		host:                strings.TrimRight(host, "/"),
		model:               model,
		requestedDimensions: options.Dimensions,
		httpClient:          httpClient,
	}, nil
}

// Embed 将单段文本转换为向量。
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vectors, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

// EmbedBatch 批量生成文本向量。
func (e *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	requestBody, err := json.Marshal(ollamaEmbedRequest{
		Model:      e.model,
		Input:      texts,
		Dimensions: e.requestedDimensions,
	})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.embedEndpoint(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := e.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxOllamaResponseBytes))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, parseOllamaError(response.StatusCode, responseBody)
	}

	var payload ollamaEmbedResponse
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return nil, err
	}
	return e.orderedEmbeddings(payload, len(texts))
}

// Dimension 返回最近一次成功 embedding 的向量维度。
func (e *OllamaEmbedder) Dimension() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.dimension
}

// Provider 返回向量化提供方名称。
func (e *OllamaEmbedder) Provider() string {
	return "ollama"
}

func (e *OllamaEmbedder) embedEndpoint() string {
	return e.host + "/api/embed"
}

func (e *OllamaEmbedder) orderedEmbeddings(payload ollamaEmbedResponse, textCount int) ([][]float32, error) {
	if len(payload.Embeddings) != textCount {
		return nil, fmt.Errorf("ollama embedding count mismatch: want=%d got=%d", textCount, len(payload.Embeddings))
	}
	expectedDimension := 0
	vectors := make([][]float32, 0, len(payload.Embeddings))
	for index, vector := range payload.Embeddings {
		if len(vector) == 0 {
			return nil, fmt.Errorf("ollama embedding is empty: index=%d", index)
		}
		if expectedDimension == 0 {
			expectedDimension = len(vector)
		}
		if len(vector) != expectedDimension {
			return nil, fmt.Errorf("ollama embedding dimension mismatch: index=%d", index)
		}
		vectors = append(vectors, append([]float32(nil), vector...))
	}
	if err := e.updateDimension(expectedDimension); err != nil {
		return nil, err
	}
	return vectors, nil
}

func (e *OllamaEmbedder) updateDimension(dimension int) error {
	if e.requestedDimensions > 0 && dimension != e.requestedDimensions {
		return fmt.Errorf("ollama embedding dimension mismatch: requested=%d current=%d", e.requestedDimensions, dimension)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.dimension == 0 {
		e.dimension = dimension
		return nil
	}
	if e.dimension != dimension {
		return fmt.Errorf("ollama embedding dimension changed: previous=%d current=%d", e.dimension, dimension)
	}
	return nil
}

func parseOllamaError(statusCode int, responseBody []byte) error {
	var payload ollamaErrorResponse
	if err := json.Unmarshal(responseBody, &payload); err == nil {
		message := strings.TrimSpace(payload.Error)
		if message != "" {
			return fmt.Errorf("ollama embedding request failed: status=%d message=%s", statusCode, message)
		}
	}
	preview := strings.TrimSpace(string(responseBody))
	if len(preview) > maxOllamaErrorPreviewBytes {
		preview = preview[:maxOllamaErrorPreviewBytes]
	}
	return fmt.Errorf("ollama embedding request failed: status=%d body=%s", statusCode, preview)
}

type ollamaEmbedRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

type ollamaErrorResponse struct {
	Error string `json:"error"`
}
