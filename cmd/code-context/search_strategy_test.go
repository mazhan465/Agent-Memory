// 文件说明：测试 CLI 搜索来源类型到混合检索策略的映射。
// 实现原理：构造 searchJSONNamespace，验证 code、knowledge、conversation、preference 等来源类型映射到预期策略名称。
// 使用方式：执行 go test ./cmd/code-context 或 go test ./...。
// 注意事项：测试不访问向量存储。
// 交互模块：cmd/code-context/search.go、internal/config、internal/contextdoc。

package main

import (
	"testing"

	"github.com/mazhan465/Agent-Memory/internal/config"
	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
)

func TestSearchStrategyName(t *testing.T) {
	tests := []struct {
		name            string
		searchNamespace searchJSONNamespace
		want            string
	}{
		{
			name:            "code",
			searchNamespace: searchJSONNamespace{SourceType: string(contextdoc.SourceTypeCodebase)},
			want:            config.SearchStrategyCode,
		},
		{
			name:            "knowledge",
			searchNamespace: searchJSONNamespace{SourceType: string(contextdoc.SourceTypeDocument)},
			want:            config.SearchStrategyKnowledge,
		},
		{
			name:            "conversation",
			searchNamespace: searchJSONNamespace{SourceType: string(contextdoc.SourceTypeConversation)},
			want:            config.SearchStrategyConversation,
		},
		{
			name:            "preference",
			searchNamespace: searchJSONNamespace{SourceType: string(contextdoc.SourceTypePreference)},
			want:            config.SearchStrategyPreference,
		},
		{
			name:            "fallback",
			searchNamespace: searchJSONNamespace{SourceType: "unknown"},
			want:            config.SearchStrategyDefault,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := searchStrategyName(tt.searchNamespace)
			if got != tt.want {
				t.Fatalf("searchStrategyName() = %s, want %s", got, tt.want)
			}
		})
	}
}
