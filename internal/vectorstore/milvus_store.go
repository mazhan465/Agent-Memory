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
	"unicode"
	"unicode/utf8"

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
	maxMilvusContentLength       = int64(65535)
)

const (
	milvusFieldDocID         = "doc_id"
	milvusFieldNamespace     = "namespace"
	milvusFieldVector        = "vector"
	milvusFieldContent       = "content"
	milvusFieldRelativePath  = "relative_path"
	milvusFieldStartLine     = "start_line"
	milvusFieldEndLine       = "end_line"
	milvusFieldFileExtension = "file_extension"
	milvusFieldLanguage      = "language"
	milvusFieldAbsolutePath  = "absolute_path"
	milvusFieldDomainPath    = "domain_path"
	milvusFieldDocumentID    = "document_id"
	milvusFieldSectionID     = "section_id"
	milvusFieldHeadingPath   = "heading_path"
	milvusFieldKnowledgeKind = "knowledge_kind"
	milvusFieldNodeKind      = "node_kind"
	milvusFieldVersion       = "version"
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

	results, err := s.client.Search(ctx, s.collectionName, []string{}, expr, milvusOutputFields(),
		[]entity.Vector{entity.FloatVector(queryVector)}, milvusFieldVector, entity.COSINE, limit, searchParam)
	if err != nil {
		return nil, err
	}
	return milvusSearchResults(results)
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

func requiredMilvusFields() []string {
	return []string{
		milvusFieldDocID,
		milvusFieldNamespace,
		milvusFieldVector,
		milvusFieldContent,
		milvusFieldRelativePath,
		milvusFieldStartLine,
		milvusFieldEndLine,
		milvusFieldFileExtension,
		milvusFieldLanguage,
		milvusFieldAbsolutePath,
		milvusFieldDomainPath,
		milvusFieldDocumentID,
		milvusFieldSectionID,
		milvusFieldHeadingPath,
		milvusFieldKnowledgeKind,
		milvusFieldNodeKind,
		milvusFieldVersion,
	}
}

func (s *MilvusStore) ensureLoaded(ctx context.Context) error {
	state, err := s.client.GetLoadState(ctx, s.collectionName, []string{})
	if err != nil {
		return err
	}
	if state == entity.LoadStateLoaded {
		return nil
	}
	return s.client.LoadCollection(ctx, s.collectionName, false)
}

func milvusSchema(collectionName string, dimension int) *entity.Schema {
	return entity.NewSchema().
		WithName(collectionName).
		WithDescription("Agent-Memory chunks").
		WithAutoID(false).
		WithField(varcharField(milvusFieldDocID, true, maxMilvusDocIDLength)).
		WithField(varcharField(milvusFieldNamespace, false, maxMilvusNamespaceLen)).
		WithField(entity.NewField().WithName(milvusFieldVector).WithDataType(entity.FieldTypeFloatVector).WithDim(int64(dimension))).
		WithField(varcharField(milvusFieldContent, false, maxMilvusContentLength)).
		WithField(varcharField(milvusFieldRelativePath, false, maxMilvusPathLength)).
		WithField(entity.NewField().WithName(milvusFieldStartLine).WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName(milvusFieldEndLine).WithDataType(entity.FieldTypeInt64)).
		WithField(varcharField(milvusFieldFileExtension, false, maxMilvusExtLength)).
		WithField(varcharField(milvusFieldLanguage, false, maxMilvusLanguageLength)).
		WithField(varcharField(milvusFieldAbsolutePath, false, maxMilvusPathLength)).
		WithField(varcharField(milvusFieldDomainPath, false, maxMilvusDomainPathLength)).
		WithField(varcharField(milvusFieldDocumentID, false, maxMilvusDocumentIDLength)).
		WithField(varcharField(milvusFieldSectionID, false, maxMilvusSectionIDLength)).
		WithField(varcharField(milvusFieldHeadingPath, false, maxMilvusHeadingPathLength)).
		WithField(varcharField(milvusFieldKnowledgeKind, false, maxMilvusKnowledgeKindLength)).
		WithField(varcharField(milvusFieldNodeKind, false, maxMilvusNodeKindLength)).
		WithField(varcharField(milvusFieldVersion, false, maxMilvusVersionLength))
}

func varcharField(name string, primaryKey bool, maxLength int64) *entity.Field {
	return entity.NewField().
		WithName(name).
		WithDataType(entity.FieldTypeVarChar).
		WithMaxLength(maxLength).
		WithIsPrimaryKey(primaryKey)
}

func documentColumns(documents []Document) []entity.Column {
	ids := make([]string, 0, len(documents))
	namespaces := make([]string, 0, len(documents))
	vectors := make([][]float32, 0, len(documents))
	contents := make([]string, 0, len(documents))
	relativePaths := make([]string, 0, len(documents))
	startLines := make([]int64, 0, len(documents))
	endLines := make([]int64, 0, len(documents))
	extensions := make([]string, 0, len(documents))
	languages := make([]string, 0, len(documents))
	absolutePaths := make([]string, 0, len(documents))
	domainPaths := make([]string, 0, len(documents))
	documentIDs := make([]string, 0, len(documents))
	sectionIDs := make([]string, 0, len(documents))
	headingPaths := make([]string, 0, len(documents))
	knowledgeKinds := make([]string, 0, len(documents))
	nodeKinds := make([]string, 0, len(documents))
	versions := make([]string, 0, len(documents))
	for _, document := range documents {
		ids = append(ids, truncateUTF8(document.ID, int(maxMilvusDocIDLength)))
		namespaces = append(namespaces, truncateUTF8(document.Namespace, int(maxMilvusNamespaceLen)))
		vectors = append(vectors, document.Vector)
		contents = append(contents, truncateUTF8(document.Content, int(maxMilvusContentLength)))
		relativePaths = append(relativePaths, truncateUTF8(document.RelativePath, int(maxMilvusPathLength)))
		startLines = append(startLines, int64(document.StartLine))
		endLines = append(endLines, int64(document.EndLine))
		extensions = append(extensions, truncateUTF8(document.FileExtension, int(maxMilvusExtLength)))
		languages = append(languages, truncateUTF8(document.Language, int(maxMilvusLanguageLength)))
		absolutePaths = append(absolutePaths, truncateUTF8(document.Metadata["absolute_path"], int(maxMilvusPathLength)))
		domainPaths = append(domainPaths, truncateUTF8(document.Metadata[MetadataDomainPath], int(maxMilvusDomainPathLength)))
		documentIDs = append(documentIDs, truncateUTF8(document.Metadata[MetadataDocumentID], int(maxMilvusDocumentIDLength)))
		sectionIDs = append(sectionIDs, truncateUTF8(document.Metadata[MetadataSectionID], int(maxMilvusSectionIDLength)))
		headingPaths = append(headingPaths, truncateUTF8(document.Metadata[MetadataHeadingPath], int(maxMilvusHeadingPathLength)))
		knowledgeKinds = append(knowledgeKinds, truncateUTF8(document.Metadata[MetadataKnowledgeKind], int(maxMilvusKnowledgeKindLength)))
		nodeKinds = append(nodeKinds, truncateUTF8(document.Metadata[MetadataNodeKind], int(maxMilvusNodeKindLength)))
		versions = append(versions, truncateUTF8(document.Metadata[MetadataVersion], int(maxMilvusVersionLength)))
	}
	return []entity.Column{
		entity.NewColumnVarChar(milvusFieldDocID, ids),
		entity.NewColumnVarChar(milvusFieldNamespace, namespaces),
		entity.NewColumnFloatVector(milvusFieldVector, len(documents[0].Vector), vectors),
		entity.NewColumnVarChar(milvusFieldContent, contents),
		entity.NewColumnVarChar(milvusFieldRelativePath, relativePaths),
		entity.NewColumnInt64(milvusFieldStartLine, startLines),
		entity.NewColumnInt64(milvusFieldEndLine, endLines),
		entity.NewColumnVarChar(milvusFieldFileExtension, extensions),
		entity.NewColumnVarChar(milvusFieldLanguage, languages),
		entity.NewColumnVarChar(milvusFieldAbsolutePath, absolutePaths),
		entity.NewColumnVarChar(milvusFieldDomainPath, domainPaths),
		entity.NewColumnVarChar(milvusFieldDocumentID, documentIDs),
		entity.NewColumnVarChar(milvusFieldSectionID, sectionIDs),
		entity.NewColumnVarChar(milvusFieldHeadingPath, headingPaths),
		entity.NewColumnVarChar(milvusFieldKnowledgeKind, knowledgeKinds),
		entity.NewColumnVarChar(milvusFieldNodeKind, nodeKinds),
		entity.NewColumnVarChar(milvusFieldVersion, versions),
	}
}

func documentDimension(documents []Document) (int, error) {
	dimension := len(documents[0].Vector)
	if dimension == 0 {
		return 0, errors.New("document vector is empty")
	}
	for _, document := range documents {
		if len(document.Vector) != dimension {
			return 0, errors.New("document vector dimension mismatch")
		}
	}
	return dimension, nil
}

func milvusOutputFields() []string {
	return []string{
		milvusFieldDocID,
		milvusFieldNamespace,
		milvusFieldContent,
		milvusFieldRelativePath,
		milvusFieldStartLine,
		milvusFieldEndLine,
		milvusFieldFileExtension,
		milvusFieldLanguage,
		milvusFieldAbsolutePath,
		milvusFieldDomainPath,
		milvusFieldDocumentID,
		milvusFieldSectionID,
		milvusFieldHeadingPath,
		milvusFieldKnowledgeKind,
		milvusFieldNodeKind,
		milvusFieldVersion,
	}
}

func milvusSearchResults(results []client.SearchResult) ([]SearchResult, error) {
	if len(results) == 0 {
		return nil, nil
	}
	if results[0].Err != nil {
		return nil, results[0].Err
	}
	columns := columnsByName(results[0].Fields)
	searchResults := make([]SearchResult, 0, results[0].ResultCount)
	for index := 0; index < results[0].ResultCount; index++ {
		document, err := milvusDocumentAt(columns, index)
		if err != nil {
			return nil, err
		}
		searchResults = append(searchResults, SearchResult{Document: document, Score: float64(results[0].Scores[index])})
	}
	return searchResults, nil
}

func columnsByName(columns []entity.Column) map[string]entity.Column {
	result := make(map[string]entity.Column, len(columns))
	for _, column := range columns {
		result[column.Name()] = column
	}
	return result
}

func milvusDocumentAt(columns map[string]entity.Column, index int) (Document, error) {
	id, err := columnString(columns, milvusFieldDocID, index)
	if err != nil {
		return Document{}, err
	}
	namespace, err := columnString(columns, milvusFieldNamespace, index)
	if err != nil {
		return Document{}, err
	}
	startLine, err := columnInt(columns, milvusFieldStartLine, index)
	if err != nil {
		return Document{}, err
	}
	endLine, err := columnInt(columns, milvusFieldEndLine, index)
	if err != nil {
		return Document{}, err
	}
	absolutePath, err := columnString(columns, milvusFieldAbsolutePath, index)
	if err != nil {
		return Document{}, err
	}
	content, err := columnString(columns, milvusFieldContent, index)
	if err != nil {
		return Document{}, err
	}
	relativePath, err := columnString(columns, milvusFieldRelativePath, index)
	if err != nil {
		return Document{}, err
	}
	extension, err := columnString(columns, milvusFieldFileExtension, index)
	if err != nil {
		return Document{}, err
	}
	language, err := columnString(columns, milvusFieldLanguage, index)
	if err != nil {
		return Document{}, err
	}
	domainPath, err := columnString(columns, milvusFieldDomainPath, index)
	if err != nil {
		return Document{}, err
	}
	documentID, err := columnString(columns, milvusFieldDocumentID, index)
	if err != nil {
		return Document{}, err
	}
	sectionID, err := columnString(columns, milvusFieldSectionID, index)
	if err != nil {
		return Document{}, err
	}
	headingPath, err := columnString(columns, milvusFieldHeadingPath, index)
	if err != nil {
		return Document{}, err
	}
	knowledgeKind, err := columnString(columns, milvusFieldKnowledgeKind, index)
	if err != nil {
		return Document{}, err
	}
	nodeKind, err := columnString(columns, milvusFieldNodeKind, index)
	if err != nil {
		return Document{}, err
	}
	version, err := columnString(columns, milvusFieldVersion, index)
	if err != nil {
		return Document{}, err
	}
	metadata := map[string]string{
		"absolute_path": absolutePath,
	}
	putMetadataIfNotEmpty(metadata, MetadataDomainPath, domainPath)
	putMetadataIfNotEmpty(metadata, MetadataDocumentID, documentID)
	putMetadataIfNotEmpty(metadata, MetadataSectionID, sectionID)
	putMetadataIfNotEmpty(metadata, MetadataHeadingPath, headingPath)
	putMetadataIfNotEmpty(metadata, MetadataKnowledgeKind, knowledgeKind)
	putMetadataIfNotEmpty(metadata, MetadataNodeKind, nodeKind)
	putMetadataIfNotEmpty(metadata, MetadataVersion, version)
	return Document{
		ID:            id,
		Namespace:     namespace,
		Content:       content,
		RelativePath:  relativePath,
		StartLine:     int(startLine),
		EndLine:       int(endLine),
		FileExtension: extension,
		Language:      language,
		Metadata:      metadata,
	}, nil
}

func putMetadataIfNotEmpty(metadata map[string]string, key string, value string) {
	if value != "" {
		metadata[key] = value
	}
}

func columnString(columns map[string]entity.Column, name string, index int) (string, error) {
	column, ok := columns[name]
	if !ok {
		return "", fmt.Errorf("milvus result field is missing: %s", name)
	}
	return column.GetAsString(index)
}

func columnInt(columns map[string]entity.Column, name string, index int) (int64, error) {
	column, ok := columns[name]
	if !ok {
		return 0, fmt.Errorf("milvus result field is missing: %s", name)
	}
	return column.GetAsInt64(index)
}

func buildMilvusExpr(namespace string, options SearchOptions) (string, error) {
	if err := validateMilvusTokenValue(namespace); err != nil {
		return "", err
	}
	expressions := []string{milvusFieldNamespace + " == " + milvusStringLiteral(namespace)}
	extensionExpr, err := buildMilvusInExpr(milvusFieldFileExtension, options.ExtensionFilters, validateMilvusExtension)
	if err != nil {
		return "", err
	}
	if extensionExpr != "" {
		expressions = append(expressions, extensionExpr)
	}
	metadataExprs, err := buildMilvusMetadataExprs(options)
	if err != nil {
		return "", err
	}
	expressions = append(expressions, metadataExprs...)
	return strings.Join(expressions, " and "), nil
}

func buildMilvusMetadataExprs(options SearchOptions) ([]string, error) {
	filters := []struct {
		field    string
		values   []string
		validate func(string) error
	}{
		{field: milvusFieldDomainPath, values: options.DomainFilters, validate: validateMilvusDomainPath},
		{field: milvusFieldDocumentID, values: options.DocumentFilters, validate: validateMilvusMetadataValue},
		{field: milvusFieldSectionID, values: options.SectionFilters, validate: validateMilvusMetadataValue},
		{field: milvusFieldHeadingPath, values: options.HeadingFilters, validate: validateMilvusMetadataValue},
		{field: milvusFieldKnowledgeKind, values: options.KnowledgeKindFilters, validate: validateMilvusMetadataValue},
		{field: milvusFieldNodeKind, values: options.NodeKindFilters, validate: validateMilvusMetadataValue},
		{field: milvusFieldVersion, values: options.VersionFilters, validate: validateMilvusMetadataValue},
	}
	expressions := make([]string, 0, len(filters))
	for _, filter := range filters {
		expr, err := buildMilvusInExpr(filter.field, filter.values, filter.validate)
		if err != nil {
			return nil, err
		}
		if expr != "" {
			expressions = append(expressions, expr)
		}
	}
	return expressions, nil
}

func buildMilvusInExpr(fieldName string, values []string, validate func(string) error) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	literals := make([]string, 0, len(values))
	for _, value := range values {
		if err := validate(value); err != nil {
			return "", err
		}
		literals = append(literals, milvusStringLiteral(value))
	}
	return fieldName + " in [" + strings.Join(literals, ", ") + "]", nil
}

func milvusStringLiteral(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return "\"" + value + "\""
}

func validateMilvusIdentifier(value string) error {
	if value == "" {
		return errors.New("milvus identifier is empty")
	}
	for index, char := range value {
		if char == '_' || unicode.IsLetter(char) || (index > 0 && unicode.IsDigit(char)) {
			continue
		}
		return fmt.Errorf("invalid milvus identifier %q", value)
	}
	return nil
}

func validateMilvusTokenValue(value string) error {
	if value == "" {
		return errors.New("milvus token value is empty")
	}
	for _, char := range value {
		if char == '_' || unicode.IsLetter(char) || unicode.IsDigit(char) {
			continue
		}
		return fmt.Errorf("invalid milvus token value %q", value)
	}
	return nil
}

func validateMilvusExtension(value string) error {
	if len(value) < 2 || value[0] != '.' {
		return fmt.Errorf("invalid milvus extension %q", value)
	}
	for _, char := range value[1:] {
		if char == '_' || char == '-' || unicode.IsLetter(char) || unicode.IsDigit(char) {
			continue
		}
		return fmt.Errorf("invalid milvus extension %q", value)
	}
	return nil
}

func validateMilvusDomainPath(value string) error {
	if value == "" {
		return errors.New("milvus domain path is empty")
	}
	for _, char := range value {
		if char == '_' || char == '-' || char == '/' || unicode.IsLetter(char) || unicode.IsDigit(char) {
			continue
		}
		return fmt.Errorf("invalid milvus domain path %q", value)
	}
	return nil
}

func validateMilvusMetadataValue(value string) error {
	if value == "" {
		return errors.New("milvus metadata value is empty")
	}
	for _, char := range value {
		if char == '_' || char == '-' || char == '/' || char == '.' || char == ' ' || char == '>' || unicode.IsLetter(char) || unicode.IsDigit(char) {
			continue
		}
		return fmt.Errorf("invalid milvus metadata value %q", value)
	}
	return nil
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	end := 0
	for end < maxBytes {
		r, size := utf8.DecodeRuneInString(value[end:])
		if r == utf8.RuneError || end+size > maxBytes {
			break
		}
		end += size
	}
	return value[:end]
}
