// 文件说明：实现 SourceCatalog 的本地 JSON 持久化。
// 实现原理：将所有 source 元信息保存到 catalog/sources.json，按 scope 和 source 生成稳定 key 进行 upsert、查询和删除。
// 使用方式：SourceStore、索引器和同步模块通过 Store 记录 source manifest、版本、checksum 和统计信息。
// 注意事项：Catalog 只保存 source 元信息，不保存向量正文；写入时使用临时文件替换，减少半写入风险。
// 交互模块：internal/contextdoc、internal/sourcestore、internal/indexer、internal/searcher。

// Package catalog 提供 SourceCatalog 本地持久化能力。
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
)

const (
	catalogDirName  = "catalog"
	catalogFileName = "sources.json"
)

// Status 表示 source 索引状态。
type Status string

const (
	// StatusIndexing 表示 source 正在索引。
	StatusIndexing Status = "indexing"
	// StatusIndexed 表示 source 已成功索引。
	StatusIndexed Status = "indexed"
	// StatusFailed 表示 source 索引失败。
	StatusFailed Status = "failed"
)

// Entry 表示 SourceCatalog 中的一条 source 元信息。
type Entry struct {
	Scope          contextdoc.Scope  `json:"scope"`
	Source         contextdoc.Source `json:"source"`
	Version        int64             `json:"version"`
	Checksum       string            `json:"checksum"`
	IndexedAt      time.Time         `json:"indexed_at"`
	Status         Status            `json:"status"`
	DocumentCount  int               `json:"document_count"`
	MemoryCount    int               `json:"memory_count"`
	KnowledgeCount int               `json:"knowledge_count"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// Validate 校验 Entry 的基础字段。
func (e Entry) Validate() error {
	if err := e.Scope.Validate(); err != nil {
		return err
	}
	if err := e.Source.Validate(); err != nil {
		return err
	}
	if !isSupportedStatus(e.Status) {
		return errors.New("unsupported catalog status")
	}
	if e.Version < 0 {
		return errors.New("catalog entry version is invalid")
	}
	if e.DocumentCount < 0 {
		return errors.New("catalog entry document count is invalid")
	}
	if e.MemoryCount < 0 {
		return errors.New("catalog entry memory count is invalid")
	}
	if e.KnowledgeCount < 0 {
		return errors.New("catalog entry knowledge count is invalid")
	}
	return nil
}

// Filter 表示 SourceCatalog 查询条件。
type Filter struct {
	Scope      contextdoc.Scope
	SourceType contextdoc.SourceType
	SourceID   string
	ToolName   string
	Status     Status
}

// Store 管理 SourceCatalog 本地文件。
type Store struct {
	storageDir string
}

// NewStore 创建 SourceCatalog 存储。
func NewStore(storageDir string) *Store {
	return &Store{storageDir: storageDir}
}

// EntryFromManifest 根据 source manifest 生成 catalog entry。
func EntryFromManifest(manifest contextdoc.Manifest, status Status) (Entry, error) {
	if err := manifest.Validate(); err != nil {
		return Entry{}, err
	}
	if status == "" {
		status = StatusIndexed
	}
	entry := Entry{
		Scope:          manifest.Scope,
		Source:         manifest.Source,
		Version:        manifest.Version,
		Checksum:       manifest.Checksum,
		IndexedAt:      manifest.IndexedAt,
		Status:         status,
		DocumentCount:  manifest.DocumentCount,
		MemoryCount:    manifest.MemoryCount,
		KnowledgeCount: manifest.KnowledgeCount,
		Metadata:       manifest.Metadata,
	}
	if entry.IndexedAt.IsZero() {
		entry.IndexedAt = time.Now()
	}
	if err := entry.Validate(); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

// Upsert 写入或替换一条 source 元信息。
func (s *Store) Upsert(ctx context.Context, entry Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if entry.IndexedAt.IsZero() {
		entry.IndexedAt = time.Now()
	}
	if err := entry.Validate(); err != nil {
		return err
	}

	entries, err := s.load()
	if err != nil {
		return err
	}
	entryKey := keyOf(entry.Scope, entry.Source)
	replaced := false
	for index := range entries {
		if keyOf(entries[index].Scope, entries[index].Source) == entryKey {
			entries[index] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		entries = append(entries, entry)
	}
	sortEntries(entries)
	return s.save(entries)
}

// UpsertManifest 根据 manifest 写入或替换一条 source 元信息。
func (s *Store) UpsertManifest(ctx context.Context, manifest contextdoc.Manifest, status Status) error {
	entry, err := EntryFromManifest(manifest, status)
	if err != nil {
		return err
	}
	return s.Upsert(ctx, entry)
}

// List 返回符合过滤条件的 source 元信息。
func (s *Store) List(ctx context.Context, filter Filter) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := s.load()
	if err != nil {
		return nil, err
	}
	result := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if matchFilter(entry, filter) {
			result = append(result, entry)
		}
	}
	return result, nil
}

// Get 读取指定 scope 和 source 的元信息。
func (s *Store) Get(ctx context.Context, scope contextdoc.Scope, source contextdoc.Source) (Entry, bool, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, false, err
	}
	if err := scope.Validate(); err != nil {
		return Entry{}, false, err
	}
	if err := source.Validate(); err != nil {
		return Entry{}, false, err
	}
	entries, err := s.load()
	if err != nil {
		return Entry{}, false, err
	}
	wantKey := keyOf(scope, source)
	for _, entry := range entries {
		if keyOf(entry.Scope, entry.Source) == wantKey {
			return entry, true, nil
		}
	}
	return Entry{}, false, nil
}

// Delete 删除指定 scope 和 source 的元信息。
func (s *Store) Delete(ctx context.Context, scope contextdoc.Scope, source contextdoc.Source) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if err := source.Validate(); err != nil {
		return err
	}
	entries, err := s.load()
	if err != nil {
		return err
	}
	wantKey := keyOf(scope, source)
	result := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if keyOf(entry.Scope, entry.Source) != wantKey {
			result = append(result, entry)
		}
	}
	if len(result) == len(entries) {
		return nil
	}
	return s.save(result)
}

func (s *Store) load() ([]Entry, error) {
	data, err := os.ReadFile(s.catalogPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []Entry{}, nil
		}
		return nil, err
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *Store) save(entries []Entry) error {
	if err := os.MkdirAll(s.catalogDir(), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tempPath := s.catalogPath() + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tempPath, s.catalogPath())
}

func (s *Store) catalogDir() string {
	return filepath.Join(s.storageDir, catalogDirName)
}

func (s *Store) catalogPath() string {
	return filepath.Join(s.catalogDir(), catalogFileName)
}

func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i int, j int) bool {
		return keyOf(entries[i].Scope, entries[i].Source) < keyOf(entries[j].Scope, entries[j].Source)
	})
}

func matchFilter(entry Entry, filter Filter) bool {
	if filter.Scope.Type != "" && entry.Scope.Type != filter.Scope.Type {
		return false
	}
	if strings.TrimSpace(filter.Scope.ID) != "" && entry.Scope.ID != filter.Scope.ID {
		return false
	}
	if filter.SourceType != "" && entry.Source.Type != filter.SourceType {
		return false
	}
	if strings.TrimSpace(filter.SourceID) != "" && entry.Source.ID != filter.SourceID {
		return false
	}
	if strings.TrimSpace(filter.ToolName) != "" && entry.Source.ToolName != filter.ToolName {
		return false
	}
	if filter.Status != "" && entry.Status != filter.Status {
		return false
	}
	return true
}

func keyOf(scope contextdoc.Scope, source contextdoc.Source) string {
	return strings.Join([]string{string(scope.Type), scope.ID, string(source.Type), source.ID}, "\x00")
}

func isSupportedStatus(status Status) bool {
	switch status {
	case StatusIndexing, StatusIndexed, StatusFailed:
		return true
	default:
		return false
	}
}
