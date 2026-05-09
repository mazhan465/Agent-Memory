// 文件说明：提供 Markdown 文档源读取能力。
// 实现原理：遍历指定路径下的 Markdown 文件，读取内容后交给文档 Parser 解析为统一节点。
// 使用方式：后续 DocumentSource 索引流程可调用 MarkdownDocumentSource.Read，获得待向量化的文档节点。
// 注意事项：当前只接入 Markdown 解析器，暂不实现 PDF、DOCX 等其他解析器。
// 交互模块：internal/document、internal/contextdoc、internal/indexer。

// Package source 抽象不同数据源的读取能力。
package source

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
	"github.com/mazhan465/Agent-Memory/internal/document"
)

// DocumentItem 表示从文档源读取并解析后的单个文档。
type DocumentItem struct {
	Scope        contextdoc.Scope
	Source       contextdoc.Source
	AbsolutePath string
	RelativePath string
	Nodes        []document.Node
}

// MarkdownDocumentSource 读取 Markdown 文档并解析为文档节点。
type MarkdownDocumentSource struct {
	RootPath string
	Scope    contextdoc.Scope
	Source   contextdoc.Source
	Parser   document.Parser
}

// NewMarkdownDocumentSource 创建 Markdown 文档源。
func NewMarkdownDocumentSource(rootPath string, scope contextdoc.Scope, source contextdoc.Source) *MarkdownDocumentSource {
	return &MarkdownDocumentSource{
		RootPath: rootPath,
		Scope:    scope,
		Source:   source,
		Parser:   document.NewMarkdownParser(),
	}
}

// Read 读取并解析 Markdown 文档源。
func (s *MarkdownDocumentSource) Read(ctx context.Context) ([]DocumentItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.Scope.Validate(); err != nil {
		return nil, err
	}
	if err := s.Source.Validate(); err != nil {
		return nil, err
	}
	if s.Source.Type != contextdoc.SourceTypeDocument && s.Source.Type != contextdoc.SourceTypeExternalKnowledge {
		return nil, errors.New("source type is not document knowledge")
	}
	parser := s.Parser
	if parser == nil {
		parser = document.NewMarkdownParser()
	}

	absoluteRoot, err := filepath.Abs(s.RootPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absoluteRoot)
	if err != nil {
		return nil, err
	}
	paths, err := markdownPaths(absoluteRoot, info)
	if err != nil {
		return nil, err
	}

	items := make([]DocumentItem, 0, len(paths))
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		nodes, err := parser.Parse(path, data)
		if err != nil {
			return nil, err
		}
		relativePath, err := filepath.Rel(absoluteRoot, path)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			relativePath = filepath.Base(path)
		}
		items = append(items, DocumentItem{
			Scope:        s.Scope,
			Source:       s.Source,
			AbsolutePath: filepath.ToSlash(path),
			RelativePath: filepath.ToSlash(relativePath),
			Nodes:        nodes,
		})
	}
	return items, nil
}

func markdownPaths(root string, info os.FileInfo) ([]string, error) {
	if !info.IsDir() {
		if isMarkdownPath(root) {
			return []string{root}, nil
		}
		return nil, errors.New("document source file is not markdown")
	}
	paths := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if shouldSkipDocumentDir(entry.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if isMarkdownPath(path) {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func isMarkdownPath(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return extension == ".md" || extension == ".markdown"
}

func shouldSkipDocumentDir(name string) bool {
	switch name {
	case ".git", "node_modules", "dist", "build", ".codebuddy":
		return true
	default:
		return false
	}
}
