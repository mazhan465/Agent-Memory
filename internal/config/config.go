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
	defaultStorageDir                = ".AgentMemory"
	defaultConfigFileName            = "config.yaml"
	defaultEmbeddingProvider         = "hash"
	defaultVectorStoreProvider       = "local"
	defaultEmbeddingDim              = 1024
	defaultMaxChunkLines             = 120
	defaultChunkOverlap              = 20
	defaultSearchLimit               = 8
	defaultOpenAIBaseURL             = "https://api.openai.com/v1"
	defaultOpenAIEmbeddingModel      = "text-embedding-3-small"
	defaultOpenAIEmbeddingDimensions = defaultEmbeddingDim
	defaultOpenAIMaxBatchSize        = 10
	defaultOllamaHost                = "http://127.0.0.1:11434"
	defaultOllamaEmbeddingModel      = "embeddinggemma"
	defaultOllamaEmbeddingDimensions = 0
	defaultMilvusAddress             = "localhost:19530"
	defaultMilvusCollection          = "agent_memory_chunks"
	envConfigPath                    = "AGENT_MEMORY_CONFIG"
	envStorageDir                    = "AGENT_MEMORY_HOME"
	envEmbeddingProvider             = "AGENT_MEMORY_EMBEDDING_PROVIDER"
	envVectorStoreProvider           = "AGENT_MEMORY_VECTOR_STORE"
	envEmbeddingDimension            = "AGENT_MEMORY_EMBEDDING_DIM"
	envOpenAIBaseURL                 = "AGENT_MEMORY_OPENAI_BASE_URL"
	envOpenAIAPIKey                  = "AGENT_MEMORY_OPENAI_API_KEY"
	envOpenAIEmbeddingModel          = "AGENT_MEMORY_OPENAI_EMBEDDING_MODEL"
	envOpenAIEmbeddingDimensions     = "AGENT_MEMORY_OPENAI_EMBEDDING_DIMENSIONS"
	envOpenAIMaxBatchSize            = "AGENT_MEMORY_OPENAI_MAX_BATCH_SIZE"
	envOllamaHost                    = "AGENT_MEMORY_OLLAMA_HOST"
	envOllamaEmbeddingModel          = "AGENT_MEMORY_OLLAMA_EMBEDDING_MODEL"
	envOllamaEmbeddingDimensions     = "AGENT_MEMORY_OLLAMA_EMBEDDING_DIMENSIONS"
	envMilvusAddress                 = "AGENT_MEMORY_MILVUS_ADDRESS"
	envMilvusUsername                = "AGENT_MEMORY_MILVUS_USERNAME"
	envMilvusPassword                = "AGENT_MEMORY_MILVUS_PASSWORD"
	envMilvusCollection              = "AGENT_MEMORY_MILVUS_COLLECTION"
	envDefaultSearchTypes            = "AGENT_MEMORY_DEFAULT_SEARCH_TYPES"
	envCustomExtensions              = "AGENT_MEMORY_CUSTOM_EXTENSIONS"
	envCustomIgnorePatterns          = "AGENT_MEMORY_CUSTOM_IGNORE_PATTERNS"
)

// SearchStrategy 表示指定来源类型的混合检索权重。
type SearchStrategy struct {
	SemanticWeight float64
	KeywordWeight  float64
}

// Config 表示 Agent-Memory 的运行配置。
type Config struct {
	StorageDir                string
	EmbeddingProvider         string
	VectorStoreProvider       string
	EmbeddingDimension        int
	OpenAIBaseURL             string
	OpenAIAPIKey              string
	OpenAIEmbeddingModel      string
	OpenAIEmbeddingDimensions int
	OpenAIMaxBatchSize        int
	OllamaHost                string
	OllamaEmbeddingModel      string
	OllamaEmbeddingDimensions int
	MilvusAddress             string
	MilvusUsername            string
	MilvusPassword            string
	MilvusCollection          string
	MaxChunkLines             int
	ChunkOverlapLines         int
	SearchLimit               int
	DefaultSearchTypes        []string
	SearchStrategies          map[string]SearchStrategy
	SupportedExts             []string
	IgnoreNames               []string
	IgnorePatterns            []string
}

// Load 从 YAML 配置文件、环境变量和默认值加载配置。
func Load() (Config, error) {
	cfg, err := defaultConfig()
	if err != nil {
		return Config{}, err
	}
	if err := applyConfigFile(&cfg); err != nil {
		return Config{}, err
	}
	applyEnvConfig(&cfg)
	return cfg, nil
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

func getString(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func mergeCSVValues(defaults []string, csvValue string, normalizeExtension bool) []string {
	seen := make(map[string]struct{}, len(defaults))
	values := make([]string, 0, len(defaults))
	appendValue := func(value string) {
		cleanValue := strings.TrimSpace(value)
		if cleanValue == "" {
			return
		}
		if normalizeExtension && !strings.HasPrefix(cleanValue, ".") {
			cleanValue = "." + cleanValue
		}
		if _, ok := seen[cleanValue]; ok {
			return
		}
		seen[cleanValue] = struct{}{}
		values = append(values, cleanValue)
	}
	for _, value := range defaults {
		appendValue(value)
	}
	for value := range strings.SplitSeq(csvValue, ",") {
		appendValue(value)
	}
	return values
}
