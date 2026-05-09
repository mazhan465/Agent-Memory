//go:build cgo

// 文件说明：测试统一 tree-sitter 语法切块器。
// 实现原理：构造 Go 和 C++ 源码，验证 tree-sitter 能按语法节点切分并保留符号元数据。
// 使用方式：执行 go test ./internal/splitter 或 go test ./...。
// 注意事项：测试不依赖真实代码库。
// 交互模块：internal/splitter。

package splitter

import "testing"

func TestTreeSitterSplitterSplitsGoDeclarations(t *testing.T) {
	content := []byte(`package sample

import "fmt"

type Service struct{}

func NewService() Service {
	return Service{}
}

func (s Service) Run() {
	fmt.Println("run")
}
`)
	splitter := NewTreeSitterSplitter(20, 0, nil)
	chunks := splitter.Split("service.go", content, "go")
	if len(chunks) != 5 {
		t.Fatalf("len(chunks) = %d, want 5: %+v", len(chunks), chunks)
	}
	assertChunk(t, chunks[0], 1, 1, "package", "sample")
	assertChunk(t, chunks[1], 3, 3, "import", "")
	assertChunk(t, chunks[2], 5, 5, "type", "Service")
	assertChunk(t, chunks[3], 7, 9, "function", "NewService")
	assertChunk(t, chunks[4], 11, 13, "method", "Run")
}

func TestTreeSitterSplitterSplitsCppDeclarations(t *testing.T) {
	content := []byte(`#include <string>

namespace demo {
class Service {
public:
  std::string Name() const {
    return "demo";
  }
};

int Add(int left, int right) {
  return left + right;
}
}
`)
	splitter := NewTreeSitterSplitter(20, 0, nil)
	chunks := splitter.Split("service.cpp", content, "cpp")
	if len(chunks) != 2 {
		t.Fatalf("len(chunks) = %d, want 2: %+v", len(chunks), chunks)
	}
	assertChunk(t, chunks[0], 1, 1, "include", "")
	assertChunk(t, chunks[1], 3, 14, "namespace", "demo")
}

func TestTreeSitterSplitterFallsBackOnParseError(t *testing.T) {
	content := []byte("package sample\n\nfunc Broken(\n")
	splitter := NewTreeSitterSplitter(20, 0, nil)
	chunks := splitter.Split("broken.go", content, "go")
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want fallback single chunk", len(chunks))
	}
	if chunks[0].StartLine != 1 || chunks[0].EndLine != 4 {
		t.Fatalf("fallback chunk lines = %d-%d, want 1-4", chunks[0].StartLine, chunks[0].EndLine)
	}
	if chunks[0].Metadata != nil {
		t.Fatalf("fallback metadata = %+v, want nil", chunks[0].Metadata)
	}
}

func assertChunk(t *testing.T, chunk Chunk, startLine int, endLine int, symbolKind string, symbolName string) {
	t.Helper()
	if chunk.StartLine != startLine || chunk.EndLine != endLine {
		t.Fatalf("chunk lines = %d-%d, want %d-%d", chunk.StartLine, chunk.EndLine, startLine, endLine)
	}
	if chunk.Metadata[MetadataParser] != treeSitterParserName {
		t.Fatalf("parser metadata = %s, want %s", chunk.Metadata[MetadataParser], treeSitterParserName)
	}
	if chunk.Metadata[MetadataSymbolKind] != symbolKind {
		t.Fatalf("symbol kind = %s, want %s", chunk.Metadata[MetadataSymbolKind], symbolKind)
	}
	if chunk.Metadata[MetadataSymbolName] != symbolName {
		t.Fatalf("symbol name = %s, want %s", chunk.Metadata[MetadataSymbolName], symbolName)
	}
}
