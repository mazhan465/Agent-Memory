// 文件说明：提供 Markdown 文档解析器到通用切块接口的适配层。
// 实现原理：先使用 document.MarkdownParser 将 Markdown 解析为标题、摘要、正文节点，再通过 DocumentChunker 转换为 Chunk。
// 使用方式：文档知识库索引流程可使用 NewMarkdownSplitter 创建适配器，生成带 heading_path 和 node_kind 的 Chunk。
// 注意事项：Markdown 本身只是文档解析器之一，后续 PDF、DOCX 等格式应实现各自 Parser，并复用 DocumentChunker。
// 交互模块：internal/document、internal/indexer、internal/vectorstore。

package splitter

import (
	"strings"

	"github.com/mazhan465/Agent-Memory/internal/document"
)

const (
	markdownLanguage = document.LanguageMarkdown

	// MetadataDocumentID 表示文档 ID。
	MetadataDocumentID = document.MetadataDocumentID
	// MetadataSectionID 表示文档章节 ID。
	MetadataSectionID = document.MetadataSectionID
	// MetadataHeadingPath 表示文档标题路径。
	MetadataHeadingPath = document.MetadataHeadingPath
	// MetadataKnowledgeKind 表示文档知识类型。
	MetadataKnowledgeKind = document.MetadataKnowledgeKind
	// MetadataNodeKind 表示文档解析节点类型。
	MetadataNodeKind = document.MetadataNodeKind
)

// MarkdownSplitter 将 Markdown 解析节点适配为通用 Chunk。
type MarkdownSplitter struct {
	parser  document.Parser
	chunker *DocumentChunker
}

// NewMarkdownSplitter 创建 Markdown 文档切块适配器。
func NewMarkdownSplitter(maxLines int, overlapLines int) *MarkdownSplitter {
	return &MarkdownSplitter{
		parser:  document.NewMarkdownParser(),
		chunker: NewDocumentChunker(maxLines, overlapLines, markdownLanguage, extendMarkdownBoundary),
	}
}

// Split 将 Markdown 解析节点转换成带元数据的内容片段。
func (s *MarkdownSplitter) Split(filePath string, content []byte, language string) []Chunk {
	nodes, err := s.parser.Parse(filePath, content)
	if err != nil {
		return nil
	}
	if language != "" && language != s.chunker.Language {
		chunker := *s.chunker
		chunker.Language = language
		return chunker.Chunks(filePath, nodes)
	}
	return s.chunker.Chunks(filePath, nodes)
}

func extendMarkdownBoundary(lines []string, start int, end int, limit int) int {
	if end >= limit {
		return limit
	}
	for hasUnclosedFence(lines[start:end]) && end < limit {
		end++
	}
	for end < limit && markdownTableLine(lines[end-1]) && markdownTableLine(lines[end]) {
		end++
	}
	return end
}

func hasUnclosedFence(lines []string) bool {
	inFence := false
	for _, line := range lines {
		if fenceLine(line) {
			inFence = !inFence
		}
	}
	return inFence
}

func fenceLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

func markdownTableLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|")
}
