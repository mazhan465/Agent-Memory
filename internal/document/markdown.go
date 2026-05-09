// 文件说明：实现 Markdown 文档解析器。
// 实现原理：扫描 Markdown 标题层级并忽略 fenced code block 内标题，将文档拆为标题、摘要和正文节点。
// 使用方式：调用 NewMarkdownParser 创建解析器，再通过 Parse 将 Markdown 内容解析为统一 Node 列表。
// 注意事项：解析器只识别文档结构，不进行最终 chunk 切分；chunk 切分由上层存储适配器负责。
// 交互模块：internal/splitter、internal/contextdoc。

package document

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
)

var nonSlugCharacter = regexp.MustCompile(`[^\p{L}\p{N}\-]+`)

// MarkdownParser 将 Markdown 内容解析为结构化文档节点。
type MarkdownParser struct{}

type markdownSection struct {
	StartLine   int
	EndLine     int
	HeadingPath []string
}

// NewMarkdownParser 创建 Markdown 文档解析器。
func NewMarkdownParser() *MarkdownParser {
	return &MarkdownParser{}
}

// Parse 将 Markdown 内容解析为标题、摘要和正文节点。
func (p *MarkdownParser) Parse(filePath string, content []byte) ([]Node, error) {
	text := normalizeNewlines(string(content))
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}

	lines := strings.Split(text, "\n")
	sections := collectMarkdownSections(lines)
	documentID := documentIDFromPath(filePath)
	nodes := make([]Node, 0, len(sections)*2)
	for _, section := range sections {
		nodes = append(nodes, markdownTitleNode(documentID, lines, section)...)
		if summary, ok := markdownSummaryNode(documentID, lines, section); ok {
			nodes = append(nodes, summary)
		}
		nodes = append(nodes, markdownBodyNode(documentID, lines, section))
	}
	return nodes, nil
}

func markdownTitleNode(documentID string, lines []string, section markdownSection) []Node {
	if len(section.HeadingPath) == 0 || section.StartLine >= len(lines) {
		return nil
	}
	return []Node{{
		Kind:          NodeKindTitle,
		Content:       section.HeadingPath[len(section.HeadingPath)-1],
		DocumentID:    documentID,
		SectionID:     sectionID(section.HeadingPath),
		HeadingPath:   append([]string(nil), section.HeadingPath...),
		KnowledgeKind: contextdoc.KnowledgeKindConcept,
		StartLine:     section.StartLine + 1,
		EndLine:       section.StartLine + 1,
	}}
}

func markdownSummaryNode(documentID string, lines []string, section markdownSection) (Node, bool) {
	start := section.StartLine
	if len(section.HeadingPath) > 0 {
		start++
	}
	for index := start; index < section.EndLine; index++ {
		line := strings.TrimSpace(lines[index])
		if line == "" {
			continue
		}
		summary, ok := parseSummaryLine(line)
		if !ok {
			return Node{}, false
		}
		return Node{
			Kind:          NodeKindSummary,
			Content:       summary,
			DocumentID:    documentID,
			SectionID:     sectionID(section.HeadingPath),
			HeadingPath:   append([]string(nil), section.HeadingPath...),
			KnowledgeKind: inferKnowledgeKind(joinHeadingPath(section.HeadingPath), summary),
			StartLine:     index + 1,
			EndLine:       index + 1,
		}, true
	}
	return Node{}, false
}

func markdownBodyNode(documentID string, lines []string, section markdownSection) Node {
	content := strings.Join(lines[section.StartLine:section.EndLine], "\n")
	headingPath := joinHeadingPath(section.HeadingPath)
	return Node{
		Kind:          NodeKindBody,
		Content:       content,
		DocumentID:    documentID,
		SectionID:     sectionID(section.HeadingPath),
		HeadingPath:   append([]string(nil), section.HeadingPath...),
		KnowledgeKind: inferKnowledgeKind(headingPath, content),
		StartLine:     section.StartLine + 1,
		EndLine:       section.EndLine,
	}
}

func collectMarkdownSections(lines []string) []markdownSection {
	sections := make([]markdownSection, 0)
	headings := make([]string, 6)
	sectionStart := 0
	sectionHeadings := []string{}
	inFence := false
	for index, line := range lines {
		if fenceLine(line) {
			inFence = !inFence
		}
		level, title, ok := markdownHeading(line)
		if inFence || !ok {
			continue
		}
		if index > sectionStart {
			sections = append(sections, markdownSection{StartLine: sectionStart, EndLine: index, HeadingPath: sectionHeadings})
		}
		headings[level-1] = title
		for reset := level; reset < len(headings); reset++ {
			headings[reset] = ""
		}
		sectionStart = index
		sectionHeadings = compactHeadings(headings)
	}
	if sectionStart < len(lines) {
		sections = append(sections, markdownSection{StartLine: sectionStart, EndLine: len(lines), HeadingPath: sectionHeadings})
	}
	return sections
}

func markdownHeading(line string) (int, string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return 0, "", false
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(trimmed) || trimmed[level] != ' ' {
		return 0, "", false
	}
	title := strings.TrimSpace(trimmed[level:])
	if title == "" {
		return 0, "", false
	}
	return level, title, true
}

func parseSummaryLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if summary, ok := strings.CutPrefix(trimmed, ">"); ok {
		return strings.TrimSpace(summary), true
	}
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"summary:", "abstract:", "摘要：", "摘要:"} {
		if strings.HasPrefix(lower, prefix) || strings.HasPrefix(trimmed, prefix) {
			return strings.TrimSpace(trimmed[len(prefix):]), true
		}
	}
	return "", false
}

func compactHeadings(headings []string) []string {
	result := make([]string, 0, len(headings))
	for _, heading := range headings {
		if heading != "" {
			result = append(result, heading)
		}
	}
	return result
}

func documentIDFromPath(filePath string) string {
	base := filepath.Base(filePath)
	extension := filepath.Ext(base)
	name := strings.TrimSuffix(base, extension)
	return slugify(name)
}

func sectionID(headings []string) string {
	if len(headings) == 0 {
		return "root"
	}
	parts := make([]string, 0, len(headings))
	for _, heading := range headings {
		parts = append(parts, slugify(heading))
	}
	return strings.Join(parts, "/")
}

func joinHeadingPath(headings []string) string {
	return strings.Join(headings, " > ")
}

func slugify(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	lower = strings.ReplaceAll(lower, "_", "-")
	lower = strings.ReplaceAll(lower, " ", "-")
	lower = nonSlugCharacter.ReplaceAllString(lower, "")
	lower = strings.Trim(lower, "-")
	if lower == "" {
		return "document"
	}
	return lower
}

func inferKnowledgeKind(headingPath string, content string) contextdoc.KnowledgeKind {
	text := strings.ToLower(headingPath + "\n" + content)
	switch {
	case containsAny(text, "troubleshoot", "faq", "error", "错误", "排障", "故障"):
		return contextdoc.KnowledgeKindTroubleshooting
	case containsAny(text, "example", "sample", "示例", "例子"):
		return contextdoc.KnowledgeKindExample
	case containsAny(text, "api", "function", "parameter", "接口", "参数", "配置项"):
		return contextdoc.KnowledgeKindAPI
	case containsAny(text, "must", "should", "rule", "禁止", "必须", "应该", "规范", "规则"):
		return contextdoc.KnowledgeKindRule
	default:
		return contextdoc.KnowledgeKindConcept
	}
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

func normalizeNewlines(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

func fenceLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}
