// 文件说明：提供 Agent-Memory 的 CLI 入口。
// 实现原理：解析 index、search、import、status、clear 子命令，组装配置、索引器、搜索器和向量存储完成操作。
// 使用方式：执行 code-context index/search/import/status/clear 操作指定上下文源。
// 注意事项：默认使用本地 HashEmbedder 和 LocalStore，配置环境变量后可切换 OpenAI-compatible Embedder 和 Milvus VectorStore。
// 交互模块：internal/config、internal/scanner、internal/splitter、internal/embed、internal/vectorstore、internal/indexer、internal/snapshot。

// Package main 提供 code-context 命令入口。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mazhan465/Agent-Memory/internal/catalog"
	"github.com/mazhan465/Agent-Memory/internal/config"
	"github.com/mazhan465/Agent-Memory/internal/embed"
	"github.com/mazhan465/Agent-Memory/internal/indexer"
	"github.com/mazhan465/Agent-Memory/internal/scanner"
	"github.com/mazhan465/Agent-Memory/internal/snapshot"
	"github.com/mazhan465/Agent-Memory/internal/splitter"
	"github.com/mazhan465/Agent-Memory/internal/vectorstore"
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
	catalogStore  *catalog.Store
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
	application, err := newApp(ctx, cfg)
	if err != nil {
		return err
	}

	switch args[0] {
	case "index":
		return application.runIndex(ctx, args[1:])
	case "search":
		return application.runSearch(ctx, args[1:])
	case "import":
		return application.runImport(ctx, args[1:])
	case "source":
		return application.runSource(ctx, args[1:])
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

func newApp(ctx context.Context, cfg config.Config) (*app, error) {
	embedderInstance, err := newEmbedder(cfg)
	if err != nil {
		return nil, err
	}
	vectorStoreInstance, err := newVectorStore(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &app{
		config:        cfg,
		embedder:      embedderInstance,
		vectorStore:   vectorStoreInstance,
		snapshotStore: snapshot.NewStore(cfg.StorageDir),
		catalogStore:  catalog.NewStore(cfg.StorageDir),
	}, nil
}

func newEmbedder(cfg config.Config) (embed.Embedder, error) {
	switch strings.ToLower(cfg.EmbeddingProvider) {
	case "", "hash":
		return embed.NewHashEmbedder(cfg.EmbeddingDimension), nil
	case "openai", "openai-compatible":
		return embed.NewOpenAIEmbedder(embed.OpenAIOptions{
			BaseURL: cfg.OpenAIBaseURL,
			APIKey:  cfg.OpenAIAPIKey,
			Model:   cfg.OpenAIEmbeddingModel,
		})
	default:
		return nil, fmt.Errorf("unsupported embedding provider %q", cfg.EmbeddingProvider)
	}
}

func newVectorStore(ctx context.Context, cfg config.Config) (vectorstore.VectorStore, error) {
	switch strings.ToLower(cfg.VectorStoreProvider) {
	case "", "local":
		return vectorstore.NewLocalStore(cfg.StorageDir), nil
	case "milvus":
		return vectorstore.NewMilvusStore(ctx, vectorstore.MilvusOptions{
			Address:        cfg.MilvusAddress,
			Username:       cfg.MilvusUsername,
			Password:       cfg.MilvusPassword,
			CollectionName: cfg.MilvusCollection,
		})
	default:
		return nil, fmt.Errorf("unsupported vector store %q", cfg.VectorStoreProvider)
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

func printUsage() {
	fmt.Println(`Usage:
  code-context index <path>
  code-context search <path> <query> [limit] [types] [session-id]
  code-context import knowledge <path> [source-id]
  code-context import memory <type> <json-or-jsonl-path> [source-id]
  code-context source list [type]
  code-context source clear <type> <source-id>
  code-context status <path>
  code-context clear <path>

Search types:
  all, code, knowledge, conversation, experience, preference, tool_history, fact

Memory import types:
  conversation, experience, preference, tool_history, fact

Examples:
  code-context index .
  code-context search . "vector database operations" 5
  code-context search . "project rules" knowledge
  code-context search . "previous fix" 5 conversation,experience session-dev
  code-context search . "project rules" --session-id=session-dev
  code-context import knowledge ./docs project-docs
  code-context import memory experience ./memories.jsonl team-experience
  code-context source list knowledge`)
}
