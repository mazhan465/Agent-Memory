// 文件说明：扫描代码库中的可索引文件。
// 实现原理：基于 filepath.WalkDir 递归遍历目录，按扩展名白名单、忽略名单、glob 忽略规则和根目录 ignore 文件过滤文件。
// 使用方式：索引器调用 Scanner.Scan 获取待切块和向量化的文件列表。
// 注意事项：默认忽略依赖、构建产物、版本控制、缓存、日志和环境配置目录，避免索引无关或敏感内容。
// 交互模块：internal/config、internal/indexer、internal/splitter。

// Package scanner 提供代码库文件扫描能力。
package scanner

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// File 表示一个可索引文件。
type File struct {
	AbsolutePath    string
	RelativePath    string
	Extension       string
	Size            int64
	ModTimeUnixNano int64
}

// Scanner 负责根据扩展名和忽略规则扫描文件。
type Scanner struct {
	supportedExts  map[string]struct{}
	ignoreNames    map[string]struct{}
	ignorePatterns []string
}

// New 创建 Scanner。
func New(supportedExts []string, ignoreNames []string) *Scanner {
	return NewWithPatterns(supportedExts, ignoreNames, nil)
}

// NewWithPatterns 创建带 glob 忽略规则的 Scanner。
func NewWithPatterns(supportedExts []string, ignoreNames []string, ignorePatterns []string) *Scanner {
	return &Scanner{
		supportedExts:  toSet(supportedExts),
		ignoreNames:    toSet(ignoreNames),
		ignorePatterns: normalizePatterns(ignorePatterns),
	}
}

// Scan 扫描 rootPath 下的可索引文件。
func (s *Scanner) Scan(rootPath string) ([]File, error) {
	absoluteRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, err
	}

	ignorePatterns := append([]string(nil), s.ignorePatterns...)
	filePatterns, err := loadRootIgnorePatterns(absoluteRoot)
	if err != nil {
		return nil, err
	}
	ignorePatterns = append(ignorePatterns, filePatterns...)
	ignorePatterns = normalizePatterns(ignorePatterns)

	files := make([]File, 0)
	walkFn := func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		name := entry.Name()
		relativePath, err := filepath.Rel(absoluteRoot, currentPath)
		if err != nil {
			return err
		}
		relativePath = filepath.ToSlash(relativePath)

		if entry.IsDir() {
			if currentPath != absoluteRoot && s.shouldIgnore(name, relativePath, ignorePatterns) {
				return filepath.SkipDir
			}
			return nil
		}

		if s.shouldIgnore(name, relativePath, ignorePatterns) || !entry.Type().IsRegular() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(name))
		if _, ok := s.supportedExts[ext]; !ok {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}

		files = append(files, File{
			AbsolutePath:    currentPath,
			RelativePath:    relativePath,
			Extension:       ext,
			Size:            info.Size(),
			ModTimeUnixNano: info.ModTime().UnixNano(),
		})
		return nil
	}

	if err := filepath.WalkDir(absoluteRoot, walkFn); err != nil {
		return nil, err
	}
	sort.SliceStable(files, func(i int, j int) bool {
		return files[i].RelativePath < files[j].RelativePath
	})
	return files, nil
}

func (s *Scanner) shouldIgnore(name string, relativePath string, ignorePatterns []string) bool {
	if strings.HasPrefix(name, ".") && name != "." {
		if _, ok := s.supportedExts[strings.ToLower(filepath.Ext(name))]; !ok {
			return true
		}
	}
	if _, ok := s.ignoreNames[name]; ok {
		return true
	}
	return matchAnyPattern(relativePath, ignorePatterns)
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

func loadRootIgnorePatterns(rootPath string) ([]string, error) {
	entries, err := os.ReadDir(rootPath)
	if err != nil {
		return nil, err
	}
	patterns := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || !isIgnoreFile(entry.Name()) {
			continue
		}
		filePatterns, err := readIgnoreFile(filepath.Join(rootPath, entry.Name()))
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, filePatterns...)
	}
	return patterns, nil
}

func isIgnoreFile(name string) bool {
	return strings.HasPrefix(name, ".") && strings.HasSuffix(name, "ignore")
}

func readIgnoreFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	patterns := make([]string, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		patterns = append(patterns, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return patterns, nil
}

func normalizePatterns(patterns []string) []string {
	seen := make(map[string]struct{}, len(patterns))
	result := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		cleanPattern := filepath.ToSlash(strings.TrimSpace(pattern))
		cleanPattern = strings.TrimPrefix(cleanPattern, "./")
		if cleanPattern == "" || strings.HasPrefix(cleanPattern, "#") || strings.HasPrefix(cleanPattern, "!") {
			continue
		}
		if _, ok := seen[cleanPattern]; ok {
			continue
		}
		seen[cleanPattern] = struct{}{}
		result = append(result, cleanPattern)
	}
	return result
}

func matchAnyPattern(relativePath string, patterns []string) bool {
	cleanPath := filepath.ToSlash(strings.Trim(relativePath, "/"))
	if cleanPath == "" || cleanPath == "." {
		return false
	}
	for _, pattern := range patterns {
		if matchPattern(cleanPath, pattern) {
			return true
		}
	}
	return false
}

func matchPattern(relativePath string, pattern string) bool {
	cleanPattern := filepath.ToSlash(strings.TrimSpace(pattern))
	if cleanPattern == "" {
		return false
	}
	rootAnchored := strings.HasPrefix(cleanPattern, "/")
	cleanPattern = strings.Trim(cleanPattern, "/")
	if cleanPattern == "" {
		return false
	}
	if basePattern, ok := strings.CutSuffix(cleanPattern, "/**"); ok {
		return matchPathOrDescendant(relativePath, basePattern, rootAnchored)
	}
	if basePattern, ok := strings.CutSuffix(cleanPattern, "/"); ok {
		return matchPathOrDescendant(relativePath, basePattern, rootAnchored)
	}
	if !strings.Contains(cleanPattern, "/") && !rootAnchored {
		return matchGlob(filepath.Base(relativePath), cleanPattern) || pathSegmentMatches(relativePath, cleanPattern)
	}
	if rootAnchored {
		return matchGlob(relativePath, cleanPattern)
	}
	return matchGlob(relativePath, cleanPattern) || pathSegmentMatches(relativePath, cleanPattern)
}

func matchPathOrDescendant(relativePath string, pattern string, rootAnchored bool) bool {
	if rootAnchored {
		return matchGlob(relativePath, pattern) || strings.HasPrefix(relativePath, pattern+"/")
	}
	parts := strings.Split(relativePath, "/")
	for start := range parts {
		candidate := strings.Join(parts[start:], "/")
		if matchGlob(candidate, pattern) || strings.HasPrefix(candidate, pattern+"/") {
			return true
		}
	}
	return false
}

func pathSegmentMatches(relativePath string, pattern string) bool {
	for part := range strings.SplitSeq(relativePath, "/") {
		if matchGlob(part, pattern) {
			return true
		}
	}
	return false
}

func matchGlob(value string, pattern string) bool {
	regex := globToRegexp(pattern)
	return regex.MatchString(value)
}

func globToRegexp(pattern string) *regexp.Regexp {
	var builder strings.Builder
	builder.WriteString("^")
	for _, char := range pattern {
		switch char {
		case '*':
			builder.WriteString(".*")
		case '?':
			builder.WriteByte('.')
		default:
			builder.WriteString(regexp.QuoteMeta(string(char)))
		}
	}
	builder.WriteString("$")
	return regexp.MustCompile(builder.String())
}
