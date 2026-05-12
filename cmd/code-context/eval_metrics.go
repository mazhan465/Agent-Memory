// 文件说明：提供评估指标的运行效率和上下文预算估算辅助函数。
// 实现原理：基于搜索结果内容统计字符数、粗略 token 数，并计算浮点分位值。
// 使用方式：eval recall 在汇总和单 case 输出中调用这些 helper 填充效率指标。
// 注意事项：token 数为轻量估算值，不代表具体模型 tokenizer 的精确结果。
// 交互模块：cmd/code-context/eval.go。
package main

import (
	"math"
	"sort"
	"strings"
)

func searchResultCharacters(results []searchJSONResult) int {
	total := 0
	for _, result := range results {
		total += len([]rune(result.Content))
	}
	return total
}

func estimatedSearchResultTokens(results []searchJSONResult) int {
	total := 0
	for _, result := range results {
		total += estimateTextTokens(result.Content)
	}
	return total
}

func estimateTextTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	return (len([]rune(text)) + 3) / 4
}

func percentileFloat64(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	items := append([]float64(nil), values...)
	sort.Float64s(items)
	if percentile <= 0 {
		return items[0]
	}
	if percentile >= 1 {
		return items[len(items)-1]
	}
	index := int(math.Ceil(percentile*float64(len(items)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(items) {
		index = len(items) - 1
	}
	return items[index]
}
