// 文件说明：实现基于本地 JSON 文件的向量存储。
// 实现原理：每个 namespace 对应一个 JSON 文件，检索时加载全部文档并计算余弦相似度。
// 使用方式：MVP 默认使用 LocalStore，后续可替换为 MilvusVectorStore。
// 注意事项：本实现适合小规模开发验证，不适合大型代码库生产检索。
// 交互模块：internal/indexer、internal/searcher、internal/config。

package vectorstore

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
)

const vectorDirName = "vectors"

// LocalStore 使用 JSON 文件持久化向量文档。
type LocalStore struct {
	storageDir string
}

// NewLocalStore 创建本地向量存储。
func NewLocalStore(storageDir string) *LocalStore {
	return &LocalStore{storageDir: storageDir}
}

// Put 覆盖写入指定 namespace 的向量文档。
func (s *LocalStore) Put(ctx context.Context, namespace string, documents []Document) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := os.MkdirAll(s.vectorDir(), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(documents, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.namespacePath(namespace), data, 0644)
}

// Search 在指定 namespace 中执行余弦相似度 TopK 检索。
func (s *LocalStore) Search(ctx context.Context, namespace string, queryVector []float32, options SearchOptions) ([]SearchResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	documents, err := s.load(namespace)
	if err != nil {
		return nil, err
	}

	extensionSet := makeExtensionSet(options.ExtensionFilters)
	results := make([]SearchResult, 0, len(documents))
	for _, document := range documents {
		if len(extensionSet) > 0 {
			if _, ok := extensionSet[document.FileExtension]; !ok {
				continue
			}
		}
		results = append(results, SearchResult{
			Document: document,
			Score:    cosineSimilarity(queryVector, document.Vector),
		})
	}

	sort.SliceStable(results, func(i int, j int) bool {
		return results[i].Score > results[j].Score
	})
	if options.Limit > 0 && len(results) > options.Limit {
		results = results[:options.Limit]
	}
	return results, nil
}

// Clear 删除指定 namespace 的本地向量数据。
func (s *LocalStore) Clear(ctx context.Context, namespace string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	err := os.Remove(s.namespacePath(namespace))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Count 返回指定 namespace 的文档数量。
func (s *LocalStore) Count(ctx context.Context, namespace string) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}
	documents, err := s.load(namespace)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return len(documents), nil
}

func (s *LocalStore) load(namespace string) ([]Document, error) {
	data, err := os.ReadFile(s.namespacePath(namespace))
	if err != nil {
		return nil, err
	}
	var documents []Document
	if err := json.Unmarshal(data, &documents); err != nil {
		return nil, err
	}
	return documents, nil
}

func (s *LocalStore) vectorDir() string {
	return filepath.Join(s.storageDir, vectorDirName)
}

func (s *LocalStore) namespacePath(namespace string) string {
	return filepath.Join(s.vectorDir(), namespace+".json")
}

func makeExtensionSet(extensions []string) map[string]struct{} {
	result := make(map[string]struct{}, len(extensions))
	for _, extension := range extensions {
		if extension != "" {
			result[extension] = struct{}{}
		}
	}
	return result
}

func cosineSimilarity(left []float32, right []float32) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}

	var dotProduct float64
	var leftNorm float64
	var rightNorm float64
	for i := range left {
		leftValue := float64(left[i])
		rightValue := float64(right[i])
		dotProduct += leftValue * rightValue
		leftNorm += leftValue * leftValue
		rightNorm += rightValue * rightValue
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dotProduct / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}
