// 文件说明：测试 Markdown 文档解析器。
// 实现原理：构造包含标题、摘要和代码块的 Markdown 内容，验证解析器输出统一文档节点。
// 使用方式：执行 go test ./internal/document 或 go test ./...。
// 注意事项：测试只覆盖解析逻辑，不涉及向量存储。
// 交互模块：internal/document。

package document

import (
	"testing"

	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
)

func TestMarkdownParserParsesTitleSummaryAndBody(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte(`# Guide

> This is a short summary.

## API Usage

Use the client API.
`)

	nodes, err := parser.Parse("/docs/go_guide.md", content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(nodes) != 5 {
		t.Fatalf("Parse() nodes len = %d, want 5", len(nodes))
	}
	if nodes[0].Kind != NodeKindTitle || nodes[0].Content != "Guide" {
		t.Fatalf("first node = %+v, want Guide title", nodes[0])
	}
	if nodes[1].Kind != NodeKindSummary || nodes[1].Content != "This is a short summary." {
		t.Fatalf("second node = %+v, want summary", nodes[1])
	}
	body := nodes[4]
	if body.Kind != NodeKindBody {
		t.Fatalf("last node kind = %s, want body", body.Kind)
	}
	if body.DocumentID != "go-guide" {
		t.Fatalf("DocumentID = %s, want go-guide", body.DocumentID)
	}
	if body.SectionID != "guide/api-usage" {
		t.Fatalf("SectionID = %s, want guide/api-usage", body.SectionID)
	}
	if body.HeadingPathText() != "Guide > API Usage" {
		t.Fatalf("HeadingPathText() = %s, want Guide > API Usage", body.HeadingPathText())
	}
	if body.KnowledgeKind != contextdoc.KnowledgeKindAPI {
		t.Fatalf("KnowledgeKind = %s, want api", body.KnowledgeKind)
	}
}

func TestMarkdownParserIgnoresHeadingInsideFence(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte("# Real\n\n```md\n# Not Heading\n```\n\n## Child\nText.\n")

	nodes, err := parser.Parse("/docs/fence.md", content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	bodyNodes := filterNodes(nodes, NodeKindBody)
	if len(bodyNodes) != 2 {
		t.Fatalf("body nodes len = %d, want 2", len(bodyNodes))
	}
	if bodyNodes[1].HeadingPathText() != "Real > Child" {
		t.Fatalf("HeadingPathText() = %s, want Real > Child", bodyNodes[1].HeadingPathText())
	}
}

func TestNodeStorageMetadata(t *testing.T) {
	node := Node{
		Kind:          NodeKindBody,
		DocumentID:    "guide",
		SectionID:     "guide/api",
		HeadingPath:   []string{"Guide", "API"},
		KnowledgeKind: contextdoc.KnowledgeKindAPI,
		Metadata:      map[string]string{"version": "v1"},
	}

	metadata := node.StorageMetadata()
	if metadata[MetadataDocumentID] != "guide" {
		t.Fatalf("document_id = %s, want guide", metadata[MetadataDocumentID])
	}
	if metadata[MetadataHeadingPath] != "Guide > API" {
		t.Fatalf("heading_path = %s, want Guide > API", metadata[MetadataHeadingPath])
	}
	if metadata[MetadataNodeKind] != string(NodeKindBody) {
		t.Fatalf("node_kind = %s, want body", metadata[MetadataNodeKind])
	}
	if metadata["version"] != "v1" {
		t.Fatalf("version = %s, want v1", metadata["version"])
	}
}

func filterNodes(nodes []Node, kind NodeKind) []Node {
	result := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Kind == kind {
			result = append(result, node)
		}
	}
	return result
}
