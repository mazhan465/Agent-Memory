// 文件说明：编排代码库索引流程。
// 实现原理：扫描文件、读取内容、切块、批量向量化，最后写入 VectorStore 并保存 Snapshot。
// 使用方式：CLI index 命令创建 Indexer 后调用 Index 方法。
// 注意事项：当前索引是全量覆盖写入；后续会扩展为基于文件 hash 的增量索引。
// 交互模块：internal/scanner、internal/splitter、internal/embed、internal/vectorstore、internal/snapshot。

package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mazhan465/Agent-Memory/internal/domain"
	"github.com/mazhan465/Agent-Memory/internal/embed"
	"github.com/mazhan465/Agent-Memory/internal/scanner"
	"github.com/mazhan465/Agent-Memory/internal/snapshot"
	"github.com/mazhan465/Agent-Memory/internal/splitter"
	"github.com/mazhan465/Agent-Memory/internal/vectorstore"
)

// Stats 表示索引结果统计。
type Stats struct {
	Namespace    string
	Path         string
	IndexedFiles int
	TotalChunks  int
}

// Indexer 负责执行完整索引流程。
type Indexer struct {
	scanner       *scanner.Scanner
	splitter      splitter.Splitter
	embedder      embed.Embedder
	vectorStore   vectorstore.VectorStore
	snapshotStore *snapshot.Store
}

// New 创建索引器。
func New(
	scanner *scanner.Scanner,
	splitter splitter.Splitter,
	embedder embed.Embedder,
	vectorStore vectorstore.VectorStore,
	snapshotStore *snapshot.Store,
) *Indexer {
	return &Indexer{
		scanner:       scanner,
		splitter:      splitter,
		embedder:      embedder,
		vectorStore:   vectorStore,
		snapshotStore: snapshotStore,
	}
}

// Index 全量索引指定代码库。
func (i *Indexer) Index(ctx context.Context, rootPath string) (Stats, error) {
	namespace, absolutePath, err := NamespaceForPath(rootPath)
	if err != nil {
		return Stats{}, err
	}

	if err := i.snapshotStore.Save(snapshot.Info{
		Path:      absolutePath,
		Namespace: namespace,
		Status:    snapshot.StatusIndexing,
	}); err != nil {
		return Stats{}, err
	}

	files, err := i.scanner.Scan(absolutePath)
	if err != nil {
		i.saveFailure(namespace, absolutePath, err)
		return Stats{}, err
	}

	domainResolver := domain.NewDefaultDomainResolver()
	documents := make([]vectorstore.Document, 0)
	indexedFiles := 0
	for _, file := range files {
		content, err := os.ReadFile(file.AbsolutePath)
		if err != nil {
			continue
		}

		chunks := i.splitter.Split(file.AbsolutePath, content, languageFromExtension(file.Extension))
		if len(chunks) == 0 {
			continue
		}

		texts := make([]string, 0, len(chunks))
		for _, chunk := range chunks {
			texts = append(texts, chunk.Content)
		}
		vectors, err := i.embedder.EmbedBatch(ctx, texts)
		if err != nil {
			i.saveFailure(namespace, absolutePath, err)
			return Stats{}, err
		}

		for chunkIndex, chunk := range chunks {
			metadata := map[string]string{
				"absolute_path": filepath.ToSlash(file.AbsolutePath),
			}
			maps.Copy(metadata, chunk.Metadata)
			decision, err := domainResolver.Resolve(ctx, domain.Input{
				Query:          chunk.Content,
				FileExtensions: []string{file.Extension},
				Metadata: map[string]string{
					"relative_path": file.RelativePath,
				},
			})
			if err != nil {
				i.saveFailure(namespace, absolutePath, err)
				return Stats{}, err
			}
			if decision.Domain != "" {
				metadata[vectorstore.MetadataDomainPath] = string(decision.Domain)
			}
			if decision.ExperienceKind != "" {
				metadata[vectorstore.MetadataExperienceKind] = string(decision.ExperienceKind)
			}
			documents = append(documents, vectorstore.Document{
				ID:            chunkID(file.RelativePath, chunk.StartLine, chunk.EndLine, chunk.Content),
				Namespace:     namespace,
				Vector:        vectors[chunkIndex],
				Content:       chunk.Content,
				RelativePath:  file.RelativePath,
				StartLine:     chunk.StartLine,
				EndLine:       chunk.EndLine,
				FileExtension: file.Extension,
				Language:      chunk.Language,
				Metadata:      metadata,
			})
		}
		indexedFiles++
	}

	if err := i.vectorStore.Put(ctx, namespace, documents); err != nil {
		i.saveFailure(namespace, absolutePath, err)
		return Stats{}, err
	}

	info := snapshot.Info{
		Path:         absolutePath,
		Namespace:    namespace,
		Status:       snapshot.StatusIndexed,
		IndexedFiles: indexedFiles,
		TotalChunks:  len(documents),
	}
	if err := i.snapshotStore.Save(info); err != nil {
		return Stats{}, err
	}

	return Stats{
		Namespace:    namespace,
		Path:         absolutePath,
		IndexedFiles: indexedFiles,
		TotalChunks:  len(documents),
	}, nil
}

func (i *Indexer) saveFailure(namespace string, absolutePath string, err error) {
	_ = i.snapshotStore.Save(snapshot.Info{
		Path:         absolutePath,
		Namespace:    namespace,
		Status:       snapshot.StatusFailed,
		ErrorMessage: err.Error(),
	})
}

func chunkID(relativePath string, startLine int, endLine int, content string) string {
	key := relativePath + ":" + strconv.Itoa(startLine) + ":" + strconv.Itoa(endLine) + ":" + content
	hash := sha256.Sum256([]byte(key))
	return "chunk_" + hex.EncodeToString(hash[:])[:16]
}

func languageFromExtension(extension string) string {
	switch extension {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".md", ".markdown":
		return "markdown"
	default:
		return "text"
	}
}
