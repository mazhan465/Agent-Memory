// 文件说明：扫描代码库中的可索引文件。
// 实现原理：基于 filepath.WalkDir 递归遍历目录，按扩展名白名单和忽略名单过滤文件。
// 使用方式：索引器调用 Scanner.Scan 获取待切块和向量化的文件列表。
// 注意事项：默认忽略依赖、构建产物、版本控制、缓存、日志和环境配置目录，避免索引无关或敏感内容。
// 交互模块：internal/config、internal/indexer、internal/splitter。

// Package scanner 提供代码库文件扫描能力。
package scanner

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// File 表示一个可索引文件。
type File struct {
	AbsolutePath string
	RelativePath string
	Extension    string
}

// Scanner 负责根据扩展名和忽略规则扫描文件。
type Scanner struct {
	supportedExts map[string]struct{}
	ignoreNames   map[string]struct{}
}

// New 创建 Scanner。
func New(supportedExts []string, ignoreNames []string) *Scanner {
	return &Scanner{
		supportedExts: toSet(supportedExts),
		ignoreNames:   toSet(ignoreNames),
	}
}

// Scan 扫描 rootPath 下的可索引文件。
func (s *Scanner) Scan(rootPath string) ([]File, error) {
	absoluteRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, err
	}

	files := make([]File, 0)
	walkFn := func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		name := entry.Name()
		if entry.IsDir() {
			if s.shouldIgnoreName(name) && currentPath != absoluteRoot {
				return filepath.SkipDir
			}
			return nil
		}

		if s.shouldIgnoreName(name) || !entry.Type().IsRegular() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(name))
		if _, ok := s.supportedExts[ext]; !ok {
			return nil
		}

		relativePath, err := filepath.Rel(absoluteRoot, currentPath)
		if err != nil {
			return err
		}
		files = append(files, File{
			AbsolutePath: currentPath,
			RelativePath: filepath.ToSlash(relativePath),
			Extension:    ext,
		})
		return nil
	}

	if err := filepath.WalkDir(absoluteRoot, walkFn); err != nil {
		return nil, err
	}
	return files, nil
}

func (s *Scanner) shouldIgnoreName(name string) bool {
	if strings.HasPrefix(name, ".") && name != "." {
		if _, ok := s.supportedExts[strings.ToLower(filepath.Ext(name))]; !ok {
			return true
		}
	}
	_, ok := s.ignoreNames[name]
	return ok
}

func toSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmedValue := strings.TrimSpace(value)
		if trimmedValue != "" {
			result[trimmedValue] = struct{}{}
		}
	}
	return result
}
