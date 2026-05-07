// 文件说明：提供代码库路径到索引命名空间的转换逻辑。
// 实现原理：将代码库绝对路径做 MD5 哈希，生成稳定且较短的 namespace。
// 使用方式：索引、搜索、快照和向量存储模块通过 NamespaceForPath 获取同一代码库的命名空间。
// 注意事项：namespace 只用于隔离索引数据，不应作为安全边界。
// 交互模块：internal/indexer、internal/searcher、internal/snapshot、internal/vectorstore。

// Package indexer 编排代码库索引流程。
package indexer

import (
	"crypto/md5"
	"encoding/hex"
	"path/filepath"
)

const namespacePrefix = "code_chunks"

// NamespaceForPath 根据代码库路径生成稳定命名空间。
func NamespaceForPath(rootPath string) (string, string, error) {
	absolutePath, err := filepath.Abs(rootPath)
	if err != nil {
		return "", "", err
	}
	hash := md5.Sum([]byte(absolutePath))
	return namespacePrefix + "_" + hex.EncodeToString(hash[:])[:8], absolutePath, nil
}
