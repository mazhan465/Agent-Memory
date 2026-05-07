// 文件说明：提供 go-code-context 的 CLI 入口。
// 实现原理：解析 index、search、status、clear 子命令，组装配置、索引器、搜索器和本地向量存储完成操作。
// 使用方式：执行 code-context index/search/status/clear 操作指定代码库索引。
// 注意事项：当前默认使用本地 HashEmbedder 和 LocalStore，搜索效果用于验证链路而非最终语义质量。
// 交互模块：internal/config、internal/scanner、internal/splitter、internal/embed、internal/vectorstore、internal/indexer、internal/searcher、internal/snapshot。

// Package main 提供 code-context 命令入口。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/aaq/go-code-context/internal/config"
	"github.com/aaq/go-code-context/internal/embed"
	"github.com/aaq/go-code-context/internal/indexer"
	"github.com/aaq/go-code-context/internal/scanner"
	"github.com/aaq/go-code-context/internal/searcher"
	"github.com/aaq/go-code-context/internal/snapshot"
	"github.com/aaq/go-code-context/internal/splitter"
	"github.com/aaq/go-code-context/internal/vectorstore"
)

const (
	exitCodeOK    = 0
	exitCodeError = 1
)

type app struct {
	config        config.Config
	embedder      embed.Embedder
	vectorStore   vectorstore.VectorStore
	snapshotStore *snapshot.Store
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitCodeError)
	}
	os.Exit(exitCodeOK)
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	application := newApp(cfg)

	switch args[0] {
	case "index":
		return application.runIndex(ctx, args[1:])
	case "search":
		return application.runSearch(ctx, args[1:])
	case "status":
		return application.runStatus(args[1:])
	case "clear":
		return application.runClear(ctx, args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func newApp(cfg config.Config) *app {
	return &app{
		config:        cfg,
		embedder:      embed.NewHashEmbedder(cfg.EmbeddingDimension),
		vectorStore:   vectorstore.NewLocalStore(cfg.StorageDir),
		snapshotStore: snapshot.NewStore(cfg.StorageDir),
	}
}

func (a *app) runIndex(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: code-context index <path>")
	}

	scannerInstance := scanner.New(a.config.SupportedExts, a.config.IgnoreNames)
	splitterInstance := splitter.NewLineSplitter(a.config.MaxChunkLines, a.config.ChunkOverlapLines)
	indexerInstance := indexer.New(scannerInstance, splitterInstance, a.embedder, a.vectorStore, a.snapshotStore)
	stats, err := indexerInstance.Index(ctx, args[0])
	if err != nil {
		return err
	}

	fmt.Printf("indexed path=%s namespace=%s files=%d chunks=%d\n", stats.Path, stats.Namespace, stats.IndexedFiles, stats.TotalChunks)
	return nil
}

func (a *app) runSearch(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return errors.New("usage: code-context search <path> <query> [limit]")
	}

	limit := a.config.SearchLimit
	if len(args) >= 3 {
		parsedLimit, err := strconv.Atoi(args[2])
		if err != nil || parsedLimit <= 0 {
			return errors.New("limit must be a positive integer")
		}
		limit = parsedLimit
	}

	searcherInstance := searcher.New(a.embedder, a.vectorStore)
	results, err := searcherInstance.Search(ctx, args[0], args[1], vectorstore.SearchOptions{Limit: limit})
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Println("no results")
		return nil
	}

	for index, result := range results {
		printResult(index+1, result)
	}
	return nil
}

func (a *app) runStatus(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: code-context status <path>")
	}

	namespace, absolutePath, err := indexer.NamespaceForPath(args[0])
	if err != nil {
		return err
	}
	info, err := a.snapshotStore.Get(namespace)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("path=%s namespace=%s status=not_indexed\n", absolutePath, namespace)
			return nil
		}
		return err
	}

	fmt.Printf("path=%s namespace=%s status=%s files=%d chunks=%d updated_at=%s\n",
		info.Path,
		info.Namespace,
		info.Status,
		info.IndexedFiles,
		info.TotalChunks,
		info.UpdatedAt.Format("2006-01-02 15:04:05"),
	)
	if info.ErrorMessage != "" {
		fmt.Printf("error=%s\n", info.ErrorMessage)
	}
	return nil
}

func (a *app) runClear(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: code-context clear <path>")
	}

	namespace, absolutePath, err := indexer.NamespaceForPath(args[0])
	if err != nil {
		return err
	}
	if err := a.vectorStore.Clear(ctx, namespace); err != nil {
		return err
	}
	if err := a.snapshotStore.Delete(namespace); err != nil {
		return err
	}
	fmt.Printf("cleared path=%s namespace=%s\n", absolutePath, namespace)
	return nil
}

func printResult(index int, result vectorstore.SearchResult) {
	location := fmt.Sprintf("%s:%d-%d", result.Document.RelativePath, result.Document.StartLine, result.Document.EndLine)
	preview := strings.TrimSpace(result.Document.Content)
	if len(preview) > 500 {
		preview = preview[:500] + "..."
	}
	fmt.Printf("%d. score=%.4f location=%s language=%s\n", index, result.Score, location, result.Document.Language)
	fmt.Printf("```%s\n%s\n```\n", result.Document.Language, preview)
}

func printUsage() {
	fmt.Println(`Usage:
  code-context index <path>
  code-context search <path> <query> [limit]
  code-context status <path>
  code-context clear <path>

Examples:
  code-context index .
  code-context search . "vector database operations" 5`)
}
