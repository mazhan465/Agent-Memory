// 文件说明：测试 sync 命令的基础行为。
// 实现原理：使用临时代码库和临时 AGENT_MEMORY_HOME，验证未索引路径不能 sync，已索引路径可增量 sync。
// 使用方式：执行 go test ./cmd/code-context 或 go test ./...。
// 注意事项：测试只使用本地 HashEmbedder 和 LocalStore。
// 交互模块：cmd/code-context/main.go、internal/indexer、internal/snapshot。

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSyncRequiresExistingIndex(t *testing.T) {
	setSyncTestEnv(t)
	repoPath := t.TempDir()
	writeSyncTestFile(t, filepath.Join(repoPath, "main.go"), "package main\n")

	err := run(context.Background(), []string{"sync", repoPath})
	if err == nil || !strings.Contains(err.Error(), "not indexed") {
		t.Fatalf("run sync error = %v, want not indexed", err)
	}
}

func TestRunSyncUpdatesIndexedPath(t *testing.T) {
	setSyncTestEnv(t)
	repoPath := t.TempDir()
	writeSyncTestFile(t, filepath.Join(repoPath, "main.go"), "package main\n\nfunc A() {}\n")

	if err := run(context.Background(), []string{"index", repoPath}); err != nil {
		t.Fatalf("run index error = %v", err)
	}
	writeSyncTestFile(t, filepath.Join(repoPath, "main.go"), "package main\n\nfunc A() string { return \"changed\" }\n")
	if err := run(context.Background(), []string{"sync", repoPath}); err != nil {
		t.Fatalf("run sync error = %v", err)
	}
}

func setSyncTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AGENT_MEMORY_HOME", t.TempDir())
	t.Setenv("AGENT_MEMORY_EMBEDDING_PROVIDER", "hash")
	t.Setenv("AGENT_MEMORY_VECTOR_STORE", "local")
}

func writeSyncTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
