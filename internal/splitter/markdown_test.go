// 文件说明：测试 Markdown 文档解析适配层。
// 实现原理：构造包含标题、表格和代码块的 Markdown 内容，验证解析节点被转换为带元数据的 Chunk。
// 使用方式：执行 go test ./internal/splitter 或 go test ./...。
// 注意事项：测试不依赖外部文件系统和网络。
// 交互模块：internal/splitter、internal/document。

package splitter

import "testing"

func TestMarkdownSplitterAddsHeadingMetadata(t *testing.T) {
	content := []byte(`# Guide

Intro text.

## API Usage

Use the client API.
`)
	splitter := NewMarkdownSplitter(20, 0)

	chunks := splitter.Split("/docs/go_guide.md", content, "")
	secondBody := findChunkByHeadingAndKind(t, chunks, "Guide > API Usage", "body")
	if secondBody.Language != markdownLanguage {
		t.Fatalf("Language = %s, want %s", secondBody.Language, markdownLanguage)
	}
	if secondBody.Metadata[MetadataDocumentID] != "go-guide" {
		t.Fatalf("document_id = %s, want go-guide", secondBody.Metadata[MetadataDocumentID])
	}
	if secondBody.Metadata[MetadataSectionID] != "guide/api-usage" {
		t.Fatalf("section_id = %s, want guide/api-usage", secondBody.Metadata[MetadataSectionID])
	}
	if secondBody.Metadata[MetadataKnowledgeKind] != "api" {
		t.Fatalf("knowledge_kind = %s, want api", secondBody.Metadata[MetadataKnowledgeKind])
	}
}

func TestMarkdownSplitterStoresTitleAndSummaryNodes(t *testing.T) {
	content := []byte("# Guide\n\n> Summary text.\n\nBody text.\n")
	splitter := NewMarkdownSplitter(20, 0)

	chunks := splitter.Split("/docs/guide.md", content, "markdown")
	_ = findChunkByHeadingAndKind(t, chunks, "Guide", "title")
	summary := findChunkByHeadingAndKind(t, chunks, "Guide", "summary")
	if summary.Content != "Summary text." {
		t.Fatalf("summary content = %q, want Summary text.", summary.Content)
	}
}

func TestMarkdownSplitterDoesNotSplitFence(t *testing.T) {
	content := []byte("# Example\n\n```go\nfunc main() {\n\tprintln(\"hello\")\n}\n```\n\nAfter fence.\n")
	splitter := NewMarkdownSplitter(4, 0)

	chunks := splitter.Split("/docs/example.md", content, "markdown")
	firstBody := findChunkByHeadingAndKind(t, chunks, "Example", "body")
	if firstBody.StartLine != 1 || firstBody.EndLine != 7 {
		t.Fatalf("first body lines = %d:%d, want 1:7", firstBody.StartLine, firstBody.EndLine)
	}
	if firstBody.Metadata[MetadataKnowledgeKind] != "example" {
		t.Fatalf("knowledge_kind = %s, want example", firstBody.Metadata[MetadataKnowledgeKind])
	}
}

func TestMarkdownSplitterKeepsTableTogether(t *testing.T) {
	content := []byte("# Options\n\n| Name | Value |\n| --- | --- |\n| timeout | 3s |\n| retry | 2 |\n\nText.\n")
	splitter := NewMarkdownSplitter(4, 0)

	chunks := splitter.Split("/docs/options.md", content, "markdown")
	firstBody := findChunkByHeadingAndKind(t, chunks, "Options", "body")
	if firstBody.EndLine != 6 {
		t.Fatalf("first body end line = %d, want 6", firstBody.EndLine)
	}
}

func TestMarkdownSplitterIgnoresHeadingInsideFence(t *testing.T) {
	content := []byte("# Real\n\n```md\n# Not Heading\n```\n\n## Child\nText.\n")
	splitter := NewMarkdownSplitter(20, 0)

	chunks := splitter.Split("/docs/fence.md", content, "markdown")
	_ = findChunkByHeadingAndKind(t, chunks, "Real > Child", "body")
}

func findChunkByHeadingAndKind(t *testing.T, chunks []Chunk, headingPath string, nodeKind string) Chunk {
	t.Helper()
	for _, chunk := range chunks {
		if chunk.Metadata[MetadataHeadingPath] == headingPath && chunk.Metadata[MetadataNodeKind] == nodeKind {
			return chunk
		}
	}
	t.Fatalf("chunk with heading_path=%q node_kind=%q not found in %+v", headingPath, nodeKind, chunks)
	return Chunk{}
}
