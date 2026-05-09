// 文件说明：测试 source 命令辅助逻辑。
// 实现原理：验证命令行 source type 别名到 contextdoc.SourceType 的映射。
// 使用方式：执行 go test ./cmd/code-context 或 go test ./...。
// 注意事项：测试不依赖外部存储。
// 交互模块：cmd/code-context/source.go。

package main

import (
	"testing"

	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
)

func TestSourceTypeFromArg(t *testing.T) {
	tests := []struct {
		name string
		want contextdoc.SourceType
	}{
		{name: "knowledge", want: contextdoc.SourceTypeDocument},
		{name: "external_knowledge", want: contextdoc.SourceTypeExternalKnowledge},
		{name: "conversation", want: contextdoc.SourceTypeConversation},
		{name: "experience", want: contextdoc.SourceTypeExperience},
		{name: "preference", want: contextdoc.SourceTypePreference},
		{name: "tool_history", want: contextdoc.SourceTypeToolHistory},
		{name: "fact", want: contextdoc.SourceTypeFact},
	}
	for _, test := range tests {
		got, err := sourceTypeFromArg(test.name)
		if err != nil {
			t.Fatalf("sourceTypeFromArg(%q) error = %v", test.name, err)
		}
		if got != test.want {
			t.Fatalf("sourceTypeFromArg(%q) = %s, want %s", test.name, got, test.want)
		}
	}
}
