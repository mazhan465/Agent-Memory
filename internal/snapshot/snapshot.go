// 文件说明：管理代码库索引状态快照。
// 实现原理：每个 namespace 对应一个 JSON 快照文件，记录路径、状态、文件数、chunk 数和更新时间。
// 使用方式：索引器完成或失败时写入快照，搜索器和 CLI status 读取快照展示状态。
// 注意事项：快照仅表示本地状态，后续接入 Milvus 后需要和远端 collection 状态做一致性校验。
// 交互模块：internal/indexer、internal/searcher、cmd/code-context。

// Package snapshot 提供索引状态持久化能力。
package snapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const snapshotDirName = "snapshots"

// Status 表示索引状态。
type Status string

const (
	// StatusIndexing 表示正在索引。
	StatusIndexing Status = "indexing"
	// StatusIndexed 表示索引成功。
	StatusIndexed Status = "indexed"
	// StatusFailed 表示索引失败。
	StatusFailed Status = "failed"
)

// Info 表示一个代码库的索引快照。
type Info struct {
	Path         string    `json:"path"`
	Namespace    string    `json:"namespace"`
	Status       Status    `json:"status"`
	IndexedFiles int       `json:"indexed_files"`
	TotalChunks  int       `json:"total_chunks"`
	ErrorMessage string    `json:"error_message,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Store 管理快照读写。
type Store struct {
	storageDir string
}

// NewStore 创建快照存储。
func NewStore(storageDir string) *Store {
	return &Store{storageDir: storageDir}
}

// Save 保存快照信息。
func (s *Store) Save(info Info) error {
	if err := os.MkdirAll(s.snapshotDir(), 0755); err != nil {
		return err
	}
	info.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.snapshotPath(info.Namespace), data, 0644)
}

// Get 读取指定 namespace 的快照信息。
func (s *Store) Get(namespace string) (Info, error) {
	data, err := os.ReadFile(s.snapshotPath(namespace))
	if err != nil {
		return Info{}, err
	}
	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return Info{}, err
	}
	return info, nil
}

// Delete 删除指定 namespace 的快照信息。
func (s *Store) Delete(namespace string) error {
	err := os.Remove(s.snapshotPath(namespace))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *Store) snapshotDir() string {
	return filepath.Join(s.storageDir, snapshotDirName)
}

func (s *Store) snapshotPath(namespace string) string {
	return filepath.Join(s.snapshotDir(), namespace+".json")
}
