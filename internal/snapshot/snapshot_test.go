// 文件说明：测试索引状态快照存储。
// 实现原理：使用临时目录保存多份 snapshot，验证 List 稳定排序和空目录行为。
// 使用方式：执行 go test ./internal/snapshot 或 go test ./...。
// 注意事项：测试只读写临时目录。
// 交互模块：internal/snapshot。

package snapshot

import "testing"

func TestStoreList(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Save(Info{Path: "/repo/b", Namespace: "code_chunks_b", Status: StatusIndexed}); err != nil {
		t.Fatalf("Save() b error = %v", err)
	}
	if err := store.Save(Info{Path: "/repo/a", Namespace: "code_chunks_a", Status: StatusIndexed}); err != nil {
		t.Fatalf("Save() a error = %v", err)
	}

	items, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].Path != "/repo/a" || items[1].Path != "/repo/b" {
		t.Fatalf("items are not sorted by path: %+v", items)
	}
}

func TestStoreListEmpty(t *testing.T) {
	store := NewStore(t.TempDir())
	items, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("len(items) = %d, want 0", len(items))
	}
}
