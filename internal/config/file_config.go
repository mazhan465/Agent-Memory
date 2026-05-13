// 文件说明：提供 YAML 配置文件的读写与覆盖逻辑。
// 实现原理：先构造默认 Config，再读取用户目录 .AgentMemory/config.yaml 覆盖默认值。
// 环境变量会在最后覆盖默认值和 YAML 文件值。
// 使用方式：通过 Load 自动读取配置文件，或调用 WriteDefaultFile 生成默认配置文件。
// 注意事项：API Key 和密码只作为配置值读取，不应在日志中输出。
// 交互模块：cmd/code-context、internal/config。

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type fileConfig struct {
	StorageDir  string            `yaml:"storage_dir"`
	Embedding   embeddingConfig   `yaml:"embedding"`
	VectorStore vectorStoreConfig `yaml:"vector_store"`
	Indexing    indexingConfig    `yaml:"indexing"`
	Search      searchConfig      `yaml:"search"`
}

type embeddingConfig struct {
	Provider  string       `yaml:"provider"`
	Dimension int          `yaml:"dimension"`
	OpenAI    openAIConfig `yaml:"openai"`
	Ollama    ollamaConfig `yaml:"ollama"`
}

type openAIConfig struct {
	BaseURL      string `yaml:"base_url"`
	APIKey       string `yaml:"api_key"`
	Model        string `yaml:"model"`
	Dimensions   int    `yaml:"dimensions"`
	MaxBatchSize int    `yaml:"max_batch_size"`
}

type ollamaConfig struct {
	Host       string `yaml:"host"`
	Model      string `yaml:"model"`
	Dimensions int    `yaml:"dimensions"`
}

type vectorStoreConfig struct {
	Provider string       `yaml:"provider"`
	Milvus   milvusConfig `yaml:"milvus"`
}

type milvusConfig struct {
	Address    string `yaml:"address"`
	Username   string `yaml:"username"`
	Password   string `yaml:"password"`
	Collection string `yaml:"collection"`
}

type indexingConfig struct {
	MaxChunkLines     *int     `yaml:"max_chunk_lines"`
	ChunkOverlapLines *int     `yaml:"chunk_overlap_lines"`
	SupportedExts     []string `yaml:"supported_exts"`
	IgnoreNames       []string `yaml:"ignore_names"`
	IgnorePatterns    []string `yaml:"ignore_patterns"`
}

type searchConfig struct {
	Limit        int                           `yaml:"limit"`
	DefaultTypes []string                      `yaml:"default_types"`
	Strategies   map[string]fileSearchStrategy `yaml:"strategies"`
}

type fileSearchStrategy struct {
	SemanticWeight *float64 `yaml:"semantic_weight"`
	KeywordWeight  *float64 `yaml:"keyword_weight"`
}

// DefaultFilePath 返回默认 YAML 配置文件路径。
func DefaultFilePath() (string, error) {
	path, _, err := configFilePath()
	return path, err
}

// WriteDefaultFile 生成默认 YAML 配置文件。
func WriteDefaultFile(force bool) (string, error) {
	path, _, err := configFilePath()
	if err != nil {
		return "", err
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			return "", fmt.Errorf("config file already exists: %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	cfg, err := defaultConfig()
	if err != nil {
		return "", err
	}
	data := []byte(defaultFileContent(cfg))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func defaultConfig() (Config, error) {
	storageDir, err := defaultHomeStorageDir()
	if err != nil {
		return Config{}, err
	}
	return Config{
		StorageDir:                storageDir,
		EmbeddingProvider:         defaultEmbeddingProvider,
		VectorStoreProvider:       defaultVectorStoreProvider,
		EmbeddingDimension:        defaultEmbeddingDim,
		OpenAIBaseURL:             defaultOpenAIBaseURL,
		OpenAIEmbeddingModel:      defaultOpenAIEmbeddingModel,
		OpenAIEmbeddingDimensions: defaultOpenAIEmbeddingDimensions,
		OpenAIMaxBatchSize:        defaultOpenAIMaxBatchSize,
		OllamaHost:                defaultOllamaHost,
		OllamaEmbeddingModel:      defaultOllamaEmbeddingModel,
		OllamaEmbeddingDimensions: defaultOllamaEmbeddingDimensions,
		MilvusAddress:             defaultMilvusAddress,
		MilvusCollection:          defaultMilvusCollection,
		MaxChunkLines:             defaultMaxChunkLines,
		ChunkOverlapLines:         defaultChunkOverlap,
		SearchLimit:               defaultSearchLimit,
		DefaultSearchTypes:        []string{"all"},
		SearchStrategies:          defaultSearchStrategies(),
		SupportedExts:             defaultSupportedExts(),
		IgnoreNames:               defaultIgnoreNames(),
		IgnorePatterns:            defaultIgnorePatterns(),
	}, nil
}

func applyConfigFile(cfg *Config) error {
	path, explicit, err := configFilePath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !explicit {
			return nil
		}
		return err
	}
	fileCfg := fileConfig{}
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return fmt.Errorf("read config file %s: %w", path, err)
	}
	applyFileConfig(cfg, fileCfg)
	return nil
}

func configFilePath() (string, bool, error) {
	if customPath := strings.TrimSpace(os.Getenv(envConfigPath)); customPath != "" {
		return filepath.Clean(customPath), true, nil
	}
	storageDir, err := defaultHomeStorageDir()
	if err != nil {
		return "", false, err
	}
	return filepath.Join(storageDir, defaultConfigFileName), false, nil
}

func applyFileConfig(cfg *Config, fileCfg fileConfig) {
	if value := strings.TrimSpace(fileCfg.StorageDir); value != "" {
		cfg.StorageDir = value
	}
	applyEmbeddingConfig(cfg, fileCfg.Embedding)
	applyVectorStoreConfig(cfg, fileCfg.VectorStore)
	applyIndexingConfig(cfg, fileCfg.Indexing)
	applySearchConfig(cfg, fileCfg.Search)
}

func applyEmbeddingConfig(cfg *Config, fileCfg embeddingConfig) {
	if value := strings.TrimSpace(fileCfg.Provider); value != "" {
		cfg.EmbeddingProvider = value
	}
	if fileCfg.Dimension > 0 {
		cfg.EmbeddingDimension = fileCfg.Dimension
	}
	if value := strings.TrimSpace(fileCfg.OpenAI.BaseURL); value != "" {
		cfg.OpenAIBaseURL = value
	}
	if value := strings.TrimSpace(fileCfg.OpenAI.APIKey); value != "" {
		cfg.OpenAIAPIKey = value
	}
	if value := strings.TrimSpace(fileCfg.OpenAI.Model); value != "" {
		cfg.OpenAIEmbeddingModel = value
	}
	if fileCfg.OpenAI.Dimensions > 0 {
		cfg.OpenAIEmbeddingDimensions = fileCfg.OpenAI.Dimensions
	}
	if fileCfg.OpenAI.MaxBatchSize > 0 {
		cfg.OpenAIMaxBatchSize = fileCfg.OpenAI.MaxBatchSize
	}
	if value := strings.TrimSpace(fileCfg.Ollama.Host); value != "" {
		cfg.OllamaHost = value
	}
	if value := strings.TrimSpace(fileCfg.Ollama.Model); value != "" {
		cfg.OllamaEmbeddingModel = value
	}
	if fileCfg.Ollama.Dimensions > 0 {
		cfg.OllamaEmbeddingDimensions = fileCfg.Ollama.Dimensions
	}
}

func applyVectorStoreConfig(cfg *Config, fileCfg vectorStoreConfig) {
	if value := strings.TrimSpace(fileCfg.Provider); value != "" {
		cfg.VectorStoreProvider = value
	}
	if value := strings.TrimSpace(fileCfg.Milvus.Address); value != "" {
		cfg.MilvusAddress = value
	}
	if value := strings.TrimSpace(fileCfg.Milvus.Username); value != "" {
		cfg.MilvusUsername = value
	}
	if value := strings.TrimSpace(fileCfg.Milvus.Password); value != "" {
		cfg.MilvusPassword = value
	}
	if value := strings.TrimSpace(fileCfg.Milvus.Collection); value != "" {
		cfg.MilvusCollection = value
	}
}

func applyIndexingConfig(cfg *Config, fileCfg indexingConfig) {
	if fileCfg.MaxChunkLines != nil && *fileCfg.MaxChunkLines > 0 {
		cfg.MaxChunkLines = *fileCfg.MaxChunkLines
	}
	if fileCfg.ChunkOverlapLines != nil && validChunkOverlap(*fileCfg.ChunkOverlapLines, cfg.MaxChunkLines) {
		cfg.ChunkOverlapLines = *fileCfg.ChunkOverlapLines
	}
	if values := normalizeStringSlice(fileCfg.SupportedExts, true); len(values) > 0 {
		cfg.SupportedExts = values
	}
	if values := normalizeStringSlice(fileCfg.IgnoreNames, false); len(values) > 0 {
		cfg.IgnoreNames = values
	}
	if values := normalizeStringSlice(fileCfg.IgnorePatterns, false); len(values) > 0 {
		cfg.IgnorePatterns = values
	}
}

func validChunkOverlap(overlap int, maxChunkLines int) bool {
	return overlap >= 0 && overlap < maxChunkLines
}

func applySearchConfig(cfg *Config, fileCfg searchConfig) {
	if fileCfg.Limit > 0 {
		cfg.SearchLimit = fileCfg.Limit
	}
	if values := normalizeStringSlice(fileCfg.DefaultTypes, false); len(values) > 0 {
		cfg.DefaultSearchTypes = values
	}
	for name, strategy := range fileCfg.Strategies {
		key := normalizeSearchStrategyName(name)
		if key == "" {
			continue
		}
		current := cfg.SearchStrategy(key)
		if strategy.SemanticWeight != nil {
			current.SemanticWeight = *strategy.SemanticWeight
		}
		if strategy.KeywordWeight != nil {
			current.KeywordWeight = *strategy.KeywordWeight
		}
		cfg.SearchStrategies[key] = normalizeSearchStrategy(current)
	}
}

func applyEnvConfig(cfg *Config) {
	if customStorageDir := strings.TrimSpace(os.Getenv(envStorageDir)); customStorageDir != "" {
		cfg.StorageDir = customStorageDir
	}
	cfg.EmbeddingProvider = getString(envEmbeddingProvider, cfg.EmbeddingProvider)
	cfg.VectorStoreProvider = getString(envVectorStoreProvider, cfg.VectorStoreProvider)
	cfg.EmbeddingDimension = getPositiveInt(envEmbeddingDimension, cfg.EmbeddingDimension)
	cfg.OpenAIBaseURL = getString(envOpenAIBaseURL, cfg.OpenAIBaseURL)
	if apiKey := strings.TrimSpace(os.Getenv(envOpenAIAPIKey)); apiKey != "" {
		cfg.OpenAIAPIKey = apiKey
	}
	cfg.OpenAIEmbeddingModel = getString(envOpenAIEmbeddingModel, cfg.OpenAIEmbeddingModel)
	cfg.OpenAIEmbeddingDimensions = getPositiveInt(envOpenAIEmbeddingDimensions, cfg.OpenAIEmbeddingDimensions)
	cfg.OpenAIMaxBatchSize = getPositiveInt(envOpenAIMaxBatchSize, cfg.OpenAIMaxBatchSize)
	cfg.OllamaHost = getString(envOllamaHost, cfg.OllamaHost)
	cfg.OllamaEmbeddingModel = getString(envOllamaEmbeddingModel, cfg.OllamaEmbeddingModel)
	cfg.OllamaEmbeddingDimensions = getPositiveInt(envOllamaEmbeddingDimensions, cfg.OllamaEmbeddingDimensions)
	cfg.MilvusAddress = getString(envMilvusAddress, cfg.MilvusAddress)
	if username := strings.TrimSpace(os.Getenv(envMilvusUsername)); username != "" {
		cfg.MilvusUsername = username
	}
	if password := strings.TrimSpace(os.Getenv(envMilvusPassword)); password != "" {
		cfg.MilvusPassword = password
	}
	cfg.MilvusCollection = getString(envMilvusCollection, cfg.MilvusCollection)
	if values := splitCSVValues(os.Getenv(envDefaultSearchTypes), false); len(values) > 0 {
		cfg.DefaultSearchTypes = values
	}
	applySearchStrategyEnv(cfg.SearchStrategies)
	cfg.SupportedExts = mergeCSVValues(cfg.SupportedExts, os.Getenv(envCustomExtensions), true)
	cfg.IgnorePatterns = mergeCSVValues(cfg.IgnorePatterns, os.Getenv(envCustomIgnorePatterns), false)
}

func defaultFileContent(cfg Config) string {
	var builder strings.Builder
	builder.WriteString("# Agent-Memory 默认配置文件。\n")
	builder.WriteString("# 默认路径为用户目录下的 .AgentMemory/config.yaml。\n")
	builder.WriteString("# 可用 AGENT_MEMORY_CONFIG 指定其他路径。\n")
	builder.WriteString("# 环境变量优先级高于本文件。\n")
	builder.WriteString("# 敏感字段如 api_key、password 不会输出到日志。\n\n")

	fmt.Fprintf(&builder, "# 存储索引快照、SourceCatalog、会话状态和本地向量数据的目录。\n")
	fmt.Fprintf(&builder, "storage_dir: %q\n\n", cfg.StorageDir)

	writeEmbeddingConfig(&builder, cfg)
	writeVectorStoreConfig(&builder, cfg)
	writeIndexingConfig(&builder, cfg)
	writeSearchConfig(&builder, cfg)
	return builder.String()
}

func writeEmbeddingConfig(builder *strings.Builder, cfg Config) {
	builder.WriteString("# Embedding 配置。\n")
	builder.WriteString("# provider 可选项：\n")
	builder.WriteString("# - hash: 默认本地哈希向量，零依赖，适合快速试用，但语义效果较弱。\n")
	builder.WriteString("# - openai: 使用 OpenAI Embeddings API。\n")
	builder.WriteString("# - openai-compatible: 使用兼容 OpenAI 协议的 Embeddings 服务。\n")
	builder.WriteString("# - ollama: 使用本地或远端 Ollama Embedding 模型。\n")
	builder.WriteString("embedding:\n")
	fmt.Fprintf(builder, "  provider: %s\n", cfg.EmbeddingProvider)
	fmt.Fprintf(builder, "  dimension: %d\n", cfg.EmbeddingDimension)
	builder.WriteString("\n")
	builder.WriteString("  # provider 为 openai 或 openai-compatible 时启用；默认不启用。\n")
	builder.WriteString("  # openai:\n")
	fmt.Fprintf(builder, "  #   base_url: %q\n", cfg.OpenAIBaseURL)
	builder.WriteString("  #   api_key: \"\"\n")
	fmt.Fprintf(builder, "  #   model: %q\n", cfg.OpenAIEmbeddingModel)
	fmt.Fprintf(builder, "  #   dimensions: %d\n", cfg.OpenAIEmbeddingDimensions)
	fmt.Fprintf(builder, "  #   max_batch_size: %d\n", cfg.OpenAIMaxBatchSize)
	builder.WriteString("\n")
	builder.WriteString("  # provider 为 ollama 时启用；默认不启用。\n")
	builder.WriteString("  # ollama:\n")
	fmt.Fprintf(builder, "  #   host: %q\n", cfg.OllamaHost)
	fmt.Fprintf(builder, "  #   model: %q\n", cfg.OllamaEmbeddingModel)
	fmt.Fprintf(builder, "  #   dimensions: %d  # 0 表示使用模型默认维度。\n\n", cfg.OllamaEmbeddingDimensions)
}

func writeVectorStoreConfig(builder *strings.Builder, cfg Config) {
	builder.WriteString("# 向量存储配置。\n")
	builder.WriteString("# provider 可选项：\n")
	builder.WriteString("# - local: 默认本地文件存储，适合单机和开发环境。\n")
	builder.WriteString("# - milvus: 使用 Milvus 向量数据库，适合较大规模索引。\n")
	builder.WriteString("vector_store:\n")
	fmt.Fprintf(builder, "  provider: %s\n", cfg.VectorStoreProvider)
	builder.WriteString("\n")
	builder.WriteString("  # provider 为 milvus 时启用；默认不启用。\n")
	builder.WriteString("  # milvus:\n")
	fmt.Fprintf(builder, "  #   address: %q\n", cfg.MilvusAddress)
	builder.WriteString("  #   username: \"\"\n")
	builder.WriteString("  #   password: \"\"\n")
	fmt.Fprintf(builder, "  #   collection: %q\n\n", cfg.MilvusCollection)
}

func writeIndexingConfig(builder *strings.Builder, cfg Config) {
	builder.WriteString("# 索引与扫描配置。\n")
	builder.WriteString("# max_chunk_lines 表示单个文本块最大行数。\n")
	builder.WriteString("# chunk_overlap_lines 表示相邻块重叠行数。\n")
	builder.WriteString("# supported_exts 表示会进入索引的文件扩展名，不写点号时会自动补齐。\n")
	builder.WriteString("# ignore_names 和 ignore_patterns 用于跳过目录、文件名或 glob 模式。\n")
	builder.WriteString("indexing:\n")
	fmt.Fprintf(builder, "  max_chunk_lines: %d\n", cfg.MaxChunkLines)
	fmt.Fprintf(builder, "  chunk_overlap_lines: %d\n", cfg.ChunkOverlapLines)
	writeYAMLStringList(builder, "  supported_exts", cfg.SupportedExts, false)
	writeYAMLStringList(builder, "  ignore_names", cfg.IgnoreNames, false)
	writeYAMLStringList(builder, "  ignore_patterns", cfg.IgnorePatterns, true)
	builder.WriteString("\n")
}

func writeSearchConfig(builder *strings.Builder, cfg Config) {
	builder.WriteString("# 搜索配置。\n")
	builder.WriteString("# default_types 可选项：\n")
	builder.WriteString("# all、code、doc、knowledge、conversation、experience、preference、tool_history、fact。\n")
	builder.WriteString("# - all: 同时搜索代码和所有已导入来源。\n")
	builder.WriteString("# - code: 只搜索当前代码库索引。\n")
	builder.WriteString("# - doc: 搜索所有非 code 来源，包括 knowledge、conversation、experience、preference、tool_history 和 fact。\n")
	builder.WriteString("# - knowledge: 搜索文档知识和外部知识来源。\n")
	builder.WriteString("# - conversation: 搜索历史对话。\n")
	builder.WriteString("# - experience: 搜索经验记忆。\n")
	builder.WriteString("# - preference: 搜索用户偏好。\n")
	builder.WriteString("# - tool_history: 搜索工具调用历史。\n")
	builder.WriteString("# - fact: 搜索事实记忆。\n")
	builder.WriteString("# strategies 中 semantic_weight 与 keyword_weight 会自动归一化。\n")
	builder.WriteString("# 权重可写 0.6/0.4 或 2/1。\n")
	builder.WriteString("search:\n")
	fmt.Fprintf(builder, "  limit: %d\n", cfg.SearchLimit)
	writeYAMLStringList(builder, "  default_types", cfg.DefaultSearchTypes, false)
	builder.WriteString("  strategies:\n")
	writeSearchStrategy(builder, "default", "兜底策略，未知来源类型使用。", cfg.SearchStrategy("default"))
	writeSearchStrategy(builder, "code", "代码库搜索策略。", cfg.SearchStrategy("code"))
	writeSearchStrategy(builder, "knowledge", "文档知识搜索策略。", cfg.SearchStrategy("knowledge"))
	writeSearchStrategy(
		builder,
		"external_knowledge",
		"外部知识搜索策略。",
		cfg.SearchStrategy("external_knowledge"),
	)
	writeSearchStrategy(builder, "conversation", "历史对话搜索策略。", cfg.SearchStrategy("conversation"))
	writeSearchStrategy(builder, "experience", "经验记忆搜索策略。", cfg.SearchStrategy("experience"))
	writeSearchStrategy(builder, "preference", "用户偏好搜索策略。", cfg.SearchStrategy("preference"))
	writeSearchStrategy(builder, "tool_history", "工具历史搜索策略。", cfg.SearchStrategy("tool_history"))
	writeSearchStrategy(builder, "fact", "事实记忆搜索策略。", cfg.SearchStrategy("fact"))
}

func writeSearchStrategy(builder *strings.Builder, name string, comment string, strategy SearchStrategy) {
	fmt.Fprintf(builder, "    # %s\n", comment)
	fmt.Fprintf(builder, "    %s:\n", name)
	fmt.Fprintf(builder, "      semantic_weight: %.2f\n", strategy.SemanticWeight)
	fmt.Fprintf(builder, "      keyword_weight: %.2f\n", strategy.KeywordWeight)
}

func writeYAMLStringList(builder *strings.Builder, key string, values []string, quote bool) {
	fmt.Fprintf(builder, "%s:\n", key)
	indent := strings.Repeat(" ", leadingSpaces(key))
	for _, value := range values {
		if quote {
			fmt.Fprintf(builder, "%s  - %q\n", indent, value)
			continue
		}
		fmt.Fprintf(builder, "%s  - %s\n", indent, value)
	}
}

func leadingSpaces(value string) int {
	return len(value) - len(strings.TrimLeft(value, " "))
}

func defaultSupportedExts() []string {
	return []string{
		".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".java", ".cpp", ".c", ".h", ".hpp",
		".cs", ".rs", ".php", ".rb", ".swift", ".kt", ".scala", ".m", ".mm", ".dart", ".sol",
		".md", ".markdown", ".ipynb",
	}
}

func defaultIgnoreNames() []string {
	return []string{
		".git", ".svn", ".hg", ".idea", ".vscode", "node_modules", "dist", "build", "out", "target",
		"coverage", "__pycache__", ".pytest_cache", ".cache", "tmp", "temp", "logs", ".agent-memory", ".AgentMemory",
	}
}

func defaultIgnorePatterns() []string {
	return []string{
		"node_modules/**", "dist/**", "build/**", "out/**", "target/**", "coverage/**", ".nyc_output/**",
		".git/**", ".svn/**", ".hg/**", ".idea/**", ".vscode/**", "__pycache__/**", ".pytest_cache/**",
		".cache/**", "tmp/**", "temp/**", "logs/**", "*.log", ".env", ".env.*", "*.local",
		"*.min.js", "*.min.css", "*.min.map", "*.bundle.js", "*.bundle.css", "*.chunk.js", "*.vendor.js",
		"*.polyfills.js", "*.runtime.js", "*.map", ".agent-memory/**", ".AgentMemory/**",
	}
}

func splitCSVValues(csvValue string, normalizeExtension bool) []string {
	return mergeCSVValues(nil, csvValue, normalizeExtension)
}

func normalizeStringSlice(values []string, normalizeExtension bool) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		cleanValue := strings.TrimSpace(value)
		if cleanValue == "" {
			continue
		}
		if normalizeExtension && !strings.HasPrefix(cleanValue, ".") {
			cleanValue = "." + cleanValue
		}
		if _, ok := seen[cleanValue]; ok {
			continue
		}
		seen[cleanValue] = struct{}{}
		result = append(result, cleanValue)
	}
	return result
}
