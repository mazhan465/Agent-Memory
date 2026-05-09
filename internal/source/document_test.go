// 文件说明：测试 Markdown 文档源读取能力。
// 实现原理：在临时目录写入 Markdown 文件，验证 DocumentSource 的扫描、解析和排序行为。
// 使用方式：执行 go test ./internal/source 或 go test ./...。
// 注意事项：测试只读写临时目录，不依赖外部服务。
// 交互模块：internal/source、internal/document、internal/contextdoc。

package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
	"github.com/mazhan465/Agent-Memory/internal/document"
)

func TestMarkdownDocumentSourceReadDirectory(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "b.md"), "# B\n\nBody B.\n")
	writeTestFile(t, filepath.Join(root, "a.markdown"), "# A\n\n> Summary A.\n\nBody A.\n")
	writeTestFile(t, filepath.Join(root, "ignore.txt"), "# Ignore\n")

	source := NewMarkdownDocumentSource(root, testScope(), testSource())
	items, err := source.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("Read() len = %d, want 2", len(items))
	}
	if items[0].RelativePath != "a.markdown" || items[1].RelativePath != "b.md" {
		t.Fatalf("relative paths = %s, %s; want sorted markdown files", items[0].RelativePath, items[1].RelativePath)
	}
	if items[0].Scope.ID != "workspace-1" || items[0].Source.ID != "docs" {
		t.Fatalf("item source metadata = %+v", items[0])
	}
	if !hasNodeKind(items[0].Nodes, document.NodeKindSummary) {
		t.Fatalf("first item nodes = %+v, want summary node", items[0].Nodes)
	}
}

func TestMarkdownDocumentSourceReadSingleFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "guide.md")
	writeTestFile(t, path, "# Guide\n\nBody.\n")

	source := NewMarkdownDocumentSource(path, testScope(), testSource())
	items, err := source.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Read() len = %d, want 1", len(items))
	}
	if items[0].RelativePath != "guide.md" {
		t.Fatalf("RelativePath = %s, want guide.md", items[0].RelativePath)
	}
}

func TestMarkdownDocumentSourceRejectsInvalidSourceType(t *testing.T) {
	source := NewMarkdownDocumentSource(t.TempDir(), testScope(), contextdoc.Source{Type: contextdoc.SourceTypeCodebase, ID: "repo"})
	_, err := source.Read(context.Background())
	if err == nil {
		t.Fatal("Read() error = nil, want invalid source type error")
	}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func testScope() contextdoc.Scope {
	return contextdoc.Scope{Type: contextdoc.ScopeTypeWorkspace, ID: "workspace-1"}
}

func testSource() contextdoc.Source {
	return contextdoc.Source{Type: contextdoc.SourceTypeDocument, ID: "docs"}
}

func hasNodeKind(nodes []document.Node, kind document.NodeKind) bool {
	for _, node := range nodes {
		if node.Kind == kind {
			return true
		}
	}
	return false
}
