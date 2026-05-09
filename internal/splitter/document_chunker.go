// 文件说明：提供文档解析节点到通用 Chunk 的切块器。
// 实现原理：将 document.Parser 输出的 title、summary、body 节点统一转换为 Chunk，body 节点按行数和重叠行切分。
// 使用方式：MarkdownSplitter 或后续 DocumentSource 可复用 DocumentChunker，把不同格式 Parser 的节点转成可向量化片段。
// 注意事项：格式相关边界保护通过 BoundaryExtender 注入，例如 Markdown 的代码块和表格边界。
// 交互模块：internal/document、internal/indexer。

package splitter

import (
	"strings"

	"github.com/mazhan465/Agent-Memory/internal/document"
)

// BoundaryExtender 在切块边界落入格式敏感区域时扩展结束位置。
type BoundaryExtender func(lines []string, start int, end int, limit int) int

// DocumentChunker 将文档解析节点转换为通用 Chunk。
type DocumentChunker struct {
	MaxLines         int
	OverlapLines     int
	Language         string
	BoundaryExtender BoundaryExtender
}

// NewDocumentChunker 创建文档节点切块器。
func NewDocumentChunker(maxLines int, overlapLines int, language string, extender BoundaryExtender) *DocumentChunker {
	if maxLines <= 0 {
		maxLines = 120
	}
	if overlapLines < 0 {
		overlapLines = 0
	}
	if overlapLines >= maxLines {
		overlapLines = maxLines / 4
	}
	if language == "" {
		language = defaultLanguage
	}
	return &DocumentChunker{
		MaxLines:         maxLines,
		OverlapLines:     overlapLines,
		Language:         language,
		BoundaryExtender: extender,
	}
}

// Chunks 将文档节点转换为可向量化片段。
func (c *DocumentChunker) Chunks(filePath string, nodes []document.Node) []Chunk {
	chunks := make([]Chunk, 0, len(nodes))
	for _, node := range nodes {
		if strings.TrimSpace(node.Content) == "" {
			continue
		}
		if node.Kind == document.NodeKindBody {
			chunks = append(chunks, c.splitBodyNode(filePath, node)...)
			continue
		}
		chunks = append(chunks, c.newDocumentNodeChunk(filePath, node.Content, node.StartLine, node.EndLine, node))
	}
	return chunks
}

func (c *DocumentChunker) splitBodyNode(filePath string, node document.Node) []Chunk {
	lines := strings.Split(node.Content, "\n")
	step := c.MaxLines - c.OverlapLines
	if step <= 0 {
		step = c.MaxLines
	}

	chunks := make([]Chunk, 0, len(lines)/step+1)
	for start := 0; start < len(lines); {
		end := min(start+c.MaxLines, len(lines))
		if c.BoundaryExtender != nil {
			end = c.BoundaryExtender(lines, start, end, len(lines))
		}
		chunkText := strings.Join(lines[start:end], "\n")
		if strings.TrimSpace(chunkText) != "" {
			chunks = append(chunks, c.newDocumentNodeChunk(
				filePath,
				chunkText,
				node.StartLine+start,
				node.StartLine+end-1,
				node,
			))
		}
		if end >= len(lines) {
			break
		}
		nextStart := end - c.OverlapLines
		if nextStart <= start {
			nextStart = end
		}
		start = nextStart
	}
	return chunks
}

func (c *DocumentChunker) newDocumentNodeChunk(
	filePath string,
	content string,
	startLine int,
	endLine int,
	node document.Node,
) Chunk {
	return Chunk{
		Content:   content,
		FilePath:  filePath,
		StartLine: startLine,
		EndLine:   endLine,
		Language:  c.Language,
		Metadata:  node.StorageMetadata(),
	}
}
