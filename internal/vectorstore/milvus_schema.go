// 文件说明：提供 Milvus schema、字段映射、搜索结果解析和表达式构建辅助函数。
// 实现原理：将 Milvus 存储的字段定义、Document 列转换、搜索结果回填和过滤表达式构建从主存储流程中拆分。
// 使用方式：MilvusStore 写入、检索和清理流程内部调用。
// 注意事项：所有动态表达式参数必须先通过校验函数，避免 Milvus 表达式注入。
// 交互模块：internal/vectorstore/milvus_store.go。
package vectorstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

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
		milvusFieldExperienceKind,
		milvusFieldDocumentID,
		milvusFieldSectionID,
		milvusFieldHeadingPath,
		milvusFieldKnowledgeKind,
		milvusFieldNodeKind,
		milvusFieldVersion,
		milvusFieldSymbolName,
		milvusFieldSymbolKind,
		milvusFieldChunkKind,
		milvusFieldRole,
		milvusFieldToolName,
		milvusFieldCommand,
		milvusFieldStatus,
		milvusFieldTags,
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
		WithField(varcharField(milvusFieldExperienceKind, false, maxMilvusKnowledgeKindLength)).
		WithField(varcharField(milvusFieldDocumentID, false, maxMilvusDocumentIDLength)).
		WithField(varcharField(milvusFieldSectionID, false, maxMilvusSectionIDLength)).
		WithField(varcharField(milvusFieldHeadingPath, false, maxMilvusHeadingPathLength)).
		WithField(varcharField(milvusFieldKnowledgeKind, false, maxMilvusKnowledgeKindLength)).
		WithField(varcharField(milvusFieldNodeKind, false, maxMilvusNodeKindLength)).
		WithField(varcharField(milvusFieldVersion, false, maxMilvusVersionLength)).
		WithField(varcharField(milvusFieldSymbolName, false, maxMilvusSymbolNameLength)).
		WithField(varcharField(milvusFieldSymbolKind, false, maxMilvusSymbolKindLength)).
		WithField(varcharField(milvusFieldChunkKind, false, maxMilvusChunkKindLength)).
		WithField(varcharField(milvusFieldRole, false, maxMilvusRoleLength)).
		WithField(varcharField(milvusFieldToolName, false, maxMilvusToolNameLength)).
		WithField(varcharField(milvusFieldCommand, false, maxMilvusCommandLength)).
		WithField(varcharField(milvusFieldStatus, false, maxMilvusStatusLength)).
		WithField(varcharField(milvusFieldTags, false, maxMilvusTagsLength))
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
	experienceKinds := make([]string, 0, len(documents))
	documentIDs := make([]string, 0, len(documents))
	sectionIDs := make([]string, 0, len(documents))
	headingPaths := make([]string, 0, len(documents))
	knowledgeKinds := make([]string, 0, len(documents))
	nodeKinds := make([]string, 0, len(documents))
	versions := make([]string, 0, len(documents))
	symbolNames := make([]string, 0, len(documents))
	symbolKinds := make([]string, 0, len(documents))
	chunkKinds := make([]string, 0, len(documents))
	roles := make([]string, 0, len(documents))
	toolNames := make([]string, 0, len(documents))
	commands := make([]string, 0, len(documents))
	statuses := make([]string, 0, len(documents))
	tags := make([]string, 0, len(documents))
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
		experienceKinds = append(experienceKinds, truncateUTF8(document.Metadata[MetadataExperienceKind], int(maxMilvusKnowledgeKindLength)))
		documentIDs = append(documentIDs, truncateUTF8(document.Metadata[MetadataDocumentID], int(maxMilvusDocumentIDLength)))
		sectionIDs = append(sectionIDs, truncateUTF8(document.Metadata[MetadataSectionID], int(maxMilvusSectionIDLength)))
		headingPaths = append(headingPaths, truncateUTF8(document.Metadata[MetadataHeadingPath], int(maxMilvusHeadingPathLength)))
		knowledgeKinds = append(knowledgeKinds, truncateUTF8(document.Metadata[MetadataKnowledgeKind], int(maxMilvusKnowledgeKindLength)))
		nodeKinds = append(nodeKinds, truncateUTF8(document.Metadata[MetadataNodeKind], int(maxMilvusNodeKindLength)))
		versions = append(versions, truncateUTF8(document.Metadata[MetadataVersion], int(maxMilvusVersionLength)))
		symbolNames = append(symbolNames, truncateUTF8(document.Metadata[metadataSymbolName], int(maxMilvusSymbolNameLength)))
		symbolKinds = append(symbolKinds, truncateUTF8(document.Metadata[metadataSymbolKind], int(maxMilvusSymbolKindLength)))
		chunkKinds = append(chunkKinds, truncateUTF8(document.Metadata[metadataChunkKind], int(maxMilvusChunkKindLength)))
		roles = append(roles, truncateUTF8(document.Metadata[metadataRole], int(maxMilvusRoleLength)))
		toolNames = append(toolNames, truncateUTF8(document.Metadata[metadataToolName], int(maxMilvusToolNameLength)))
		commands = append(commands, truncateUTF8(document.Metadata[metadataCommand], int(maxMilvusCommandLength)))
		statuses = append(statuses, truncateUTF8(document.Metadata[metadataStatus], int(maxMilvusStatusLength)))
		tags = append(tags, truncateUTF8(document.Metadata[metadataTags], int(maxMilvusTagsLength)))
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
		entity.NewColumnVarChar(milvusFieldExperienceKind, experienceKinds),
		entity.NewColumnVarChar(milvusFieldDocumentID, documentIDs),
		entity.NewColumnVarChar(milvusFieldSectionID, sectionIDs),
		entity.NewColumnVarChar(milvusFieldHeadingPath, headingPaths),
		entity.NewColumnVarChar(milvusFieldKnowledgeKind, knowledgeKinds),
		entity.NewColumnVarChar(milvusFieldNodeKind, nodeKinds),
		entity.NewColumnVarChar(milvusFieldVersion, versions),
		entity.NewColumnVarChar(milvusFieldSymbolName, symbolNames),
		entity.NewColumnVarChar(milvusFieldSymbolKind, symbolKinds),
		entity.NewColumnVarChar(milvusFieldChunkKind, chunkKinds),
		entity.NewColumnVarChar(milvusFieldRole, roles),
		entity.NewColumnVarChar(milvusFieldToolName, toolNames),
		entity.NewColumnVarChar(milvusFieldCommand, commands),
		entity.NewColumnVarChar(milvusFieldStatus, statuses),
		entity.NewColumnVarChar(milvusFieldTags, tags),
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

func milvusSearchLimit(limit int, options SearchOptions) int {
	if limit <= 0 {
		return limit
	}
	if shouldRerankSearchResults(options) {
		return limit * 3
	}
	return limit
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
		milvusFieldExperienceKind,
		milvusFieldDocumentID,
		milvusFieldSectionID,
		milvusFieldHeadingPath,
		milvusFieldKnowledgeKind,
		milvusFieldNodeKind,
		milvusFieldVersion,
		milvusFieldSymbolName,
		milvusFieldSymbolKind,
		milvusFieldChunkKind,
		milvusFieldRole,
		milvusFieldToolName,
		milvusFieldCommand,
		milvusFieldStatus,
		milvusFieldTags,
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
		if index >= len(results[0].Scores) {
			return nil, errors.New("milvus result score count mismatch")
		}
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
	experienceKind, err := columnString(columns, milvusFieldExperienceKind, index)
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
	symbolName, err := columnString(columns, milvusFieldSymbolName, index)
	if err != nil {
		return Document{}, err
	}
	symbolKind, err := columnString(columns, milvusFieldSymbolKind, index)
	if err != nil {
		return Document{}, err
	}
	chunkKind, err := columnString(columns, milvusFieldChunkKind, index)
	if err != nil {
		return Document{}, err
	}
	role, err := columnString(columns, milvusFieldRole, index)
	if err != nil {
		return Document{}, err
	}
	toolName, err := columnString(columns, milvusFieldToolName, index)
	if err != nil {
		return Document{}, err
	}
	command, err := columnString(columns, milvusFieldCommand, index)
	if err != nil {
		return Document{}, err
	}
	status, err := columnString(columns, milvusFieldStatus, index)
	if err != nil {
		return Document{}, err
	}
	tags, err := columnString(columns, milvusFieldTags, index)
	if err != nil {
		return Document{}, err
	}
	metadata := map[string]string{
		"absolute_path": absolutePath,
	}
	putMetadataIfNotEmpty(metadata, MetadataDomainPath, domainPath)
	putMetadataIfNotEmpty(metadata, MetadataExperienceKind, experienceKind)
	putMetadataIfNotEmpty(metadata, MetadataDocumentID, documentID)
	putMetadataIfNotEmpty(metadata, MetadataSectionID, sectionID)
	putMetadataIfNotEmpty(metadata, MetadataHeadingPath, headingPath)
	putMetadataIfNotEmpty(metadata, MetadataKnowledgeKind, knowledgeKind)
	putMetadataIfNotEmpty(metadata, MetadataNodeKind, nodeKind)
	putMetadataIfNotEmpty(metadata, MetadataVersion, version)
	putMetadataIfNotEmpty(metadata, metadataSymbolName, symbolName)
	putMetadataIfNotEmpty(metadata, metadataSymbolKind, symbolKind)
	putMetadataIfNotEmpty(metadata, metadataChunkKind, chunkKind)
	putMetadataIfNotEmpty(metadata, metadataRole, role)
	putMetadataIfNotEmpty(metadata, metadataToolName, toolName)
	putMetadataIfNotEmpty(metadata, metadataCommand, command)
	putMetadataIfNotEmpty(metadata, metadataStatus, status)
	putMetadataIfNotEmpty(metadata, metadataTags, tags)
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

func buildMilvusFileExpr(namespace string, relativePaths []string) (string, error) {
	if err := validateMilvusTokenValue(namespace); err != nil {
		return "", err
	}
	pathExpr, err := buildMilvusInExpr(milvusFieldRelativePath, relativePaths, validateMilvusRelativePath)
	if err != nil {
		return "", err
	}
	if pathExpr == "" {
		return "", errors.New("milvus relative path filter is empty")
	}
	return milvusFieldNamespace + " == " + milvusStringLiteral(namespace) + " and " + pathExpr, nil
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

func validateMilvusRelativePath(value string) error {
	if value == "" {
		return errors.New("milvus relative path is empty")
	}
	for _, char := range value {
		if char == '_' || char == '-' || char == '/' || char == '.' || char == ' ' || unicode.IsLetter(char) || unicode.IsDigit(char) {
			continue
		}
		return fmt.Errorf("invalid milvus relative path %q", value)
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
