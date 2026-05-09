// 文件说明：实现基础领域识别和经验检索预算建议。
// 实现原理：基于 query、文件扩展名、工具名和 source metadata 做规则打分，输出领域、置信度、经验类型和预算建议。
// 使用方式：搜索器和会话上下文构建器可调用 DefaultClassifier.Analyze 获取领域匹配结果。
// 注意事项：当前为轻量规则分类器，不依赖外部模型；后续可替换为 LLM 或机器学习分类器。
// 交互模块：internal/contextdoc、internal/searcher、internal/sessioncontext。

// Package domain 提供 Agent/Coding 场景的领域识别能力。
package domain

import (
	"math"
	"path/filepath"
	"strings"

	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
)

const (
	mediumConfidence = 0.35
	strongConfidence = 0.75
	maxScore         = 4.0
)

// Input 表示领域识别输入。
type Input struct {
	Query          string
	FileExtensions []string
	ToolName       string
	Metadata       map[string]string
	MemoryDomains  []contextdoc.Domain
}

// Budget 表示不同经验类型的检索预算占比。
type Budget struct {
	DomainPercent      int
	ProjectToolPercent int
	GeneralPercent     int
}

// Result 表示领域识别结果。
type Result struct {
	Domain           contextdoc.Domain
	DomainConfidence float64
	ExperienceKind   contextdoc.ExperienceKind
	Budget           Budget
}

// Classifier 定义领域识别接口。
type Classifier interface {
	Analyze(input Input) Result
}

// DefaultClassifier 是基于规则的领域识别器。
type DefaultClassifier struct {
	rules []rule
}

// NewDefaultClassifier 创建默认领域识别器。
func NewDefaultClassifier() *DefaultClassifier {
	return &DefaultClassifier{rules: defaultRules()}
}

// Analyze 识别输入所属领域，并给出经验类型和检索预算建议。
func (c *DefaultClassifier) Analyze(input Input) Result {
	if c == nil || len(c.rules) == 0 {
		c = NewDefaultClassifier()
	}
	scores := make(map[contextdoc.Domain]float64, len(c.rules))
	text := normalizeText(input.Query + " " + input.ToolName + " " + metadataText(input.Metadata))
	for _, rule := range c.rules {
		scores[rule.domain] += rule.scoreText(text)
		scores[rule.domain] += rule.scoreExtensions(input.FileExtensions)
	}

	detectedDomain, score := bestDomain(scores)
	confidence := normalizeScore(score)
	if confidence < mediumConfidence {
		return Result{
			ExperienceKind: contextdoc.ExperienceKindGeneral,
			Budget:         generalBudget(),
		}
	}
	return Result{
		Domain:           detectedDomain,
		DomainConfidence: confidence,
		ExperienceKind:   contextdoc.ExperienceKindDomain,
		Budget:           budgetForConfidence(confidence),
	}
}

type rule struct {
	domain     contextdoc.Domain
	keywords   []string
	extensions []string
}

func (r rule) scoreText(text string) float64 {
	score := 0.0
	for _, keyword := range r.keywords {
		if strings.Contains(text, normalizeText(keyword)) {
			score++
		}
	}
	return score
}

func (r rule) scoreExtensions(extensions []string) float64 {
	score := 0.0
	for _, extension := range extensions {
		cleanExtension := normalizeExtension(extension)
		for _, ruleExtension := range r.extensions {
			if cleanExtension == ruleExtension {
				score += 1.5
			}
		}
	}
	return score
}

func defaultRules() []rule {
	return []rule{
		{
			domain:     mustDomain("programming/go"),
			keywords:   []string{"go", "golang", "goroutine", "channel", "gofmt", "go test", "go mod", "panic", "defer"},
			extensions: []string{".go"},
		},
		{
			domain: mustDomain("database/milvus"),
			keywords: []string{
				"milvus", "vector database", "vector store", "collection", "embedding", "topk", "hybrid search",
			},
		},
		{
			domain:     mustDomain("infrastructure/kubernetes"),
			keywords:   []string{"kubernetes", "k8s", "pod", "deployment", "container", "helm", "kubectl", "namespace"},
			extensions: []string{".yaml", ".yml"},
		},
		{
			domain: mustDomain("programming/frontend"),
			keywords: []string{
				"frontend", "react", "vue", "typescript", "javascript", "css", "tailwind", "vite", "webpack",
			},
			extensions: []string{".ts", ".tsx", ".js", ".jsx", ".css", ".html"},
		},
		{
			domain:     mustDomain("database/sql"),
			keywords:   []string{"database", "sql", "mysql", "postgres", "redis", "transaction", "index", "query"},
			extensions: []string{".sql"},
		},
		{
			domain:     mustDomain("programming/testing"),
			keywords:   []string{"test", "unit test", "coverage", "mock", "assert", "fixture", "go test"},
			extensions: []string{"_test.go"},
		},
		{
			domain:     mustDomain("devops/deployment"),
			keywords:   []string{"deploy", "release", "build", "ci", "pipeline", "docker", "makefile", "rollback"},
			extensions: []string{"dockerfile", "makefile"},
		},
	}
}

func mustDomain(value string) contextdoc.Domain {
	domain, err := contextdoc.NormalizeDomain(value)
	if err != nil {
		panic(err)
	}
	return domain
}

func bestDomain(scores map[contextdoc.Domain]float64) (contextdoc.Domain, float64) {
	var best contextdoc.Domain
	bestScore := 0.0
	for domain, score := range scores {
		if score > bestScore || score == bestScore && string(domain) < string(best) {
			best = domain
			bestScore = score
		}
	}
	return best, bestScore
}

func normalizeScore(score float64) float64 {
	if score <= 0 {
		return 0
	}
	return math.Min(score/maxScore, 1)
}

func budgetForConfidence(confidence float64) Budget {
	if confidence >= strongConfidence {
		return Budget{DomainPercent: 65, ProjectToolPercent: 25, GeneralPercent: 10}
	}
	return Budget{DomainPercent: 50, ProjectToolPercent: 30, GeneralPercent: 20}
}

func generalBudget() Budget {
	return Budget{DomainPercent: 0, ProjectToolPercent: 30, GeneralPercent: 70}
}

func metadataText(metadata map[string]string) string {
	parts := make([]string, 0, len(metadata))
	for key, value := range metadata {
		parts = append(parts, key, value)
	}
	return strings.Join(parts, " ")
}

func normalizeText(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}

func normalizeExtension(extension string) string {
	cleanExtension := strings.ToLower(strings.TrimSpace(extension))
	if cleanExtension == "" {
		return ""
	}
	base := strings.ToLower(filepath.Base(cleanExtension))
	if base == "dockerfile" || base == "makefile" || strings.HasSuffix(base, "_test.go") {
		return base
	}
	if strings.HasPrefix(cleanExtension, ".") {
		return cleanExtension
	}
	return "." + cleanExtension
}
