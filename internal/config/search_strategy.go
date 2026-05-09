// 文件说明：定义不同来源类型的混合检索权重策略。
// 实现原理：为 code、knowledge、conversation、preference 等来源设置默认 semantic/keyword 权重，并允许通过环境变量覆盖。
// 使用方式：搜索入口根据来源类型调用 Config.SearchStrategy 获取对应策略后传给 VectorStore。
// 注意事项：权重会归一化，非法环境变量会被忽略并使用默认值。
// 交互模块：cmd/code-context、internal/vectorstore。

package config

import (
	"os"
	"strconv"
	"strings"
)

const (
	// SearchStrategyDefault 表示兜底搜索策略。
	SearchStrategyDefault = "default"
	// SearchStrategyCode 表示代码库搜索策略。
	SearchStrategyCode = "code"
	// SearchStrategyKnowledge 表示知识库文档搜索策略。
	SearchStrategyKnowledge = "knowledge"
	// SearchStrategyConversation 表示历史会话搜索策略。
	SearchStrategyConversation = "conversation"
	// SearchStrategyExperience 表示经验记忆搜索策略。
	SearchStrategyExperience = "experience"
	// SearchStrategyPreference 表示用户偏好搜索策略。
	SearchStrategyPreference = "preference"
	// SearchStrategyToolHistory 表示工具历史搜索策略。
	SearchStrategyToolHistory = "tool_history"
	// SearchStrategyFact 表示事实记忆搜索策略。
	SearchStrategyFact = "fact"
)

// SearchStrategy 返回指定来源类型的搜索策略。
func (c Config) SearchStrategy(name string) SearchStrategy {
	key := normalizeSearchStrategyName(name)
	if strategy, ok := c.SearchStrategies[key]; ok {
		return normalizeSearchStrategy(strategy)
	}
	if strategy, ok := c.SearchStrategies[SearchStrategyDefault]; ok {
		return normalizeSearchStrategy(strategy)
	}
	return SearchStrategy{SemanticWeight: 0.75, KeywordWeight: 0.25}
}

func defaultSearchStrategies() map[string]SearchStrategy {
	return map[string]SearchStrategy{
		SearchStrategyDefault:      {SemanticWeight: 0.75, KeywordWeight: 0.25},
		SearchStrategyCode:         {SemanticWeight: 0.60, KeywordWeight: 0.40},
		SearchStrategyKnowledge:    {SemanticWeight: 0.70, KeywordWeight: 0.30},
		SearchStrategyConversation: {SemanticWeight: 0.85, KeywordWeight: 0.15},
		SearchStrategyExperience:   {SemanticWeight: 0.75, KeywordWeight: 0.25},
		SearchStrategyPreference:   {SemanticWeight: 0.55, KeywordWeight: 0.45},
		SearchStrategyToolHistory:  {SemanticWeight: 0.80, KeywordWeight: 0.20},
		SearchStrategyFact:         {SemanticWeight: 0.65, KeywordWeight: 0.35},
		"external_knowledge":       {SemanticWeight: 0.70, KeywordWeight: 0.30},
	}
}

func applySearchStrategyEnv(strategies map[string]SearchStrategy) {
	for name, strategy := range strategies {
		semanticWeight := getOptionalFloat(searchStrategyEnvName(name, "SEMANTIC_WEIGHT"), strategy.SemanticWeight)
		keywordWeight := getOptionalFloat(searchStrategyEnvName(name, "KEYWORD_WEIGHT"), strategy.KeywordWeight)
		strategies[name] = normalizeSearchStrategy(SearchStrategy{
			SemanticWeight: semanticWeight,
			KeywordWeight:  keywordWeight,
		})
	}
}

func normalizeSearchStrategy(strategy SearchStrategy) SearchStrategy {
	semanticWeight := strategy.SemanticWeight
	keywordWeight := strategy.KeywordWeight
	if semanticWeight < 0 {
		semanticWeight = 0
	}
	if keywordWeight < 0 {
		keywordWeight = 0
	}
	if semanticWeight == 0 && keywordWeight == 0 {
		return SearchStrategy{SemanticWeight: 0.75, KeywordWeight: 0.25}
	}
	total := semanticWeight + keywordWeight
	return SearchStrategy{SemanticWeight: semanticWeight / total, KeywordWeight: keywordWeight / total}
}

func normalizeSearchStrategyName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func searchStrategyEnvName(name string, suffix string) string {
	upperName := strings.ToUpper(strings.ReplaceAll(normalizeSearchStrategyName(name), "-", "_"))
	return "AGENT_MEMORY_SEARCH_" + upperName + "_" + suffix
}

func getOptionalFloat(name string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsedValue, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsedValue
}
