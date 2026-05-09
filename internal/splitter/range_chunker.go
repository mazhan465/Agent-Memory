// 文件说明：提供语法节点行区间到 Chunk 的通用转换能力。
// 实现原理：将不同解析器输出的语法节点行区间统一切成 splitter.Chunk，超长区间继续按行窗口拆分。
// 使用方式：TreeSitterSplitter 和后续其他语法切块器复用 RangeChunker 生成最终 chunk。
// 注意事项：行号使用 1-based 闭区间；调用方需要保证 ranges 已按源码语义选择好。
// 交互模块：internal/splitter。

package splitter

import (
	"sort"
	"strings"
)

const (
	// MetadataChunkKind 表示 chunk 来源类型。
	MetadataChunkKind = "chunk_kind"
	// MetadataSymbolName 表示语法节点符号名。
	MetadataSymbolName = "symbol_name"
	// MetadataSymbolKind 表示语法节点符号类型。
	MetadataSymbolKind = "symbol_kind"
	// MetadataParser 表示生成 chunk 的解析器。
	MetadataParser = "parser"
)

// SyntaxNodeRange 表示语法节点覆盖的源码行区间。
type SyntaxNodeRange struct {
	StartLine  int
	EndLine    int
	SymbolName string
	SymbolKind string
	ChunkKind  string
	Parser     string
}

// RangeChunker 将语法节点行区间转换为 Chunk。
type RangeChunker struct {
	MaxLines     int
	OverlapLines int
}

// NewRangeChunker 创建行区间切块器。
func NewRangeChunker(maxLines int, overlapLines int) *RangeChunker {
	lineSplitter := NewLineSplitter(maxLines, overlapLines)
	return &RangeChunker{MaxLines: lineSplitter.MaxLines, OverlapLines: lineSplitter.OverlapLines}
}

// Chunks 将语法节点行区间转换为可向量化 chunk。
func (c *RangeChunker) Chunks(filePath string, language string, lines []string, ranges []SyntaxNodeRange) []Chunk {
	if c == nil {
		c = NewRangeChunker(0, 0)
	}
	normalizedRanges := normalizeSyntaxRanges(ranges)
	chunks := make([]Chunk, 0, len(normalizedRanges))
	for _, nodeRange := range normalizedRanges {
		chunks = append(chunks, c.lineRangeChunks(filePath, language, lines, nodeRange)...)
	}
	return chunks
}

func (c *RangeChunker) lineRangeChunks(
	filePath string,
	language string,
	lines []string,
	nodeRange SyntaxNodeRange,
) []Chunk {
	startLine := max(nodeRange.StartLine, 1)
	endLine := min(nodeRange.EndLine, len(lines))
	if startLine > endLine {
		return nil
	}
	step := c.MaxLines - c.OverlapLines
	if step <= 0 {
		step = c.MaxLines
	}
	chunks := make([]Chunk, 0, (endLine-startLine+1)/step+1)
	for start := startLine; start <= endLine; start += step {
		end := min(start+c.MaxLines-1, endLine)
		content := strings.Join(lines[start-1:end], "\n")
		if strings.TrimSpace(content) != "" {
			chunks = append(chunks, Chunk{
				Content:   content,
				FilePath:  filePath,
				StartLine: start,
				EndLine:   end,
				Language:  language,
				Metadata:  syntaxMetadata(nodeRange),
			})
		}
		if end == endLine {
			break
		}
	}
	return chunks
}

func normalizeSyntaxRanges(ranges []SyntaxNodeRange) []SyntaxNodeRange {
	candidates := make([]SyntaxNodeRange, 0, len(ranges))
	for _, nodeRange := range ranges {
		if nodeRange.StartLine <= 0 || nodeRange.EndLine < nodeRange.StartLine {
			continue
		}
		candidates = append(candidates, nodeRange)
	}
	sort.SliceStable(candidates, func(i int, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.StartLine != right.StartLine {
			return left.StartLine < right.StartLine
		}
		if left.EndLine != right.EndLine {
			return left.EndLine > right.EndLine
		}
		return left.SymbolKind < right.SymbolKind
	})

	result := make([]SyntaxNodeRange, 0, len(candidates))
	for _, candidate := range candidates {
		if len(result) > 0 {
			last := result[len(result)-1]
			if candidate.StartLine >= last.StartLine && candidate.EndLine <= last.EndLine {
				continue
			}
		}
		result = append(result, candidate)
	}
	return result
}

func syntaxMetadata(nodeRange SyntaxNodeRange) map[string]string {
	metadata := make(map[string]string, 4)
	putMetadata(metadata, MetadataChunkKind, nodeRange.ChunkKind)
	putMetadata(metadata, MetadataSymbolName, nodeRange.SymbolName)
	putMetadata(metadata, MetadataSymbolKind, nodeRange.SymbolKind)
	putMetadata(metadata, MetadataParser, nodeRange.Parser)
	return metadata
}

func putMetadata(metadata map[string]string, key string, value string) {
	if strings.TrimSpace(value) != "" {
		metadata[key] = value
	}
}
