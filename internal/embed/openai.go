// 文件说明：实现 OpenAI-compatible embedding 客户端。
// 实现原理：通过 OpenAI embeddings HTTP API 批量生成向量，并保持返回顺序与输入文本一致。
// 使用方式：配置 AGENT_MEMORY_EMBEDDING_PROVIDER=openai 后由 CLI 自动创建。
// 注意事项：API Key 只从环境变量读取，不会写入日志或持久化文件。
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
	defaultOpenAIBaseURL        = "https://api.openai.com/v1"
	defaultOpenAIEmbeddingModel = "text-embedding-3-small"
	defaultOpenAIMaxBatchSize   = 10
	defaultOpenAIHTTPTimeout    = 60 * time.Second
	maxOpenAIResponseBytes      = 16 << 20
	maxOpenAIErrorPreviewBytes  = 512
)

// OpenAIOptions 表示 OpenAI-compatible embedding 客户端配置。
type OpenAIOptions struct {
	BaseURL      string
	APIKey       string
	Model        string
	Dimensions   int
	MaxBatchSize int
	HTTPClient   *http.Client
}

// OpenAIEmbedder 通过 OpenAI-compatible embeddings API 生成文本向量。
type OpenAIEmbedder struct {
	baseURL             string
	apiKey              string
	model               string
	requestedDimensions int
	maxBatchSize        int
	httpClient          *http.Client
	dimension           int
	mu                  sync.RWMutex
}

// NewOpenAIEmbedder 创建 OpenAI-compatible embedding 客户端。
func NewOpenAIEmbedder(options OpenAIOptions) (*OpenAIEmbedder, error) {
	apiKey := strings.TrimSpace(options.APIKey)
	if apiKey == "" {
		return nil, errors.New("openai api key is required")
	}

	baseURL := strings.TrimSpace(options.BaseURL)
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	model := strings.TrimSpace(options.Model)
	if model == "" {
		model = defaultOpenAIEmbeddingModel
	}

	maxBatchSize := options.MaxBatchSize
	if maxBatchSize <= 0 {
		maxBatchSize = defaultOpenAIMaxBatchSize
	}

	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultOpenAIHTTPTimeout}
	}

	return &OpenAIEmbedder{
		baseURL:             strings.TrimRight(baseURL, "/"),
		apiKey:              apiKey,
		model:               model,
		requestedDimensions: options.Dimensions,
		maxBatchSize:        maxBatchSize,
		httpClient:          httpClient,
	}, nil
}

// Embed 将单段文本转换为向量。
func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vectors, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

// EmbedBatch 批量生成文本向量。
func (e *OpenAIEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	vectors := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += e.maxBatchSize {
		end := min(start+e.maxBatchSize, len(texts))
		batchVectors, err := e.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		vectors = append(vectors, batchVectors...)
	}
	return vectors, nil
}

func (e *OpenAIEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	requestBody, err := json.Marshal(openAIEmbeddingRequest{
		Model:      e.model,
		Input:      texts,
		Dimensions: e.requestedDimensions,
	})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.embeddingsEndpoint(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+e.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := e.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxOpenAIResponseBytes))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, parseOpenAIError(response.StatusCode, responseBody)
	}

	var payload openAIEmbeddingResponse
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return nil, err
	}
	return e.orderedEmbeddings(payload, len(texts))
}

// Dimension 返回最近一次成功 embedding 的向量维度。
func (e *OpenAIEmbedder) Dimension() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.dimension
}

// Provider 返回向量化提供方名称。
func (e *OpenAIEmbedder) Provider() string {
	return "openai"
}

func (e *OpenAIEmbedder) embeddingsEndpoint() string {
	return e.baseURL + "/embeddings"
}

func (e *OpenAIEmbedder) orderedEmbeddings(payload openAIEmbeddingResponse, textCount int) ([][]float32, error) {
	if len(payload.Data) != textCount {
		return nil, fmt.Errorf("openai embedding data count mismatch: want=%d got=%d", textCount, len(payload.Data))
	}

	ordered := make([][]float32, textCount)
	seen := make([]bool, textCount)
	expectedDimension := 0
	for _, item := range payload.Data {
		if item.Index < 0 || item.Index >= textCount {
			return nil, fmt.Errorf("openai embedding index out of range: index=%d", item.Index)
		}
		if seen[item.Index] {
			return nil, fmt.Errorf("openai embedding index duplicated: index=%d", item.Index)
		}
		if len(item.Embedding) == 0 {
			return nil, fmt.Errorf("openai embedding is empty: index=%d", item.Index)
		}
		if expectedDimension == 0 {
			expectedDimension = len(item.Embedding)
		}
		if len(item.Embedding) != expectedDimension {
			return nil, fmt.Errorf("openai embedding dimension mismatch: index=%d", item.Index)
		}
		ordered[item.Index] = append([]float32(nil), item.Embedding...)
		seen[item.Index] = true
	}
	for index, ok := range seen {
		if !ok {
			return nil, fmt.Errorf("openai embedding index missing: index=%d", index)
		}
	}
	if err := e.updateDimension(expectedDimension); err != nil {
		return nil, err
	}
	return ordered, nil
}

func (e *OpenAIEmbedder) updateDimension(dimension int) error {
	if e.requestedDimensions > 0 && dimension != e.requestedDimensions {
		return fmt.Errorf("openai embedding dimension mismatch: requested=%d current=%d", e.requestedDimensions, dimension)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.dimension == 0 {
		e.dimension = dimension
		return nil
	}
	if e.dimension != dimension {
		return fmt.Errorf("openai embedding dimension changed: previous=%d current=%d", e.dimension, dimension)
	}
	return nil
}

func parseOpenAIError(statusCode int, responseBody []byte) error {
	var payload openAIErrorResponse
	if err := json.Unmarshal(responseBody, &payload); err == nil {
		message := strings.TrimSpace(payload.Error.Message)
		if message != "" {
			return fmt.Errorf("openai embedding request failed: status=%d message=%s", statusCode, message)
		}
	}
	preview := strings.TrimSpace(string(responseBody))
	if len(preview) > maxOpenAIErrorPreviewBytes {
		preview = preview[:maxOpenAIErrorPreviewBytes]
	}
	return fmt.Errorf("openai embedding request failed: status=%d body=%s", statusCode, preview)
}

type openAIEmbeddingRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type openAIEmbeddingResponse struct {
	Data []openAIEmbeddingData `json:"data"`
}

type openAIEmbeddingData struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type openAIErrorResponse struct {
	Error openAIError `json:"error"`
}

type openAIError struct {
	Message string `json:"message"`
}
