//go:build !cgo

// 文件说明：提供无 cgo 环境下的 tree-sitter 切块器降级实现。
// 实现原理：tree-sitter Go grammar 依赖 cgo；当 cgo 不可用时返回 fallback splitter，保证交叉编译可用。
// 使用方式：调用 NewTreeSitterSplitter 的代码无需感知 cgo 是否启用。
// 注意事项：无 cgo 构建不具备语法级切块能力，会退化为行级切块。
// 交互模块：cmd/code-context、internal/indexer、internal/splitter。

package splitter

// NewTreeSitterSplitter 在无 cgo 环境下返回 fallback 切块器。
func NewTreeSitterSplitter(maxLines int, overlapLines int, fallback Splitter) Splitter {
	if fallback != nil {
		return fallback
	}
	return NewLineSplitter(maxLines, overlapLines)
}
