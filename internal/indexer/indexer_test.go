// 文件说明：测试代码库索引器的全量和增量索引流程。
// 实现原理：使用临时代码库、本地 HashEmbedder、本地 VectorStore 和 SnapshotStore，验证文件增删改后只更新变化文件统计。
// 使用方式：执行 go test ./internal/indexer 或 go test ./...。
// 注意事项：测试只读写临时目录，不连接外部向量数据库。
// 交互模块：internal/indexer、internal/scanner、internal/splitter、internal/embed、internal/vectorstore、internal/snapshot。

package indexer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestIndexerIndexSkipsUnchangedFiles(t *testing.T) {
	ctx := context.Background()
	repoPath := t.TempDir()
	storagePath := t.TempDir()
	filePath := filepath.Join(repoPath, "a.go")
	content := "package main\n\nfunc A() {}\n"
	writeIndexerTestFile(t, filePath, content)

	countingEmbedder := newCountingEmbedder(8)
	indexer := New(
		scanner.New([]string{".go"}, nil),
		splitter.NewLineSplitter(20, 0),
		countingEmbedder,
		vectorstore.NewLocalStore(storagePath),
		snapshot.NewStore(storagePath),
	)
	firstStats, err := indexer.Index(ctx, repoPath)
	if err != nil {
		t.Fatalf("Index() first error = %v", err)
	}
	if !firstStats.FullReindex || countingEmbedder.batchCalls != 1 {
		t.Fatalf("first stats = %+v, batchCalls = %d, want full reindex and 1 embed batch", firstStats, countingEmbedder.batchCalls)
	}

	secondStats, err := indexer.Index(ctx, repoPath)
	if err != nil {
		t.Fatalf("Index() second error = %v", err)
	}
	if secondStats.AddedFiles != 0 || secondStats.ModifiedFiles != 0 || secondStats.RemovedFiles != 0 {
		t.Fatalf("second stats changes = %+v, want no changes", secondStats)
	}
	if countingEmbedder.batchCalls != 1 {
		t.Fatalf("batchCalls after no-change index = %d, want 1", countingEmbedder.batchCalls)
	}

	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filePath, future, future); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}
	thirdStats, err := indexer.Index(ctx, repoPath)
	if err != nil {
		t.Fatalf("Index() third error = %v", err)
	}
	if thirdStats.AddedFiles != 0 || thirdStats.ModifiedFiles != 0 || thirdStats.RemovedFiles != 0 {
		t.Fatalf("third stats changes = %+v, want no content changes", thirdStats)
	}
	if countingEmbedder.batchCalls != 1 {
		t.Fatalf("batchCalls after same-content metadata change = %d, want 1", countingEmbedder.batchCalls)
	}
}

func TestIndexerIndexMigratesHashSnapshotToFileStates(t *testing.T) {
	ctx := context.Background()
	repoPath := t.TempDir()
	storagePath := t.TempDir()
	writeIndexerTestFile(t, filepath.Join(repoPath, "a.go"), "package main\n\nfunc A() {}\n")

	snapshotStore := snapshot.NewStore(storagePath)
	indexer := New(
		scanner.New([]string{".go"}, nil),
		splitter.NewLineSplitter(20, 0),
		embed.NewHashEmbedder(8),
		vectorstore.NewLocalStore(storagePath),
		snapshotStore,
	)
	firstStats, err := indexer.Index(ctx, repoPath)
	if err != nil {
		t.Fatalf("Index() first error = %v", err)
	}
	info, err := snapshotStore.Get(firstStats.Namespace)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	info.FileStates = nil
	if err := snapshotStore.Save(info); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	secondStats, err := indexer.Index(ctx, repoPath)
	if err != nil {
		t.Fatalf("Index() second error = %v", err)
	}
	if secondStats.FullReindex || secondStats.AddedFiles != 0 || secondStats.ModifiedFiles != 0 || secondStats.RemovedFiles != 0 {
		t.Fatalf("second stats = %+v, want incremental no changes", secondStats)
	}
	info, err = snapshotStore.Get(firstStats.Namespace)
	if err != nil {
		t.Fatalf("Get() migrated error = %v", err)
	}
	if len(info.FileStates) != 1 || info.FileStates["a.go"].Hash == "" {
		t.Fatalf("FileStates = %+v, want migrated state for a.go", info.FileStates)
	}
}

func TestIndexerIndexReturnsEmbeddingErrorAndMarksFailed(t *testing.T) {
	ctx := context.Background()
	repoPath := t.TempDir()
	storagePath := t.TempDir()
	writeIndexerTestFile(t, filepath.Join(repoPath, "a.go"), "package main\n\nfunc A() {}\n")

	snapshotStore := snapshot.NewStore(storagePath)
	indexer := New(
		scanner.New([]string{".go"}, nil),
		splitter.NewLineSplitter(20, 0),
		&failingEmbedder{err: errors.New("embedding service unavailable")},
		vectorstore.NewLocalStore(storagePath),
		snapshotStore,
	)
	_, err := indexer.Index(ctx, repoPath)
	if err == nil {
		t.Fatal("Index() error = nil, want embedding error")
	}
	if !strings.Contains(err.Error(), "embed file chunks failed") || !strings.Contains(err.Error(), "embedding service unavailable") {
		t.Fatalf("Index() error = %v, want embedding context", err)
	}
	namespace, _, err := NamespaceForPath(repoPath)
	if err != nil {
		t.Fatalf("NamespaceForPath() error = %v", err)
	}
	info, err := snapshotStore.Get(namespace)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if info.Status != snapshot.StatusFailed {
		t.Fatalf("status = %s, want failed", info.Status)
	}
	if !strings.Contains(info.ErrorMessage, "embedding service unavailable") {
		t.Fatalf("ErrorMessage = %q, want embedding service error", info.ErrorMessage)
	}
}

type countingEmbedder struct {
	inner      *embed.HashEmbedder
	batchCalls int
}

func newCountingEmbedder(dimension int) *countingEmbedder {
	return &countingEmbedder{inner: embed.NewHashEmbedder(dimension)}
}

func (e *countingEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return e.inner.Embed(ctx, text)
}

func (e *countingEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	e.batchCalls++
	return e.inner.EmbedBatch(ctx, texts)
}

func (e *countingEmbedder) Dimension() int {
	return e.inner.Dimension()
}

func (e *countingEmbedder) Provider() string {
	return e.inner.Provider()
}

type failingEmbedder struct {
	err error
}

func (e *failingEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, e.err
}

func (e *failingEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, e.err
}

func (e *failingEmbedder) Dimension() int {
	return 0
}

func (e *failingEmbedder) Provider() string {
	return "failing"
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
