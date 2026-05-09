//go:build cgo

// 文件说明：实现基于 tree-sitter 的统一多语言语法切块器。
// 实现原理：按语言配置加载 tree-sitter grammar，遍历语法树收集目标节点行区间，再通过 RangeChunker 生成 chunk。
// 使用方式：CLI 索引流程使用 NewTreeSitterSplitter 创建统一语法切块器，Go/C++ 优先走 tree-sitter，失败回退 LineSplitter。
// 注意事项：tree-sitter 解析失败或语法树包含错误时回退，避免错误 AST 影响索引可用性。
// 交互模块：cmd/code-context、internal/indexer、internal/splitter。

package splitter

import (
	"errors"
	"strings"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	treesittercpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	treesittergo "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

const treeSitterParserName = "tree-sitter"

// TreeSitterSplitter 使用 tree-sitter 执行多语言语法切块。
type TreeSitterSplitter struct {
	configs      map[string]treeSitterLanguageConfig
	rangeChunker *RangeChunker
	fallback     Splitter
}

type treeSitterLanguageConfig struct {
	language   *treesitter.Language
	languageID string
	nodeKinds  map[string]string
}

// NewTreeSitterSplitter 创建统一 tree-sitter 切块器。
func NewTreeSitterSplitter(maxLines int, overlapLines int, fallback Splitter) *TreeSitterSplitter {
	if fallback == nil {
		fallback = NewLineSplitter(maxLines, overlapLines)
	}
	configs := map[string]treeSitterLanguageConfig{
		"go":  newGoTreeSitterConfig(),
		"cpp": newCppTreeSitterConfig(),
	}
	return &TreeSitterSplitter{
		configs:      configs,
		rangeChunker: NewRangeChunker(maxLines, overlapLines),
		fallback:     fallback,
	}
}

// Split 将源码按 tree-sitter 语法节点切块，失败时回退通用切块器。
func (s *TreeSitterSplitter) Split(filePath string, content []byte, language string) []Chunk {
	config, ok := s.configs[language]
	if !ok {
		return s.fallback.Split(filePath, content, language)
	}
	text := normalizeCodeText(content)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	ranges, err := parseTreeSitterRanges(content, config)
	if err != nil || len(ranges) == 0 {
		return s.fallback.Split(filePath, content, language)
	}
	lines := strings.Split(text, "\n")
	chunks := s.rangeChunker.Chunks(filePath, language, lines, ranges)
	if len(chunks) == 0 {
		return s.fallback.Split(filePath, content, language)
	}
	return chunks
}

func newGoTreeSitterConfig() treeSitterLanguageConfig {
	return treeSitterLanguageConfig{
		language:   treesitter.NewLanguage(treesittergo.Language()),
		languageID: "go",
		nodeKinds: map[string]string{
			"package_clause":       "package",
			"import_declaration":   "import",
			"const_declaration":    "const",
			"var_declaration":      "var",
			"type_declaration":     "type",
			"function_declaration": "function",
			"method_declaration":   "method",
		},
	}
}

func newCppTreeSitterConfig() treeSitterLanguageConfig {
	return treeSitterLanguageConfig{
		language:   treesitter.NewLanguage(treesittercpp.Language()),
		languageID: "cpp",
		nodeKinds: map[string]string{
			"preproc_include":      "include",
			"preproc_def":          "macro",
			"preproc_function_def": "macro",
			"namespace_definition": "namespace",
			"class_specifier":      "class",
			"struct_specifier":     "struct",
			"union_specifier":      "union",
			"template_declaration": "template",
			"function_definition":  "function",
		},
	}
}

func parseTreeSitterRanges(content []byte, config treeSitterLanguageConfig) ([]SyntaxNodeRange, error) {
	parser := treesitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(config.language); err != nil {
		return nil, err
	}
	tree := parser.Parse(content, nil)
	if tree == nil {
		return nil, errors.New("tree-sitter parse returned nil tree")
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil || root.HasError() {
		return nil, errors.New("tree-sitter parse contains syntax error")
	}
	ranges := make([]SyntaxNodeRange, 0)
	collectTreeSitterRanges(root, content, config, &ranges)
	return ranges, nil
}

func collectTreeSitterRanges(
	node *treesitter.Node,
	content []byte,
	config treeSitterLanguageConfig,
	ranges *[]SyntaxNodeRange,
) {
	if node == nil {
		return
	}
	if symbolKind, ok := config.nodeKinds[node.Kind()]; ok {
		*ranges = append(*ranges, treeSitterNodeRange(node, content, symbolKind))
		return
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		collectTreeSitterRanges(&child, content, config, ranges)
	}
}

func treeSitterNodeRange(node *treesitter.Node, content []byte, symbolKind string) SyntaxNodeRange {
	startLine := int(node.StartPosition().Row) + 1
	endLine := treeSitterEndLine(node)
	return SyntaxNodeRange{
		StartLine:  startLine,
		EndLine:    endLine,
		SymbolName: treeSitterSymbolName(node, content),
		SymbolKind: symbolKind,
		ChunkKind:  "syntax_node",
		Parser:     treeSitterParserName,
	}
}

func treeSitterEndLine(node *treesitter.Node) int {
	endPosition := node.EndPosition()
	if endPosition.Column == 0 && endPosition.Row > node.StartPosition().Row {
		return int(endPosition.Row)
	}
	return int(endPosition.Row) + 1
}

func treeSitterSymbolName(node *treesitter.Node, content []byte) string {
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		return strings.TrimSpace(nameNode.Utf8Text(content))
	}
	return firstNamedIdentifier(node, content)
}

func firstNamedIdentifier(node *treesitter.Node, content []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier", "field_identifier", "type_identifier", "qualified_identifier", "namespace_identifier", "package_identifier":
		return strings.TrimSpace(node.Utf8Text(content))
	}
	cursor := node.Walk()
	defer cursor.Close()
	for _, child := range node.NamedChildren(cursor) {
		if name := firstNamedIdentifier(&child, content); name != "" {
			return name
		}
	}
	return ""
}

func normalizeCodeText(content []byte) string {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}
