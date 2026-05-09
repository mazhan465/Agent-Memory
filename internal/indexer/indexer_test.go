// 文件说明：测试代码库索引器的全量和增量索引流程。
// 实现原理：使用临时代码库、本地 HashEmbedder、本地 VectorStore 和 SnapshotStore，验证文件增删改后只更新变化文件统计。
// 使用方式：执行 go test ./internal/indexer 或 go test ./...。
// 注意事项：测试只读写临时目录，不连接外部向量数据库。
// 交互模块：internal/indexer、internal/scanner、internal/splitter、internal/embed、internal/vectorstore、internal/snapshot。

package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mazhan465/Agent-Memory/internal/embed"
	"github.com/mazhan465/Agent-Memory/internal/scanner"
	"github.com/mazhan465/Agent-Memory/internal/snapshot"
	"github.com/mazhan465/Agent-Memory/internal/splitter"
	"github.com/mazhan465/Agent-Memory/internal/vectorstore"
)

func TestIndexerIndexIncremental(t *testing.T) {
	ctx := context.Background()
	repoPath := t.TempDir()
	storagePath := t.TempDir()
	writeIndexerTestFile(t, filepath.Join(repoPath, "a.go"), "package main\n\nfunc A() {}\n")
	writeIndexerTestFile(t, filepath.Join(repoPath, "b.go"), "package main\n\nfunc B() {}\n")

	store := vectorstore.NewLocalStore(storagePath)
	indexer := New(
		scanner.New([]string{".go"}, nil),
		splitter.NewLineSplitter(20, 0),
		embed.NewHashEmbedder(8),
		store,
		snapshot.NewStore(storagePath),
	)
	firstStats, err := indexer.Index(ctx, repoPath)
	if err != nil {
		t.Fatalf("Index() first error = %v", err)
	}
	if !firstStats.FullReindex || firstStats.IndexedFiles != 2 || firstStats.TotalChunks != 2 {
		t.Fatalf("first stats = %+v, want full reindex with 2 files and 2 chunks", firstStats)
	}

	writeIndexerTestFile(t, filepath.Join(repoPath, "a.go"), "package main\n\nfunc A() string { return \"changed\" }\n")
	if err := os.Remove(filepath.Join(repoPath, "b.go")); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	writeIndexerTestFile(t, filepath.Join(repoPath, "c.go"), "package main\n\nfunc C() {}\n")

	secondStats, err := indexer.Index(ctx, repoPath)
	if err != nil {
		t.Fatalf("Index() second error = %v", err)
	}
	if secondStats.FullReindex {
		t.Fatalf("second stats FullReindex = true, want false")
	}
	if secondStats.AddedFiles != 1 || secondStats.ModifiedFiles != 1 || secondStats.RemovedFiles != 1 {
		t.Fatalf("second stats changes = %+v, want added=1 modified=1 removed=1", secondStats)
	}
	if secondStats.IndexedFiles != 2 || secondStats.TotalChunks != 2 {
		t.Fatalf("second stats = %+v, want 2 files and 2 chunks", secondStats)
	}
	count, err := store.Count(ctx, secondStats.Namespace)
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("Count() = %d, want 2", count)
	}
}

func writeIndexerTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
