// 文件说明：实现 Go 代码 AST 感知切块器。
// 实现原理：使用 go/parser 解析 Go 文件，按顶层声明的起止行切分；解析失败或非 Go 文件时回退到通用 Splitter。
// 使用方式：CLI 索引流程优先使用 GoASTSplitter，其他语言仍由 fallback 处理。
// 注意事项：当前只按 Go 顶层声明切分，后续可继续细化到方法、结构体成员和 tree-sitter 多语言切块。
// 交互模块：cmd/code-context、internal/indexer、internal/splitter。

package splitter

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

const goASTChunkKind = "go_ast_decl"

// GoASTSplitter 按 Go AST 顶层声明切分代码。
type GoASTSplitter struct {
	MaxLines     int
	OverlapLines int
	Fallback     Splitter
}

// NewGoASTSplitter 创建 Go AST 切块器。
func NewGoASTSplitter(maxLines int, overlapLines int, fallback Splitter) *GoASTSplitter {
	lineSplitter := NewLineSplitter(maxLines, overlapLines)
	if fallback == nil {
		fallback = lineSplitter
	}
	return &GoASTSplitter{
		MaxLines:     lineSplitter.MaxLines,
		OverlapLines: lineSplitter.OverlapLines,
		Fallback:     fallback,
	}
}

// Split 将 Go 文件按 AST 顶层声明切块，其他语言使用 fallback。
func (s *GoASTSplitter) Split(filePath string, content []byte, language string) []Chunk {
	if language != "go" {
		return s.Fallback.Split(filePath, content, language)
	}
	text := normalizeCodeText(content)
	if strings.TrimSpace(text) == "" {
		return nil
	}

	fileSet := token.NewFileSet()
	parsedFile, err := parser.ParseFile(fileSet, filePath, text, parser.ParseComments)
	if err != nil || parsedFile == nil || len(parsedFile.Decls) == 0 {
		return s.Fallback.Split(filePath, content, language)
	}

	lines := strings.Split(text, "\n")
	chunks := make([]Chunk, 0, len(parsedFile.Decls)+1)
	firstDeclLine := fileSet.Position(parsedFile.Decls[0].Pos()).Line
	chunks = append(chunks, s.packageHeaderChunk(filePath, lines, firstDeclLine)...)
	for _, declaration := range parsedFile.Decls {
		chunks = append(chunks, s.declarationChunks(filePath, lines, fileSet, declaration)...)
	}
	if len(chunks) == 0 {
		return s.Fallback.Split(filePath, content, language)
	}
	return chunks
}

func (s *GoASTSplitter) packageHeaderChunk(filePath string, lines []string, firstDeclLine int) []Chunk {
	if firstDeclLine <= 1 {
		return nil
	}
	return s.lineRangeChunks(filePath, lines, 1, firstDeclLine-1, "package_header")
}

func (s *GoASTSplitter) declarationChunks(filePath string, lines []string, fileSet *token.FileSet, declaration ast.Decl) []Chunk {
	startLine := fileSet.Position(declaration.Pos()).Line
	endLine := fileSet.Position(declaration.End()).Line
	return s.lineRangeChunks(filePath, lines, startLine, endLine, goASTChunkKind)
}

func (s *GoASTSplitter) lineRangeChunks(
	filePath string,
	lines []string,
	startLine int,
	endLine int,
	chunkKind string,
) []Chunk {
	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	if startLine > endLine {
		return nil
	}
	step := s.MaxLines - s.OverlapLines
	if step <= 0 {
		step = s.MaxLines
	}
	chunks := make([]Chunk, 0, (endLine-startLine+1)/step+1)
	for start := startLine; start <= endLine; start += step {
		end := min(start+s.MaxLines-1, endLine)
		content := strings.Join(lines[start-1:end], "\n")
		if strings.TrimSpace(content) != "" {
			chunks = append(chunks, Chunk{
				Content:   content,
				FilePath:  filePath,
				StartLine: start,
				EndLine:   end,
				Language:  "go",
				Metadata: map[string]string{
					"chunk_kind": chunkKind,
				},
			})
		}
		if end == endLine {
			break
		}
	}
	return chunks
}

func normalizeCodeText(content []byte) string {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}
