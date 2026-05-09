// 文件说明：定义文档知识库的解析器接口、节点模型和通用元数据键。
// 实现原理：不同格式文档通过 Parser 解析为统一 Node，Node 标记标题、摘要或正文，并携带章节路径和知识类型。
// 使用方式：DocumentSource 或格式适配器调用 Parser.Parse，将 Node 转换为 ContextDocument 或向量存储文档。
// 注意事项：本包只负责解析结构，不负责 embedding、向量存储或 source 持久化。
// 交互模块：internal/splitter、internal/contextdoc、internal/indexer。

// Package document 提供多格式文档解析抽象。
package document

import (
	"maps"

	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
)

const (
	// MetadataDocumentID 表示文档 ID。
	MetadataDocumentID = "document_id"
	// MetadataSectionID 表示文档章节 ID。
	MetadataSectionID = "section_id"
	// MetadataHeadingPath 表示文档标题路径。
	MetadataHeadingPath = "heading_path"
	// MetadataKnowledgeKind 表示文档知识类型。
	MetadataKnowledgeKind = "knowledge_kind"
	// MetadataNodeKind 表示解析节点类型。
	MetadataNodeKind = "node_kind"
)

// LanguageMarkdown 表示 Markdown 文档格式。
const LanguageMarkdown = "markdown"

// NodeKind 表示解析后的文档结构节点类型。
type NodeKind string

const (
	// NodeKindTitle 表示标题节点。
	NodeKindTitle NodeKind = "title"
	// NodeKindSummary 表示摘要节点。
	NodeKindSummary NodeKind = "summary"
	// NodeKindBody 表示正文节点。
	NodeKindBody NodeKind = "body"
)

// Parser 定义文档格式解析器接口。
type Parser interface {
	Parse(filePath string, content []byte) ([]Node, error)
}

// Node 表示文档解析后的结构化节点。
type Node struct {
	Kind          NodeKind
	Content       string
	DocumentID    string
	SectionID     string
	HeadingPath   []string
	KnowledgeKind contextdoc.KnowledgeKind
	StartLine     int
	EndLine       int
	Metadata      map[string]string
}

// HeadingPathText 返回适合写入存储元数据的标题路径文本。
func (n Node) HeadingPathText() string {
	return joinHeadingPath(n.HeadingPath)
}

// StorageMetadata 返回用于向量存储或 ContextDocument 的通用元数据。
func (n Node) StorageMetadata() map[string]string {
	metadata := map[string]string{
		MetadataDocumentID:    n.DocumentID,
		MetadataSectionID:     n.SectionID,
		MetadataHeadingPath:   n.HeadingPathText(),
		MetadataKnowledgeKind: string(n.KnowledgeKind),
		MetadataNodeKind:      string(n.Kind),
	}
	maps.Copy(metadata, n.Metadata)
	return metadata
}
