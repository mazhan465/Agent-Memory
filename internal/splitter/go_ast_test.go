// 文件说明：测试 Go AST 感知切块器。
// 实现原理：构造 Go 源码，验证切块器能按顶层声明切分并在语法错误时回退。
// 使用方式：执行 go test ./internal/splitter 或 go test ./...。
// 注意事项：测试不依赖真实代码库。
// 交互模块：internal/splitter。

package splitter

import "testing"

func TestGoASTSplitterSplitsTopLevelDeclarations(t *testing.T) {
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
	splitter := NewGoASTSplitter(20, 0, nil)
	chunks := splitter.Split("service.go", content, "go")
	if len(chunks) != 5 {
		t.Fatalf("len(chunks) = %d, want 5: %+v", len(chunks), chunks)
	}
	if chunks[0].StartLine != 1 || chunks[0].EndLine != 2 {
		t.Fatalf("package chunk lines = %d-%d, want 1-2", chunks[0].StartLine, chunks[0].EndLine)
	}
	if chunks[1].StartLine != 3 || chunks[1].EndLine != 3 {
		t.Fatalf("import chunk lines = %d-%d, want 3-3", chunks[1].StartLine, chunks[1].EndLine)
	}
	if chunks[2].StartLine != 5 || chunks[2].EndLine != 5 {
		t.Fatalf("type chunk lines = %d-%d, want 5-5", chunks[2].StartLine, chunks[2].EndLine)
	}
	if chunks[3].StartLine != 7 || chunks[3].EndLine != 9 {
		t.Fatalf("first function chunk lines = %d-%d, want 7-9", chunks[3].StartLine, chunks[3].EndLine)
	}
	if chunks[4].StartLine != 11 || chunks[4].EndLine != 13 {
		t.Fatalf("second function chunk lines = %d-%d, want 11-13", chunks[4].StartLine, chunks[4].EndLine)
	}
}

func TestGoASTSplitterFallsBackOnParseError(t *testing.T) {
	content := []byte("package sample\n\nfunc Broken(\n")
	splitter := NewGoASTSplitter(20, 0, nil)
	chunks := splitter.Split("broken.go", content, "go")
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want fallback single chunk", len(chunks))
	}
	if chunks[0].StartLine != 1 || chunks[0].EndLine != 4 {
		t.Fatalf("fallback chunk lines = %d-%d, want 1-4", chunks[0].StartLine, chunks[0].EndLine)
	}
}
