// 文件说明：测试 CLI 导入辅助逻辑。
// 实现原理：使用临时 JSON/JSONL 文件验证长期记忆记录解析和 source 类型识别。
// 使用方式：执行 go test ./cmd/code-context 或 go test ./...。
// 注意事项：测试仅使用临时目录，不访问真实用户数据。
// 交互模块：cmd/code-context/import.go。

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
)

func TestReadMemoryRecordsJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.jsonl")
	content := "{\"id\":\"one\",\"content\":\"first memory\"}\n" +
		"{\"id\":\"two\",\"content\":\"second memory\",\"domain_path\":\"programming/go\"}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	records, err := readMemoryRecords(path)
	if err != nil {
		t.Fatalf("readMemoryRecords() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	if records[1].DomainPath != "programming/go" {
		t.Fatalf("DomainPath = %s, want programming/go", records[1].DomainPath)
	}
}

func TestReadMemoryRecordsJSONArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.json")
	content := `[{"id":"one","content":"first memory","tags":["go"]}]`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	records, err := readMemoryRecords(path)
	if err != nil {
		t.Fatalf("readMemoryRecords() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if len(records[0].Tags) != 1 || records[0].Tags[0] != "go" {
		t.Fatalf("Tags = %+v, want [go]", records[0].Tags)
	}
}

func TestReadMemoryRecordsRejectsEmptyContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.jsonl")
	if err := os.WriteFile(path, []byte("{\"id\":\"bad\"}\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := readMemoryRecords(path)
	if err == nil {
		t.Fatal("readMemoryRecords() error = nil, want empty content error")
	}
}

func TestMemorySourceType(t *testing.T) {
	tests := []struct {
		name string
		want contextdoc.SourceType
	}{
		{name: "conversation", want: contextdoc.SourceTypeConversation},
		{name: "experience", want: contextdoc.SourceTypeExperience},
		{name: "preference", want: contextdoc.SourceTypePreference},
		{name: "tool_history", want: contextdoc.SourceTypeToolHistory},
		{name: "fact", want: contextdoc.SourceTypeFact},
	}
	for _, test := range tests {
		got, err := memorySourceType(test.name)
		if err != nil {
			t.Fatalf("memorySourceType(%q) error = %v", test.name, err)
		}
		if got != test.want {
			t.Fatalf("memorySourceType(%q) = %s, want %s", test.name, got, test.want)
		}
	}
}
