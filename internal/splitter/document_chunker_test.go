// 文件说明：测试通用文档节点切块器。
// 实现原理：构造 title、summary、body 节点，验证 DocumentChunker 的节点转换、正文切块和边界扩展行为。
// 使用方式：执行 go test ./internal/splitter 或 go test ./...。
// 注意事项：测试不依赖具体文档格式解析器。
// 交互模块：internal/splitter、internal/document。

package splitter

import (
	"testing"

	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
	"github.com/mazhan465/Agent-Memory/internal/document"
)

func TestDocumentChunkerKeepsTitleAndSummary(t *testing.T) {
	chunker := NewDocumentChunker(3, 0, "markdown", nil)
	nodes := []document.Node{
		{
			Kind:          document.NodeKindTitle,
			Content:       "Guide",
			DocumentID:    "guide",
			SectionID:     "guide",
			HeadingPath:   []string{"Guide"},
			KnowledgeKind: contextdoc.KnowledgeKindConcept,
			StartLine:     1,
			EndLine:       1,
		},
		{
			Kind:          document.NodeKindSummary,
			Content:       "Summary text.",
			DocumentID:    "guide",
			SectionID:     "guide",
			HeadingPath:   []string{"Guide"},
			KnowledgeKind: contextdoc.KnowledgeKindConcept,
			StartLine:     3,
			EndLine:       3,
		},
	}

	chunks := chunker.Chunks("/docs/guide.md", nodes)
	if len(chunks) != 2 {
		t.Fatalf("Chunks() len = %d, want 2", len(chunks))
	}
	if chunks[0].Metadata[MetadataNodeKind] != "title" {
		t.Fatalf("node_kind = %s, want title", chunks[0].Metadata[MetadataNodeKind])
	}
	if chunks[1].Content != "Summary text." {
		t.Fatalf("summary content = %q, want Summary text.", chunks[1].Content)
	}
}

func TestDocumentChunkerSplitsBodyNodes(t *testing.T) {
	chunker := NewDocumentChunker(2, 0, "markdown", nil)
	nodes := []document.Node{{
		Kind:          document.NodeKindBody,
		Content:       "line1\nline2\nline3",
		DocumentID:    "guide",
		SectionID:     "guide/body",
		HeadingPath:   []string{"Guide", "Body"},
		KnowledgeKind: contextdoc.KnowledgeKindConcept,
		StartLine:     10,
		EndLine:       12,
	}}

	chunks := chunker.Chunks("/docs/guide.md", nodes)
	if len(chunks) != 2 {
		t.Fatalf("Chunks() len = %d, want 2", len(chunks))
	}
	if chunks[0].StartLine != 10 || chunks[0].EndLine != 11 {
		t.Fatalf("first chunk lines = %d:%d, want 10:11", chunks[0].StartLine, chunks[0].EndLine)
	}
	if chunks[1].StartLine != 12 || chunks[1].EndLine != 12 {
		t.Fatalf("second chunk lines = %d:%d, want 12:12", chunks[1].StartLine, chunks[1].EndLine)
	}
}

func TestDocumentChunkerUsesBoundaryExtender(t *testing.T) {
	chunker := NewDocumentChunker(2, 0, "markdown", func(lines []string, start int, end int, limit int) int {
		return limit
	})
	nodes := []document.Node{{
		Kind:          document.NodeKindBody,
		Content:       "line1\nline2\nline3",
		DocumentID:    "guide",
		SectionID:     "guide",
		HeadingPath:   []string{"Guide"},
		KnowledgeKind: contextdoc.KnowledgeKindConcept,
		StartLine:     1,
		EndLine:       3,
	}}

	chunks := chunker.Chunks("/docs/guide.md", nodes)
	if len(chunks) != 1 {
		t.Fatalf("Chunks() len = %d, want 1", len(chunks))
	}
	if chunks[0].EndLine != 3 {
		t.Fatalf("chunk end line = %d, want 3", chunks[0].EndLine)
	}
}
