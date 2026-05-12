// 文件说明：实现基于 Milvus 的向量存储。
// 实现原理：使用单个 collection 存储所有代码库 chunk，通过 namespace 字段隔离不同代码库索引。
// 使用方式：配置 AGENT_MEMORY_VECTOR_STORE=milvus 后由 CLI 自动创建并连接 Milvus。
// 注意事项：Milvus collection 名称和过滤表达式参数会先校验再使用，避免动态表达式注入风险。
// 交互模块：internal/config、cmd/code-context、internal/indexer、internal/searcher。

package vectorstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

const (
	defaultMilvusCollection      = "agent_memory_chunks"
	defaultMilvusShards          = int32(2)
	maxMilvusQueryCount          = int64(16384)
	maxMilvusDocIDLength         = int64(128)
	maxMilvusNamespaceLen        = int64(128)
	maxMilvusPathLength          = int64(2048)
	maxMilvusExtLength           = int64(32)
	maxMilvusLanguageLength      = int64(64)
	maxMilvusDomainPathLength    = int64(256)
	maxMilvusDocumentIDLength    = int64(256)
	maxMilvusSectionIDLength     = int64(512)
	maxMilvusHeadingPathLength   = int64(2048)
	maxMilvusKnowledgeKindLength = int64(64)
	maxMilvusNodeKindLength      = int64(64)
	maxMilvusVersionLength       = int64(128)
	maxMilvusSymbolNameLength    = int64(512)
	maxMilvusSymbolKindLength    = int64(64)
	maxMilvusChunkKindLength     = int64(64)
	maxMilvusRoleLength          = int64(64)
	maxMilvusToolNameLength      = int64(128)
	maxMilvusCommandLength       = int64(2048)
	maxMilvusStatusLength        = int64(64)
	maxMilvusTagsLength          = int64(2048)
	maxMilvusContentLength       = int64(65535)
)

const (
	milvusFieldDocID          = "doc_id"
	milvusFieldNamespace      = "namespace"
	milvusFieldVector         = "vector"
	milvusFieldContent        = "content"
	milvusFieldRelativePath   = "relative_path"
	milvusFieldStartLine      = "start_line"
	milvusFieldEndLine        = "end_line"
	milvusFieldFileExtension  = "file_extension"
	milvusFieldLanguage       = "language"
	milvusFieldAbsolutePath   = "absolute_path"
	milvusFieldDomainPath     = "domain_path"
	milvusFieldExperienceKind = "experience_kind"
	milvusFieldDocumentID     = "document_id"
	milvusFieldSectionID      = "section_id"
	milvusFieldHeadingPath    = "heading_path"
	milvusFieldKnowledgeKind  = "knowledge_kind"
	milvusFieldNodeKind       = "node_kind"
	milvusFieldVersion        = "version"
	milvusFieldSymbolName     = "symbol_name"
	milvusFieldSymbolKind     = "symbol_kind"
	milvusFieldChunkKind      = "chunk_kind"
	milvusFieldRole           = "role"
	milvusFieldToolName       = "tool_name"
	milvusFieldCommand        = "command"
	milvusFieldStatus         = "status"
	milvusFieldTags           = "tags"
)

// MilvusOptions 表示 Milvus 向量存储配置。
type MilvusOptions struct {
	Address        string
	Username       string
	Password       string
	CollectionName string
}

// MilvusStore 使用 Milvus 持久化和检索向量文档。
type MilvusStore struct {
	client         milvusClient
	collectionName string
}

type milvusClient interface {
	HasCollection(ctx context.Context, collName string) (bool, error)
	CreateCollection(ctx context.Context, schema *entity.Schema, shardsNum int32, opts ...client.CreateCollectionOption) error
	DescribeCollection(ctx context.Context, collName string) (*entity.Collection, error)
	CreateIndex(ctx context.Context, collName string, fieldName string, idx entity.Index, async bool, opts ...client.IndexOption) error
	GetLoadState(ctx context.Context, collectionName string, partitionNames []string) (entity.LoadState, error)
	LoadCollection(ctx context.Context, collName string, async bool, opts ...client.LoadCollectionOption) error
	Insert(ctx context.Context, collName string, partitionName string, columns ...entity.Column) (entity.Column, error)
	Flush(ctx context.Context, collName string, async bool, opts ...client.FlushOption) error
	Delete(ctx context.Context, collName string, partitionName string, expr string) error
	Search(ctx context.Context, collName string, partitions []string, expr string, outputFields []string,
		vectors []entity.Vector, vectorField string, metricType entity.MetricType, topK int,
		sp entity.SearchParam, opts ...client.SearchQueryOptionFunc) ([]client.SearchResult, error)
	Query(ctx context.Context, collectionName string, partitionNames []string, expr string, outputFields []string,
		opts ...client.SearchQueryOptionFunc) (client.ResultSet, error)
}

// NewMilvusStore 创建 Milvus 向量存储。
func NewMilvusStore(ctx context.Context, options MilvusOptions) (*MilvusStore, error) {
	collectionName := strings.TrimSpace(options.CollectionName)
	if collectionName == "" {
		collectionName = defaultMilvusCollection
	}
	if err := validateMilvusIdentifier(collectionName); err != nil {
		return nil, err
	}
	address := strings.TrimSpace(options.Address)
	if address == "" {
		return nil, errors.New("milvus address is required")
	}

	milvusClient, err := client.NewClient(ctx, client.Config{
		Address:  address,
		Username: strings.TrimSpace(options.Username),
		Password: strings.TrimSpace(options.Password),
	})
	if err != nil {
		return nil, err
	}
	return &MilvusStore{client: milvusClient, collectionName: collectionName}, nil
}

// Put 覆盖写入指定 namespace 的向量文档。
func (s *MilvusStore) Put(ctx context.Context, namespace string, documents []Document) error {
	if err := validateMilvusTokenValue(namespace); err != nil {
		return err
	}
	if len(documents) == 0 {
		return s.Clear(ctx, namespace)
	}
	dimension, err := documentDimension(documents)
	if err != nil {
		return err
	}
	if err := s.ensureCollection(ctx, dimension); err != nil {
		return err
	}
	if err := s.Clear(ctx, namespace); err != nil {
		return err
	}

	if _, err := s.client.Insert(ctx, s.collectionName, "", documentColumns(documents)...); err != nil {
		return err
	}
	return s.client.Flush(ctx, s.collectionName, false)
}

// ReplaceFiles 替换指定 namespace 中若干文件对应的向量文档。
func (s *MilvusStore) ReplaceFiles(
	ctx context.Context,
	namespace string,
	relativePaths []string,
	documents []Document,
) error {
	if err := validateMilvusTokenValue(namespace); err != nil {
		return err
	}
	if len(relativePaths) == 0 && len(documents) == 0 {
		return nil
	}
	if len(documents) > 0 {
		dimension, err := documentDimension(documents)
		if err != nil {
			return err
		}
		if err := s.ensureCollection(ctx, dimension); err != nil {
			return err
		}
	} else {
		exists, err := s.client.HasCollection(ctx, s.collectionName)
		if err != nil || !exists {
			return err
		}
	}
	if len(relativePaths) > 0 {
		if err := s.deleteFiles(ctx, namespace, relativePaths); err != nil {
			return err
		}
	}
	if len(documents) > 0 {
		if _, err := s.client.Insert(ctx, s.collectionName, "", documentColumns(documents)...); err != nil {
			return err
		}
	}
	return s.client.Flush(ctx, s.collectionName, false)
}

// Search 在指定 namespace 中执行 Milvus TopK 检索。
func (s *MilvusStore) Search(ctx context.Context, namespace string, queryVector []float32, options SearchOptions) ([]SearchResult, error) {
	if err := validateMilvusTokenValue(namespace); err != nil {
		return nil, err
	}
	if len(queryVector) == 0 {
		return nil, errors.New("query vector is empty")
	}
	if err := s.ensureCollection(ctx, len(queryVector)); err != nil {
		return nil, err
	}
	if err := s.ensureLoaded(ctx); err != nil {
		return nil, err
	}

	limit := options.Limit
	if limit <= 0 {
		limit = len(queryVector)
	}
	searchParam, err := entity.NewIndexFlatSearchParam()
	if err != nil {
		return nil, err
	}
	expr, err := buildMilvusExpr(namespace, options)
	if err != nil {
		return nil, err
	}
	count, err := s.countByExpr(ctx, expr)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}

	searchLimit := milvusSearchLimit(limit, options)
	results, err := s.client.Search(ctx, s.collectionName, []string{}, expr, milvusOutputFields(),
		[]entity.Vector{entity.FloatVector(queryVector)}, milvusFieldVector, entity.COSINE, searchLimit, searchParam)
	if err != nil {
		return nil, err
	}
	matches, err := milvusSearchResults(results)
	if err != nil {
		return nil, err
	}
	if shouldRerankSearchResults(options) {
		return rerankSearchResults(matches, options, limit), nil
	}
	if limit > 0 && len(matches) > limit {
		return matches[:limit], nil
	}
	return matches, nil
}

// Clear 删除指定 namespace 的 Milvus 向量数据。
func (s *MilvusStore) Clear(ctx context.Context, namespace string) error {
	if err := validateMilvusTokenValue(namespace); err != nil {
		return err
	}
	exists, err := s.client.HasCollection(ctx, s.collectionName)
	if err != nil || !exists {
		return err
	}
	if err := s.ensureLoaded(ctx); err != nil {
		return err
	}
	expr, err := buildMilvusExpr(namespace, SearchOptions{})
	if err != nil {
		return err
	}
	if err := s.client.Delete(ctx, s.collectionName, "", expr); err != nil {
		return err
	}
	return s.client.Flush(ctx, s.collectionName, false)
}

// Count 返回指定 namespace 的文档数量。
func (s *MilvusStore) Count(ctx context.Context, namespace string) (int, error) {
	if err := validateMilvusTokenValue(namespace); err != nil {
		return 0, err
	}
	exists, err := s.client.HasCollection(ctx, s.collectionName)
	if err != nil || !exists {
		return 0, err
	}
	if err := s.ensureLoaded(ctx); err != nil {
		return 0, err
	}
	expr, err := buildMilvusExpr(namespace, SearchOptions{})
	if err != nil {
		return 0, err
	}
	return s.countByExpr(ctx, expr)
}

func (s *MilvusStore) countByExpr(ctx context.Context, expr string) (int, error) {
	columns, err := s.client.Query(ctx, s.collectionName, []string{}, expr, []string{milvusFieldDocID}, client.WithLimit(maxMilvusQueryCount))
	if err != nil {
		return 0, err
	}
	if len(columns) == 0 {
		return 0, nil
	}
	return columns[0].Len(), nil
}

func (s *MilvusStore) deleteFiles(ctx context.Context, namespace string, relativePaths []string) error {
	if err := s.ensureLoaded(ctx); err != nil {
		return err
	}
	expr, err := buildMilvusFileExpr(namespace, relativePaths)
	if err != nil {
		return err
	}
	return s.client.Delete(ctx, s.collectionName, "", expr)
}

func (s *MilvusStore) ensureCollection(ctx context.Context, dimension int) error {
	exists, err := s.client.HasCollection(ctx, s.collectionName)
	if err != nil {
		return err
	}
	if exists {
		return s.ensureCollectionDimension(ctx, dimension)
	}

	schema := milvusSchema(s.collectionName, dimension)
	if err := s.client.CreateCollection(ctx, schema, defaultMilvusShards); err != nil {
		return err
	}
	index, err := entity.NewIndexFlat(entity.COSINE)
	if err != nil {
		return err
	}
	return s.client.CreateIndex(ctx, s.collectionName, milvusFieldVector, index, false)
}

func (s *MilvusStore) ensureCollectionDimension(ctx context.Context, dimension int) error {
	collection, err := s.client.DescribeCollection(ctx, s.collectionName)
	if err != nil {
		return err
	}
	if err := ensureMilvusSchemaFields(collection.Schema); err != nil {
		return err
	}
	for _, field := range collection.Schema.Fields {
		if field.Name != milvusFieldVector {
			continue
		}
		actualDimension, err := strconv.Atoi(field.TypeParams[entity.TypeParamDim])
		if err != nil {
			return err
		}
		if actualDimension != dimension {
			return fmt.Errorf("milvus vector dimension mismatch: collection=%d current=%d", actualDimension, dimension)
		}
		return nil
	}
	return errors.New("milvus vector field is missing")
}

func ensureMilvusSchemaFields(schema *entity.Schema) error {
	fields := make(map[string]struct{}, len(schema.Fields))
	for _, field := range schema.Fields {
		fields[field.Name] = struct{}{}
	}
	for _, required := range requiredMilvusFields() {
		if _, ok := fields[required]; !ok {
			return fmt.Errorf("milvus collection schema is missing field %s; recreate collection", required)
		}
	}
	return nil
}
