// 文件说明：定义项目运行配置和默认值。
// 实现原理：从环境变量和内置常量构造 Config，供 CLI、索引器、搜索器统一使用。
// 使用方式：调用 Load 获取配置，必要时通过 WithRootPath 设置目标代码库路径。
// 注意事项：本文件不读取敏感配置内容到日志，后续接入外部服务时也应避免输出密钥。
// 交互模块：cmd/code-context、internal/indexer、internal/searcher、internal/snapshot、internal/vectorstore。

// Package config 提供项目配置加载能力。
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultStorageDir     = ".go-code-context"
	defaultEmbeddingDim   = 256
	defaultMaxChunkLines  = 120
	defaultChunkOverlap   = 20
	defaultSearchLimit    = 8
	envStorageDir         = "GO_CODE_CONTEXT_HOME"
	envEmbeddingDimension = "GO_CODE_CONTEXT_EMBEDDING_DIM"
)

// Config 表示 go-code-context 的运行配置。
type Config struct {
	StorageDir         string
	EmbeddingDimension int
	MaxChunkLines      int
	ChunkOverlapLines  int
	SearchLimit        int
	SupportedExts      []string
	IgnoreNames        []string
}

// Load 从环境变量和默认值加载配置。
func Load() (Config, error) {
	storageDir, err := defaultHomeStorageDir()
	if err != nil {
		return Config{}, err
	}
	if customStorageDir := strings.TrimSpace(os.Getenv(envStorageDir)); customStorageDir != "" {
		storageDir = customStorageDir
	}

	return Config{
		StorageDir:         storageDir,
		EmbeddingDimension: getPositiveInt(envEmbeddingDimension, defaultEmbeddingDim),
		MaxChunkLines:      defaultMaxChunkLines,
		ChunkOverlapLines:  defaultChunkOverlap,
		SearchLimit:        defaultSearchLimit,
		SupportedExts: []string{
			".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".java", ".cpp", ".c", ".h", ".hpp",
			".cs", ".rs", ".php", ".rb", ".swift", ".kt", ".scala", ".md", ".markdown",
		},
		IgnoreNames: []string{
			".git", ".svn", ".hg", ".idea", ".vscode", "node_modules", "dist", "build", "out", "target",
			"coverage", "__pycache__", ".pytest_cache", ".cache", "tmp", "temp", "logs", ".go-code-context",
		},
	}, nil
}

func defaultHomeStorageDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, defaultStorageDir), nil
}

func getPositiveInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsedValue, err := strconv.Atoi(value)
	if err != nil || parsedValue <= 0 {
		return fallback
	}
	return parsedValue
}
