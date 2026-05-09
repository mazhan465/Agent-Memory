// 文件说明：提供知识文档和长期记忆导入命令。
// 实现原理：将 Markdown 文档或 JSON/JSONL 记忆记录转成向量文档，按 source namespace 写入 VectorStore 并登记 SourceCatalog。
// 使用方式：runImport 由 CLI import 命令调用，支持 knowledge 和 memory 两类导入。
// 注意事项：memory 导入当前面向基础可用，输入记录必须包含 content 字段。
// 交互模块：internal/catalog、internal/contextdoc、internal/document、internal/domain、internal/source、internal/splitter、internal/vectorstore。

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mazhan465/Agent-Memory/internal/catalog"
	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
	"github.com/mazhan465/Agent-Memory/internal/document"
	"github.com/mazhan465/Agent-Memory/internal/domain"
	"github.com/mazhan465/Agent-Memory/internal/source"
	"github.com/mazhan465/Agent-Memory/internal/splitter"
	"github.com/mazhan465/Agent-Memory/internal/vectorstore"
)

const (
	memoryRecordLineBufferSize = 1024 * 1024
	memoryLanguage             = "memory"
)

type memoryImportRecord struct {
	ID             string            `json:"id"`
	Content        string            `json:"content"`
	Summary        string            `json:"summary,omitempty"`
	Role           string            `json:"role,omitempty"`
	ConversationID string            `json:"conversation_id,omitempty"`
	MessageID      string            `json:"message_id,omitempty"`
	ToolName       string            `json:"tool_name,omitempty"`
	RelativePath   string            `json:"relative_path,omitempty"`
	Line           int               `json:"line,omitempty"`
	Importance     float64           `json:"importance,omitempty"`
	ExperienceKind string            `json:"experience_kind,omitempty"`
	DomainPath     string            `json:"domain_path,omitempty"`
	Tags           []string          `json:"tags,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	CreatedAt      string            `json:"created_at,omitempty"`
	UpdatedAt      string            `json:"updated_at,omitempty"`
}

func (a *app) runImport(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: code-context import <knowledge|memory> ...")
	}
	switch args[0] {
	case "knowledge", "document", "documents":
		return a.runImportKnowledge(ctx, args[1:])
	case "memory":
		return a.runImportMemory(ctx, args[1:])
	case "conversation", "experience", "preference", "tool_history", "fact":
		return a.runImportMemory(ctx, args)
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown import type %q", args[0])
	}
}

func (a *app) runImportKnowledge(ctx context.Context, args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: code-context import knowledge <path> [source-id]")
	}
	sourceID := ""
	if len(args) == 2 {
		sourceID = args[1]
	}
	scope, documentSource, absolutePath, err := documentKnowledgeSource(args[0], sourceID)
	if err != nil {
		return err
	}
	reader := source.NewMarkdownDocumentSource(absolutePath, scope, documentSource)
	items, err := reader.Read(ctx)
	if err != nil {
		return err
	}
	namespace, err := contextdoc.NamespaceForSource(scope, documentSource)
	if err != nil {
		return err
	}
	documents, err := a.buildKnowledgeDocuments(ctx, namespace, items)
	if err != nil {
		return err
	}
	if err := a.vectorStore.Put(ctx, namespace, documents); err != nil {
		return err
	}
	manifest := a.sourceManifest(sourceManifestOptions{
		Scope:          scope,
		Source:         documentSource,
		Namespace:      namespace,
		DocumentCount:  len(items),
		KnowledgeCount: len(documents),
		Documents:      documents,
	})
	if err := a.catalogStore.UpsertManifest(ctx, manifest, catalog.StatusIndexed); err != nil {
		return err
	}
	fmt.Printf("imported knowledge path=%s source_id=%s namespace=%s documents=%d chunks=%d\n",
		absolutePath, documentSource.ID, namespace, len(items), len(documents))
	return nil
}

func (a *app) runImportMemory(ctx context.Context, args []string) error {
	if len(args) < 2 || len(args) > 3 {
		return errors.New("usage: code-context import memory <type> <json-or-jsonl-path> [source-id]")
	}
	sourceType, err := memorySourceType(args[0])
	if err != nil {
		return err
	}
	sourceID := ""
	if len(args) == 3 {
		sourceID = args[2]
	}
	scope, memorySource, absolutePath, err := memorySourceConfig(args[1], sourceType, sourceID)
	if err != nil {
		return err
	}
	records, err := readMemoryRecords(absolutePath)
	if err != nil {
		return err
	}
	namespace, err := contextdoc.NamespaceForSource(scope, memorySource)
	if err != nil {
		return err
	}
	documents, err := a.buildMemoryDocuments(ctx, namespace, memorySource, absolutePath, records)
	if err != nil {
		return err
	}
	if err := a.vectorStore.Put(ctx, namespace, documents); err != nil {
		return err
	}
	manifest := a.sourceManifest(sourceManifestOptions{
		Scope:       scope,
		Source:      memorySource,
		Namespace:   namespace,
		MemoryCount: len(documents),
		Documents:   documents,
	})
	if err := a.catalogStore.UpsertManifest(ctx, manifest, catalog.StatusIndexed); err != nil {
		return err
	}
	fmt.Printf("imported memory type=%s path=%s source_id=%s namespace=%s records=%d\n",
		memorySource.Type, absolutePath, memorySource.ID, namespace, len(documents))
	return nil
}

func memorySourceType(value string) (contextdoc.SourceType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "conversation", "history", "chat":
		return contextdoc.SourceTypeConversation, nil
	case "experience", "experiences":
		return contextdoc.SourceTypeExperience, nil
	case "preference", "preferences", "user_preference":
		return contextdoc.SourceTypePreference, nil
	case "tool_history", "tool", "tool-history":
		return contextdoc.SourceTypeToolHistory, nil
	case "fact", "facts":
		return contextdoc.SourceTypeFact, nil
	default:
		return "", fmt.Errorf("unsupported memory type %q", value)
	}
}

func memorySourceConfig(
	path string,
	sourceType contextdoc.SourceType,
	sourceID string,
) (contextdoc.Scope, contextdoc.Source, string, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return contextdoc.Scope{}, contextdoc.Source{}, "", err
	}
	absolutePath = filepath.Clean(absolutePath)
	cleanSourceID := strings.TrimSpace(sourceID)
	if cleanSourceID == "" {
		cleanSourceID = defaultDocumentSourceID(absolutePath)
	}
	scope := contextdoc.Scope{Type: contextdoc.ScopeTypeWorkspace, ID: filepath.ToSlash(filepath.Dir(absolutePath))}
	memorySource := contextdoc.Source{
		Type: sourceType,
		ID:   cleanSourceID,
		Metadata: map[string]string{
			"source_path": filepath.ToSlash(absolutePath),
		},
	}
	return scope, memorySource, filepath.ToSlash(absolutePath), nil
}

func readMemoryRecords(path string) ([]memoryImportRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var records []memoryImportRecord
		if err := json.Unmarshal(trimmed, &records); err != nil {
			return nil, err
		}
		return validateMemoryRecords(records)
	}
	return readMemoryJSONL(trimmed)
}

func readMemoryJSONL(data []byte) ([]memoryImportRecord, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), memoryRecordLineBufferSize)
	records := make([]memoryImportRecord, 0)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var record memoryImportRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return validateMemoryRecords(records)
}

func validateMemoryRecords(records []memoryImportRecord) ([]memoryImportRecord, error) {
	for index, record := range records {
		if strings.TrimSpace(record.Content) == "" {
			return nil, fmt.Errorf("memory record %d content is empty", index+1)
		}
	}
	return records, nil
}

type knowledgeChunk struct {
	item  source.DocumentItem
	chunk splitter.Chunk
}

func (a *app) buildKnowledgeDocuments(
	ctx context.Context,
	namespace string,
	items []source.DocumentItem,
) ([]vectorstore.Document, error) {
	chunker := splitter.NewDocumentChunker(
		a.config.MaxChunkLines,
		a.config.ChunkOverlapLines,
		document.LanguageMarkdown,
		nil,
	)
	chunks := make([]knowledgeChunk, 0)
	texts := make([]string, 0)
	for _, item := range items {
		itemChunks := chunker.Chunks(item.RelativePath, item.Nodes)
		for _, chunk := range itemChunks {
			chunks = append(chunks, knowledgeChunk{item: item, chunk: chunk})
			texts = append(texts, chunk.Content)
		}
	}
	if len(chunks) == 0 {
		return nil, nil
	}
	vectors, err := a.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(chunks) {
		return nil, fmt.Errorf("embedding count mismatch: got %d, want %d", len(vectors), len(chunks))
	}
	resolver := domain.NewDefaultDomainResolver()
	documents := make([]vectorstore.Document, 0, len(chunks))
	for index, entry := range chunks {
		document, err := knowledgeVectorDocument(ctx, namespace, entry, vectors[index], resolver)
		if err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	return documents, nil
}

func (a *app) buildMemoryDocuments(
	ctx context.Context,
	namespace string,
	source contextdoc.Source,
	absolutePath string,
	records []memoryImportRecord,
) ([]vectorstore.Document, error) {
	texts := make([]string, 0, len(records))
	for _, record := range records {
		texts = append(texts, record.Content)
	}
	if len(texts) == 0 {
		return nil, nil
	}
	vectors, err := a.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(records) {
		return nil, fmt.Errorf("embedding count mismatch: got %d, want %d", len(vectors), len(records))
	}
	resolver := domain.NewDefaultDomainResolver()
	documents := make([]vectorstore.Document, 0, len(records))
	for index, record := range records {
		document, err := memoryVectorDocument(ctx, namespace, source, absolutePath, record, vectors[index], index, resolver)
		if err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	return documents, nil
}

func knowledgeVectorDocument(
	ctx context.Context,
	namespace string,
	entry knowledgeChunk,
	vector []float32,
	resolver *domain.DomainResolver,
) (vectorstore.Document, error) {
	extension := strings.ToLower(filepath.Ext(entry.item.RelativePath))
	metadata := map[string]string{
		"absolute_path": entry.item.AbsolutePath,
		"scope_type":    string(entry.item.Scope.Type),
		"scope_id":      entry.item.Scope.ID,
		"source_type":   string(entry.item.Source.Type),
		"source_id":     entry.item.Source.ID,
	}
	maps.Copy(metadata, entry.chunk.Metadata)
	for key, value := range entry.item.Source.Metadata {
		if strings.TrimSpace(value) != "" {
			metadata["source_"+key] = value
		}
	}
	decision, err := resolver.Resolve(ctx, domain.Input{
		Query:          entry.chunk.Content,
		FileExtensions: []string{extension},
		Metadata:       metadata,
	})
	if err != nil {
		return vectorstore.Document{}, err
	}
	if decision.Domain != "" {
		metadata[vectorstore.MetadataDomainPath] = string(decision.Domain)
	}
	if decision.ExperienceKind != "" {
		metadata[vectorstore.MetadataExperienceKind] = string(decision.ExperienceKind)
	}
	return vectorstore.Document{
		ID: contextdoc.StableID(
			"knowledge",
			namespace,
			entry.item.RelativePath,
			strconv.Itoa(entry.chunk.StartLine),
			entry.chunk.Content,
		),
		Namespace:     namespace,
		Vector:        vector,
		Content:       entry.chunk.Content,
		RelativePath:  entry.item.RelativePath,
		StartLine:     entry.chunk.StartLine,
		EndLine:       entry.chunk.EndLine,
		FileExtension: extension,
		Language:      entry.chunk.Language,
		Metadata:      metadata,
	}, nil
}

func memoryVectorDocument(
	ctx context.Context,
	namespace string,
	source contextdoc.Source,
	absolutePath string,
	record memoryImportRecord,
	vector []float32,
	index int,
	resolver *domain.DomainResolver,
) (vectorstore.Document, error) {
	metadata := memoryMetadata(source, absolutePath, record)
	decision, err := resolver.Resolve(ctx, domain.Input{Query: record.Content, Metadata: metadata})
	if err != nil {
		return vectorstore.Document{}, err
	}
	if metadata[vectorstore.MetadataDomainPath] == "" && decision.Domain != "" {
		metadata[vectorstore.MetadataDomainPath] = string(decision.Domain)
	}
	if metadata[vectorstore.MetadataExperienceKind] == "" && decision.ExperienceKind != "" {
		metadata[vectorstore.MetadataExperienceKind] = string(decision.ExperienceKind)
	}
	return vectorstore.Document{
		ID:            memoryRecordID(namespace, source, record, index),
		Namespace:     namespace,
		Vector:        vector,
		Content:       record.Content,
		RelativePath:  firstNonEmpty(record.RelativePath, filepath.Base(absolutePath)),
		StartLine:     record.Line,
		EndLine:       record.Line,
		FileExtension: filepath.Ext(absolutePath),
		Language:      memoryLanguage,
		Metadata:      metadata,
	}, nil
}

func memoryMetadata(source contextdoc.Source, absolutePath string, record memoryImportRecord) map[string]string {
	metadata := map[string]string{
		"absolute_path": filepath.ToSlash(absolutePath),
		"source_type":   string(source.Type),
		"source_id":     source.ID,
	}
	maps.Copy(metadata, source.Metadata)
	maps.Copy(metadata, record.Metadata)
	setMetadata(metadata, "record_id", record.ID)
	setMetadata(metadata, "summary", record.Summary)
	setMetadata(metadata, "role", record.Role)
	setMetadata(metadata, "conversation_id", record.ConversationID)
	setMetadata(metadata, "message_id", record.MessageID)
	setMetadata(metadata, "tool_name", record.ToolName)
	setMetadata(metadata, "created_at", record.CreatedAt)
	setMetadata(metadata, "updated_at", record.UpdatedAt)
	if record.Importance > 0 {
		metadata["importance"] = strconv.FormatFloat(record.Importance, 'f', -1, 64)
	}
	if len(record.Tags) > 0 {
		metadata["tags"] = strings.Join(record.Tags, ",")
	}
	setMetadata(metadata, vectorstore.MetadataDomainPath, record.DomainPath)
	setMetadata(metadata, vectorstore.MetadataExperienceKind, defaultExperienceKind(source.Type, record.ExperienceKind))
	return metadata
}

func setMetadata(metadata map[string]string, key string, value string) {
	if strings.TrimSpace(value) != "" {
		metadata[key] = value
	}
}

func defaultExperienceKind(sourceType contextdoc.SourceType, value string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	if sourceType == contextdoc.SourceTypeExperience {
		return string(contextdoc.ExperienceKindGeneral)
	}
	return ""
}

func memoryRecordID(
	namespace string,
	source contextdoc.Source,
	record memoryImportRecord,
	index int,
) string {
	if strings.TrimSpace(record.ID) != "" {
		return contextdoc.StableID("memory", namespace, string(source.Type), source.ID, record.ID)
	}
	return contextdoc.StableID("memory", namespace, string(source.Type), source.ID, strconv.Itoa(index), record.Content)
}

func documentKnowledgeSource(rootPath string, sourceID string) (contextdoc.Scope, contextdoc.Source, string, error) {
	absolutePath, err := filepath.Abs(rootPath)
	if err != nil {
		return contextdoc.Scope{}, contextdoc.Source{}, "", err
	}
	absolutePath = filepath.Clean(absolutePath)
	cleanSourceID := strings.TrimSpace(sourceID)
	if cleanSourceID == "" {
		cleanSourceID = defaultDocumentSourceID(absolutePath)
	}
	scope := contextdoc.Scope{Type: contextdoc.ScopeTypeWorkspace, ID: filepath.ToSlash(absolutePath)}
	documentSource := contextdoc.Source{
		Type: contextdoc.SourceTypeDocument,
		ID:   cleanSourceID,
		Metadata: map[string]string{
			"root_path": filepath.ToSlash(absolutePath),
		},
	}
	return scope, documentSource, filepath.ToSlash(absolutePath), nil
}

func defaultDocumentSourceID(absolutePath string) string {
	base := filepath.Base(absolutePath)
	if extension := filepath.Ext(base); extension != "" {
		base = strings.TrimSuffix(base, extension)
	}
	base = strings.TrimSpace(base)
	if base == "" || base == string(filepath.Separator) || base == "." {
		return contextdoc.StableID("document_source", absolutePath)
	}
	return base
}

type sourceManifestOptions struct {
	Scope          contextdoc.Scope
	Source         contextdoc.Source
	Namespace      string
	DocumentCount  int
	MemoryCount    int
	KnowledgeCount int
	Documents      []vectorstore.Document
}

func (a *app) sourceManifest(options sourceManifestOptions) contextdoc.Manifest {
	embeddingDimension := a.config.EmbeddingDimension
	if len(options.Documents) > 0 {
		embeddingDimension = len(options.Documents[0].Vector)
	}
	return contextdoc.Manifest{
		Scope:              options.Scope,
		Source:             options.Source,
		Version:            time.Now().Unix(),
		Checksum:           checksumVectorDocuments(options.Documents),
		EmbeddingProvider:  a.config.EmbeddingProvider,
		EmbeddingModel:     a.embeddingModelName(),
		EmbeddingDimension: embeddingDimension,
		IndexedAt:          time.Now(),
		DocumentCount:      options.DocumentCount,
		MemoryCount:        options.MemoryCount,
		KnowledgeCount:     options.KnowledgeCount,
		Metadata: map[string]string{
			"namespace": options.Namespace,
		},
	}
}

func (a *app) embeddingModelName() string {
	switch strings.ToLower(a.config.EmbeddingProvider) {
	case "", "hash":
		return fmt.Sprintf("hash-%d", a.config.EmbeddingDimension)
	default:
		return a.config.OpenAIEmbeddingModel
	}
}

func checksumVectorDocuments(documents []vectorstore.Document) string {
	hash := sha256.New()
	for _, document := range documents {
		hash.Write([]byte(document.ID))
		hash.Write([]byte("\x00"))
		hash.Write([]byte(document.RelativePath))
		hash.Write([]byte("\x00"))
		hash.Write([]byte(document.Content))
		hash.Write([]byte("\x00"))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}
