// 文件说明：提供已导入 source 的查看和清理命令。
// 实现原理：通过 SourceCatalog 查询 source 元信息，并按 source namespace 清理 VectorStore 数据和 catalog 记录。
// 使用方式：runSource 由 CLI source 命令调用，支持 list 和 clear 子命令。
// 注意事项：clear 会删除匹配 source 的向量数据和 catalog 元信息，但不会删除原始文件。
// 交互模块：internal/catalog、internal/contextdoc、internal/vectorstore。

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mazhan465/Agent-Memory/internal/catalog"
	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
)

type sourceListResponse struct {
	Count   int             `json:"count"`
	Sources []catalog.Entry `json:"sources"`
}

type sourceClearResponse struct {
	SourceType string   `json:"source_type"`
	SourceID   string   `json:"source_id"`
	Cleared    int      `json:"cleared"`
	Namespaces []string `json:"namespaces"`
}

func (a *app) runSource(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: code-context source <list|clear> ...")
	}
	switch args[0] {
	case "list":
		return a.runSourceList(ctx, args[1:])
	case "clear":
		return a.runSourceClear(ctx, args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown source command %q", args[0])
	}
}

func (a *app) runSourceList(ctx context.Context, args []string) error {
	if len(args) > 1 {
		return errors.New("usage: code-context source list [type]")
	}
	filter := catalog.Filter{}
	if len(args) == 1 {
		sourceType, err := sourceTypeFromArg(args[0])
		if err != nil {
			return err
		}
		filter.SourceType = sourceType
	}
	entries, err := a.catalogStore.List(ctx, filter)
	if err != nil {
		return err
	}
	return printJSON(sourceListResponse{Count: len(entries), Sources: entries})
}

func (a *app) runSourceClear(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: code-context source clear <type> <source-id>")
	}
	sourceType, err := sourceTypeFromArg(args[0])
	if err != nil {
		return err
	}
	entries, err := a.catalogStore.List(ctx, catalog.Filter{SourceType: sourceType, SourceID: args[1]})
	if err != nil {
		return err
	}
	clearedNamespaces := make([]string, 0, len(entries))
	for _, entry := range entries {
		namespace, err := contextdoc.NamespaceForSource(entry.Scope, entry.Source)
		if err != nil {
			return err
		}
		if err := a.vectorStore.Clear(ctx, namespace); err != nil {
			return err
		}
		if err := a.catalogStore.Delete(ctx, entry.Scope, entry.Source); err != nil {
			return err
		}
		clearedNamespaces = append(clearedNamespaces, namespace)
	}
	return printJSON(sourceClearResponse{
		SourceType: string(sourceType),
		SourceID:   args[1],
		Cleared:    len(clearedNamespaces),
		Namespaces: clearedNamespaces,
	})
}

func sourceTypeFromArg(value string) (contextdoc.SourceType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "knowledge", "document", "documents", "docs":
		return contextdoc.SourceTypeDocument, nil
	case "external_knowledge", "external-knowledge":
		return contextdoc.SourceTypeExternalKnowledge, nil
	case "conversation", "history", "chat":
		return contextdoc.SourceTypeConversation, nil
	case "experience", "experiences":
		return contextdoc.SourceTypeExperience, nil
	case "preference", "preferences", "user_preference":
		return contextdoc.SourceTypePreference, nil
	case "tool_history", "tool", "tool-history":
		return contextdoc.SourceTypeToolHistory, nil
	case "fact", "facts":
		return contextdoc.SourceTypeFact, nil
	default:
		return "", fmt.Errorf("unsupported source type %q", value)
	}
}
