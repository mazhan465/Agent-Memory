// 文件说明：定义文本向量化接口并实现本地哈希向量化能力。
// 实现原理：将文本拆成 token 后使用 SHA-256 哈希映射到固定维度向量，并做 L2 归一化。
// 使用方式：索引器和搜索器通过 Embedder 接口生成代码片段或查询文本的向量。
// 注意事项：HashEmbedder 仅用于本地闭环验证，不代表真实语义能力；生产环境应替换为 OpenAI/Ollama/Milvus 配套 embedding。
// 交互模块：internal/indexer、internal/searcher、internal/vectorstore。

// Package embed 提供文本向量化能力。
package embed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"regexp"
	"strings"
)

var tokenRegexp = regexp.MustCompile(`[A-Za-z0-9_一-龥]+`)

// Embedder 定义文本向量化接口。
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
	Provider() string
}

// HashEmbedder 是无外部依赖的本地向量化实现。
type HashEmbedder struct {
	dimension int
}

// NewHashEmbedder 创建哈希向量化器。
func NewHashEmbedder(dimension int) *HashEmbedder {
	if dimension <= 0 {
		dimension = 256
	}
	return &HashEmbedder{dimension: dimension}
}

// Embed 将文本转换为固定维度向量。
func (e *HashEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	vector := make([]float32, e.dimension)
	tokens := tokenRegexp.FindAllString(strings.ToLower(text), -1)
	if len(tokens) == 0 {
		tokens = []string{""}
	}

	for _, token := range tokens {
		hash := sha256.Sum256([]byte(token))
		index := int(binary.BigEndian.Uint32(hash[:4]) % uint32(e.dimension))
		sign := float32(1)
		if hash[4]%2 == 0 {
			sign = -1
		}
		vector[index] += sign
	}
	normalize(vector)
	return vector, nil
}

// EmbedBatch 批量生成文本向量。
func (e *HashEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, 0, len(texts))
	for _, text := range texts {
		vector, err := e.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		vectors = append(vectors, vector)
	}
	return vectors, nil
}

// Dimension 返回向量维度。
func (e *HashEmbedder) Dimension() int {
	return e.dimension
}

// Provider 返回向量化提供方名称。
func (e *HashEmbedder) Provider() string {
	return "hash"
}

func normalize(vector []float32) {
	var sum float64
	for _, value := range vector {
		sum += float64(value * value)
	}
	if sum == 0 {
		return
	}

	norm := float32(math.Sqrt(sum))
	for i := range vector {
		vector[i] = vector[i] / norm
	}
}
