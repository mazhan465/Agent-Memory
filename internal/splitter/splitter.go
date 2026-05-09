// 文件说明：定义通用切块接口并提供行级切块实现。
// 实现原理：按固定最大行数切分文件内容，并使用重叠行保留上下文连续性。
// 使用方式：索引器将扫描到的文件内容传入 Splitter.Split，得到带行号和可选元数据的 Chunk 列表。
// 注意事项：当前行级实现适合 MVP 通用切块，文档知识库可使用 MarkdownSplitter 保留章节元数据。
// 交互模块：internal/indexer、internal/scanner、internal/embed。

// Package splitter 提供代码文本切块能力。
package splitter

import "strings"

const defaultLanguage = "text"

// Chunk 表示一个可向量化的内容片段。
type Chunk struct {
	Content   string
	FilePath  string
	StartLine int
	EndLine   int
	Language  string
	Metadata  map[string]string
}

// Splitter 定义代码切块接口。
type Splitter interface {
	Split(filePath string, content []byte, language string) []Chunk
}

// LineSplitter 按行切分代码文件。
type LineSplitter struct {
	MaxLines     int
	OverlapLines int
}

// NewLineSplitter 创建行级切块器。
func NewLineSplitter(maxLines int, overlapLines int) *LineSplitter {
	if maxLines <= 0 {
		maxLines = 120
	}
	if overlapLines < 0 {
		overlapLines = 0
	}
	if overlapLines >= maxLines {
		overlapLines = maxLines / 4
	}
	return &LineSplitter{MaxLines: maxLines, OverlapLines: overlapLines}
}

// Split 将文件内容切分成带行号的代码片段。
func (s *LineSplitter) Split(filePath string, content []byte, language string) []Chunk {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(text) == "" {
		return nil
	}

	if language == "" {
		language = defaultLanguage
	}

	step := s.MaxLines - s.OverlapLines
	if step <= 0 {
		step = s.MaxLines
	}

	chunks := make([]Chunk, 0, len(lines)/step+1)
	for start := 0; start < len(lines); start += step {
		end := min(start+s.MaxLines, len(lines))

		chunkText := strings.Join(lines[start:end], "\n")
		if strings.TrimSpace(chunkText) != "" {
			chunks = append(chunks, Chunk{
				Content:   chunkText,
				FilePath:  filePath,
				StartLine: start + 1,
				EndLine:   end,
				Language:  language,
			})
		}
		if end == len(lines) {
			break
		}
	}
	return chunks
}
