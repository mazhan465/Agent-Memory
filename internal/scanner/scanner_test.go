// 文件说明：测试代码库扫描和 ignore 规则过滤。
// 实现原理：构造临时目录、ignore 文件和自定义 glob 规则，验证 Scanner 只返回应索引文件。
// 使用方式：执行 go test ./internal/scanner 或 go test ./...。
// 注意事项：测试只读写临时目录，不依赖真实代码库。
// 交互模块：internal/scanner。

package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScannerScanUsesIgnoreFilesAndPatterns(t *testing.T) {
	root := t.TempDir()
	writeScannerTestFile(t, filepath.Join(root, ".gitignore"), "ignored-dir/\n*.tmp\n")
	writeScannerTestFile(t, filepath.Join(root, ".contextignore"), "private/**\n")
	writeScannerTestFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeScannerTestFile(t, filepath.Join(root, "keep", "service.go"), "package keep\n")
	writeScannerTestFile(t, filepath.Join(root, "ignored-dir", "skip.go"), "package skip\n")
	writeScannerTestFile(t, filepath.Join(root, "private", "secret.go"), "package private\n")
	writeScannerTestFile(t, filepath.Join(root, "generated", "skip.go"), "package generated\n")
	writeScannerTestFile(t, filepath.Join(root, "note.tmp"), "temporary\n")

	scanner := NewWithPatterns([]string{".go", ".tmp"}, []string{"node_modules"}, []string{"generated/**"})
	files, err := scanner.Scan(root)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	got := make([]string, 0, len(files))
	for _, file := range files {
		got = append(got, file.RelativePath)
	}
	want := []string{"keep/service.go", "main.go"}
	if len(got) != len(want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("files = %v, want %v", got, want)
		}
		if files[index].Size <= 0 || files[index].ModTimeUnixNano <= 0 {
			t.Fatalf("file metadata = %+v, want positive size and mod time", files[index])
		}
	}
}

func writeScannerTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
