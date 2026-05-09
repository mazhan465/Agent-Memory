// 文件说明：实现多信号领域解析框架。
// 实现原理：将显式上下文、规则分类、领域画像和记忆分布转换为 Evidence，再按权重融合为领域决策。
// 使用方式：搜索器或会话上下文构建器调用 DomainResolver.Resolve 获取领域、置信度、evidence 和检索预算。
// 注意事项：当前 ProfileSignal 使用轻量文本匹配，后续可替换为 embedding 相似度匹配。
// 交互模块：internal/domain、internal/contextdoc、internal/searcher、internal/sessioncontext。

package domain

import (
	"context"
	"sort"
	"strings"

	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
)

const (
	sourceExplicit           = "explicit_context"
	sourceRule               = "rule"
	sourceProfile            = "domain_profile"
	sourceMemoryDistribution = "memory_distribution"
)

// Evidence 表示领域判断证据。
type Evidence struct {
	Source string            `json:"source"`
	Domain contextdoc.Domain `json:"domain_path"`
	Score  float64           `json:"score"`
	Reason string            `json:"reason"`
}

// Decision 表示多信号领域判断结果。
type Decision struct {
	Domain           contextdoc.Domain         `json:"domain_path,omitempty"`
	ParentDomain     contextdoc.Domain         `json:"parent_domain,omitempty"`
	DomainConfidence float64                   `json:"domain_confidence"`
	ExperienceKind   contextdoc.ExperienceKind `json:"experience_kind"`
	Budget           Budget                    `json:"budget"`
	Evidence         []Evidence                `json:"evidence,omitempty"`
}

// Signal 定义领域判断信号源。
type Signal interface {
	Evaluate(ctx context.Context, input Input) ([]Evidence, error)
}

// DomainResolver 融合多个领域信号源。
type DomainResolver struct {
	signals []weightedSignal
}

type weightedSignal struct {
	weight float64
	signal Signal
}

// NewDomainResolver 创建领域解析器。
func NewDomainResolver(signals ...Signal) *DomainResolver {
	if len(signals) == 0 {
		return NewDefaultDomainResolver()
	}
	weightedSignals := make([]weightedSignal, 0, len(signals))
	for _, signal := range signals {
		weightedSignals = append(weightedSignals, weightedSignal{weight: 1, signal: signal})
	}
	return &DomainResolver{signals: weightedSignals}
}

// NewDefaultDomainResolver 创建默认多信号领域解析器。
func NewDefaultDomainResolver() *DomainResolver {
	return &DomainResolver{signals: []weightedSignal{
		{weight: 0.35, signal: ExplicitSignal{}},
		{weight: 0.35, signal: NewProfileSignal(DefaultProfileStore())},
		{weight: 0.20, signal: MemoryDistributionSignal{}},
		{weight: 0.10, signal: NewRuleSignal(NewDefaultClassifier())},
	}}
}

// Resolve 融合多个信号并输出领域决策。
func (r *DomainResolver) Resolve(ctx context.Context, input Input) (Decision, error) {
	if r == nil || len(r.signals) == 0 {
		r = NewDefaultDomainResolver()
	}
	scores := make(map[contextdoc.Domain]float64)
	evidenceList := make([]Evidence, 0)
	for _, weighted := range r.signals {
		if weighted.signal == nil || weighted.weight <= 0 {
			continue
		}
		evidences, err := weighted.signal.Evaluate(ctx, input)
		if err != nil {
			return Decision{}, err
		}
		for _, evidence := range evidences {
			if evidence.Domain == "" || evidence.Score <= 0 {
				continue
			}
			normalizedScore := clamp01(evidence.Score)
			evidence.Score = normalizedScore
			evidenceList = append(evidenceList, evidence)
			scores[evidence.Domain] += normalizedScore * weighted.weight
		}
	}

	detectedDomain, score := bestDomain(scores)
	confidence := clamp01(score)
	if confidence < mediumConfidence {
		return Decision{
			ExperienceKind: contextdoc.ExperienceKindGeneral,
			Budget:         generalBudget(),
			Evidence:       evidenceList,
		}, nil
	}
	return Decision{
		Domain:           detectedDomain,
		ParentDomain:     detectedDomain.Parent(),
		DomainConfidence: confidence,
		ExperienceKind:   contextdoc.ExperienceKindDomain,
		Budget:           budgetForConfidence(confidence),
		Evidence:         topEvidence(evidenceList, detectedDomain, 8),
	}, nil
}

// Analyze 兼容 Classifier 接口。
func (r *DomainResolver) Analyze(input Input) Result {
	decision, err := r.Resolve(context.Background(), input)
	if err != nil {
		return Result{ExperienceKind: contextdoc.ExperienceKindGeneral, Budget: generalBudget()}
	}
	return Result{
		Domain:           decision.Domain,
		DomainConfidence: decision.DomainConfidence,
		ExperienceKind:   decision.ExperienceKind,
		Budget:           decision.Budget,
	}
}

// ExplicitSignal 从显式上下文中提取领域证据。
type ExplicitSignal struct{}

// Evaluate 返回显式上下文领域证据。
func (s ExplicitSignal) Evaluate(ctx context.Context, input Input) ([]Evidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	evidences := make([]Evidence, 0)
	if domainValue := strings.TrimSpace(input.Metadata["domain_path"]); domainValue != "" {
		domainValue, err := contextdoc.NormalizeDomain(domainValue)
		if err == nil {
			evidences = append(evidences, Evidence{Source: sourceExplicit, Domain: domainValue, Score: 1, Reason: "metadata domain_path"})
		}
	}
	for _, extension := range input.FileExtensions {
		switch normalizeExtension(extension) {
		case ".go":
			evidences = append(evidences, Evidence{Source: sourceExplicit, Domain: mustDomain("programming/go"), Score: 0.8, Reason: "go file extension"})
		case ".sql":
			evidences = append(evidences, Evidence{Source: sourceExplicit, Domain: mustDomain("database/sql"), Score: 0.8, Reason: "sql file extension"})
		case ".ts", ".tsx", ".js", ".jsx":
			evidences = append(evidences, Evidence{Source: sourceExplicit, Domain: mustDomain("programming/frontend"), Score: 0.65, Reason: "frontend file extension"})
		}
	}
	return evidences, nil
}

// RuleSignal 将规则分类器包装为信号源。
type RuleSignal struct {
	classifier Classifier
}

// NewRuleSignal 创建规则信号源。
func NewRuleSignal(classifier Classifier) RuleSignal {
	if classifier == nil {
		classifier = NewDefaultClassifier()
	}
	return RuleSignal{classifier: classifier}
}

// Evaluate 返回规则分类领域证据。
func (s RuleSignal) Evaluate(ctx context.Context, input Input) ([]Evidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := s.classifier.Analyze(input)
	if result.Domain == "" || result.DomainConfidence <= 0 {
		return nil, nil
	}
	return []Evidence{{Source: sourceRule, Domain: result.Domain, Score: result.DomainConfidence, Reason: "rule classifier"}}, nil
}

// ProfileSignal 基于领域画像文本匹配生成证据。
type ProfileSignal struct {
	store ProfileStore
}

// NewProfileSignal 创建领域画像信号源。
func NewProfileSignal(store ProfileStore) ProfileSignal {
	if store == nil {
		store = DefaultProfileStore()
	}
	return ProfileSignal{store: store}
}

// Evaluate 返回领域画像匹配证据。
func (s ProfileSignal) Evaluate(ctx context.Context, input Input) ([]Evidence, error) {
	profiles, err := s.store.ListProfiles(ctx)
	if err != nil {
		return nil, err
	}
	text := normalizeText(input.Query + " " + input.ToolName + " " + metadataText(input.Metadata))
	evidences := make([]Evidence, 0, len(profiles))
	for _, profile := range profiles {
		score := profileTextScore(text, profile)
		if score <= 0 {
			continue
		}
		evidences = append(evidences, Evidence{Source: sourceProfile, Domain: profile.Domain, Score: score, Reason: "domain profile text match"})
	}
	return evidences, nil
}

// MemoryDistributionSignal 根据预检索记忆领域分布生成证据。
type MemoryDistributionSignal struct{}

// Evaluate 返回记忆领域分布证据。
func (s MemoryDistributionSignal) Evaluate(ctx context.Context, input Input) ([]Evidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(input.MemoryDomains) == 0 {
		return nil, nil
	}
	counts := make(map[contextdoc.Domain]int)
	for _, memoryDomain := range input.MemoryDomains {
		if memoryDomain == "" {
			continue
		}
		counts[memoryDomain]++
	}
	evidences := make([]Evidence, 0, len(counts))
	for memoryDomain, count := range counts {
		evidences = append(evidences, Evidence{
			Source: sourceMemoryDistribution,
			Domain: memoryDomain,
			Score:  float64(count) / float64(len(input.MemoryDomains)),
			Reason: "memory hit distribution",
		})
	}
	return evidences, nil
}

func profileTextScore(text string, profile DomainProfile) float64 {
	if text == "" {
		return 0
	}
	matches := 0
	profileTerms := append([]string{string(profile.Domain), profile.Description}, profile.Aliases...)
	profileTerms = append(profileTerms, profile.Examples...)
	for _, term := range profileTerms {
		term = normalizeText(term)
		if term != "" && strings.Contains(text, term) {
			matches++
		}
	}
	if matches == 0 {
		return 0
	}
	return clamp01(float64(matches) / 3.0)
}

func topEvidence(evidences []Evidence, domain contextdoc.Domain, limit int) []Evidence {
	matched := make([]Evidence, 0, len(evidences))
	for _, evidence := range evidences {
		if evidence.Domain == domain {
			matched = append(matched, evidence)
		}
	}
	sort.SliceStable(matched, func(i int, j int) bool {
		return matched[i].Score > matched[j].Score
	})
	if limit > 0 && len(matched) > limit {
		return matched[:limit]
	}
	return matched
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
