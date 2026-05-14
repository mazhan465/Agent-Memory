// 文件说明：测试项目配置加载逻辑。
// 实现原理：通过 t.Setenv 设置环境变量，验证 Load 会读取并清理配置值。
// 使用方式：执行 go test ./internal/config 或 go test ./...。
// 注意事项：测试不会读取真实用户环境变量。
// 交互模块：internal/config。

package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLoadEmbeddingConfigFromEnv(t *testing.T) {
	setIsolatedHome(t)
	t.Setenv(envStorageDir, "/tmp/agent-memory-test")
	t.Setenv(envEmbeddingProvider, "openai")
	t.Setenv(envVectorStoreProvider, "milvus")
	t.Setenv(envEmbeddingDimension, "512")
	t.Setenv(envOpenAIBaseURL, "https://example.com/v1")
	t.Setenv(envOpenAIAPIKey, " test-api-key-placeholder ")
	t.Setenv(envOpenAIEmbeddingModel, "text-embedding-test")
	t.Setenv(envOpenAIEmbeddingDimensions, "768")
	t.Setenv(envOpenAIMaxBatchSize, "5")
	t.Setenv(envOpenAIMaxInputLength, "4096")
	t.Setenv(envOllamaHost, " http://localhost:11434 ")
	t.Setenv(envOllamaEmbeddingModel, "nomic-embed-text")
	t.Setenv(envOllamaEmbeddingDimensions, "384")
	t.Setenv(envMilvusAddress, "127.0.0.1:19530")
	t.Setenv(envMilvusUsername, " test-user ")
	t.Setenv(envMilvusPassword, " test-password-placeholder ")
	t.Setenv(envMilvusCollection, "test_collection")
	t.Setenv(envCustomExtensions, "vue,.svelte")
	t.Setenv(envCustomIgnorePatterns, "private/**,*.backup")
	t.Setenv("AGENT_MEMORY_SEARCH_CODE_SEMANTIC_WEIGHT", "0.2")
	t.Setenv("AGENT_MEMORY_SEARCH_CODE_KEYWORD_WEIGHT", "0.8")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.StorageDir != "/tmp/agent-memory-test" {
		t.Fatalf("StorageDir = %s, want /tmp/agent-memory-test", cfg.StorageDir)
	}
	if cfg.EmbeddingProvider != "openai" {
		t.Fatalf("EmbeddingProvider = %s, want openai", cfg.EmbeddingProvider)
	}
	if cfg.VectorStoreProvider != "milvus" {
		t.Fatalf("VectorStoreProvider = %s, want milvus", cfg.VectorStoreProvider)
	}
	if cfg.EmbeddingDimension != 512 {
		t.Fatalf("EmbeddingDimension = %d, want 512", cfg.EmbeddingDimension)
	}
	if cfg.OpenAIBaseURL != "https://example.com/v1" {
		t.Fatalf("OpenAIBaseURL = %s, want https://example.com/v1", cfg.OpenAIBaseURL)
	}
	if cfg.OpenAIAPIKey != "test-api-key-placeholder" {
		t.Fatalf("OpenAIAPIKey was not trimmed")
	}
	if cfg.OpenAIEmbeddingModel != "text-embedding-test" {
		t.Fatalf("OpenAIEmbeddingModel = %s, want text-embedding-test", cfg.OpenAIEmbeddingModel)
	}
	if cfg.OpenAIEmbeddingDimensions != 768 {
		t.Fatalf("OpenAIEmbeddingDimensions = %d, want 768", cfg.OpenAIEmbeddingDimensions)
	}
	if cfg.OpenAIMaxBatchSize != 5 {
		t.Fatalf("OpenAIMaxBatchSize = %d, want 5", cfg.OpenAIMaxBatchSize)
	}
	if cfg.OllamaHost != "http://localhost:11434" {
		t.Fatalf("OllamaHost = %s, want http://localhost:11434", cfg.OllamaHost)
	}
	if cfg.OllamaEmbeddingModel != "nomic-embed-text" {
		t.Fatalf("OllamaEmbeddingModel = %s, want nomic-embed-text", cfg.OllamaEmbeddingModel)
	}
	if cfg.OllamaEmbeddingDimensions != 384 {
		t.Fatalf("OllamaEmbeddingDimensions = %d, want 384", cfg.OllamaEmbeddingDimensions)
	}
	if cfg.MilvusAddress != "127.0.0.1:19530" {
		t.Fatalf("MilvusAddress = %s, want 127.0.0.1:19530", cfg.MilvusAddress)
	}
	if cfg.MilvusUsername != "test-user" {
		t.Fatalf("MilvusUsername was not trimmed")
	}
	if cfg.MilvusPassword != "test-password-placeholder" {
		t.Fatalf("MilvusPassword was not trimmed")
	}
	if cfg.MilvusCollection != "test_collection" {
		t.Fatalf("MilvusCollection = %s, want test_collection", cfg.MilvusCollection)
	}
	if !containsString(cfg.SupportedExts, ".vue") || !containsString(cfg.SupportedExts, ".svelte") {
		t.Fatalf("SupportedExts does not contain custom extensions: %v", cfg.SupportedExts)
	}
	if !containsString(cfg.IgnorePatterns, "private/**") || !containsString(cfg.IgnorePatterns, "*.backup") {
		t.Fatalf("IgnorePatterns does not contain custom patterns: %v", cfg.IgnorePatterns)
	}
	if len(cfg.DefaultSearchTypes) != 1 || cfg.DefaultSearchTypes[0] != "all" {
		t.Fatalf("DefaultSearchTypes = %v, want [all]", cfg.DefaultSearchTypes)
	}
	codeStrategy := cfg.SearchStrategy(SearchStrategyCode)
	if codeStrategy.SemanticWeight != 0.2 || codeStrategy.KeywordWeight != 0.8 {
		t.Fatalf("code strategy = %+v, want semantic=0.2 keyword=0.8", codeStrategy)
	}
	conversationStrategy := cfg.SearchStrategy(SearchStrategyConversation)
	if conversationStrategy.SemanticWeight != 0.85 || conversationStrategy.KeywordWeight != 0.15 {
		t.Fatalf("conversation strategy = %+v, want semantic=0.85 keyword=0.15", conversationStrategy)
	}
}

func TestDefaultCodeSearchStrategyPrefersKeywords(t *testing.T) {
	setIsolatedHome(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.EmbeddingDimension != 1024 {
		t.Fatalf("EmbeddingDimension = %d, want 1024", cfg.EmbeddingDimension)
	}
	if cfg.OpenAIEmbeddingDimensions != 1024 {
		t.Fatalf("OpenAIEmbeddingDimensions = %d, want 1024", cfg.OpenAIEmbeddingDimensions)
	}
	if cfg.OpenAIMaxBatchSize != 10 {
		t.Fatalf("OpenAIMaxBatchSize = %d, want 10", cfg.OpenAIMaxBatchSize)
	}
	if cfg.OpenAIMaxInputLength != 8192 {
		t.Fatalf("OpenAIMaxInputLength = %d, want 8192", cfg.OpenAIMaxInputLength)
	}
	codeStrategy := cfg.SearchStrategy(SearchStrategyCode)
	if codeStrategy.SemanticWeight != 0.3 || codeStrategy.KeywordWeight != 0.7 {
		t.Fatalf("code strategy = %+v, want semantic=0.3 keyword=0.7", codeStrategy)
	}
}

func TestLoadConfigFromYAMLFile(t *testing.T) {
	setIsolatedHome(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := `storage_dir: /tmp/yaml-agent-memory
embedding:
  provider: openai
  dimension: 1024
  openai:
    base_url: https://example.com/v1
    api_key: yaml-api-key-placeholder
    model: text-embedding-3-large
    dimensions: 1536
    max_batch_size: 6
    max_input_length: 8192
  ollama:
    host: http://localhost:11435
    model: nomic-embed-text
    dimensions: 384
vector_store:
  provider: milvus
  milvus:
    address: 127.0.0.1:19530
    username: test-user
    password: yaml-password-placeholder
    collection: yaml_collection
indexing:
  max_chunk_lines: 80
  chunk_overlap_lines: 10
  supported_exts:
    - go
    - .md
  ignore_names:
    - .git
    - vendor
  ignore_patterns:
    - vendor/**
search:
  limit: 12
  default_types:
    - code
    - experience
  strategies:
    code:
      semantic_weight: 0.3
      keyword_weight: 0.7
    experience:
      semantic_weight: 2
      keyword_weight: 1
`
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv(envConfigPath, configPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.StorageDir != "/tmp/yaml-agent-memory" {
		t.Fatalf("StorageDir = %s, want /tmp/yaml-agent-memory", cfg.StorageDir)
	}
	if cfg.EmbeddingProvider != "openai" || cfg.EmbeddingDimension != 1024 {
		t.Fatalf("embedding config = %s/%d, want openai/1024", cfg.EmbeddingProvider, cfg.EmbeddingDimension)
	}
	if cfg.OpenAIAPIKey != "yaml-api-key-placeholder" || cfg.OpenAIEmbeddingModel != "text-embedding-3-large" {
		t.Fatalf("openai config = key:%s model:%s", cfg.OpenAIAPIKey, cfg.OpenAIEmbeddingModel)
	}
	if cfg.OpenAIEmbeddingDimensions != 1536 || cfg.OpenAIMaxBatchSize != 6 || cfg.OpenAIMaxInputLength != 8192 {
		t.Fatalf(
			"openai dimensions/batch/input = %d/%d/%d, want 1536/6/8192",
			cfg.OpenAIEmbeddingDimensions,
			cfg.OpenAIMaxBatchSize,
			cfg.OpenAIMaxInputLength,
		)
	}
	if cfg.OllamaEmbeddingDimensions != 384 {
		t.Fatalf("OllamaEmbeddingDimensions = %d, want 384", cfg.OllamaEmbeddingDimensions)
	}
	if cfg.VectorStoreProvider != "milvus" || cfg.MilvusCollection != "yaml_collection" {
		t.Fatalf("vector store config = %s/%s", cfg.VectorStoreProvider, cfg.MilvusCollection)
	}
	if cfg.MaxChunkLines != 80 || cfg.ChunkOverlapLines != 10 {
		t.Fatalf("chunk config = %d/%d, want 80/10", cfg.MaxChunkLines, cfg.ChunkOverlapLines)
	}
	if !slices.Equal(cfg.SupportedExts, []string{".go", ".md"}) {
		t.Fatalf("SupportedExts = %v, want [.go .md]", cfg.SupportedExts)
	}
	if !slices.Equal(cfg.DefaultSearchTypes, []string{"code", "experience"}) {
		t.Fatalf("DefaultSearchTypes = %v, want [code experience]", cfg.DefaultSearchTypes)
	}
	codeStrategy := cfg.SearchStrategy(SearchStrategyCode)
	if codeStrategy.SemanticWeight != 0.3 || codeStrategy.KeywordWeight != 0.7 {
		t.Fatalf("code strategy = %+v, want semantic=0.3 keyword=0.7", codeStrategy)
	}
	experienceStrategy := cfg.SearchStrategy(SearchStrategyExperience)
	if experienceStrategy.SemanticWeight != 2.0/3.0 || experienceStrategy.KeywordWeight != 1.0/3.0 {
		t.Fatalf("experience strategy = %+v, want semantic=2/3 keyword=1/3", experienceStrategy)
	}
}

func TestWriteDefaultFile(t *testing.T) {
	setIsolatedHome(t)
	path, err := WriteDefaultFile(false)
	if err != nil {
		t.Fatalf("WriteDefaultFile() error = %v", err)
	}
	wantPath := filepath.Join(os.Getenv("HOME"), defaultStorageDir, defaultConfigFileName)
	if path != wantPath {
		t.Fatalf("path = %s, want %s", path, wantPath)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	generated := string(data)
	for _, want := range []string{
		"# provider 可选项：",
		"# - hash:",
		"# - openai:",
		"# - openai-compatible:",
		"# - ollama:",
		"  # openai:",
		"  #   dimensions: 1024",
		"  #   max_batch_size: 10",
		"  # ollama:",
		"  #   dimensions: 0",
		"  # milvus:",
		"default_types:",
		"external_knowledge:",
		"semantic_weight:",
		"keyword_weight:",
	} {
		if !strings.Contains(generated, want) {
			t.Fatalf("generated config does not contain %q: %s", want, generated)
		}
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() generated config error = %v", err)
	}
	if cfg.EmbeddingProvider != defaultEmbeddingProvider || cfg.VectorStoreProvider != defaultVectorStoreProvider {
		t.Fatalf("generated config providers = %s/%s", cfg.EmbeddingProvider, cfg.VectorStoreProvider)
	}
	if _, err := WriteDefaultFile(false); err == nil {
		t.Fatalf("WriteDefaultFile(false) succeeded for existing file")
	}
	if _, err := WriteDefaultFile(true); err != nil {
		t.Fatalf("WriteDefaultFile(true) error = %v", err)
	}
}

func setIsolatedHome(t *testing.T) {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	for _, name := range []string{
		envConfigPath,
		envStorageDir,
		envEmbeddingProvider,
		envVectorStoreProvider,
		envEmbeddingDimension,
		envOpenAIBaseURL,
		envOpenAIAPIKey,
		envOpenAIEmbeddingModel,
		envOpenAIEmbeddingDimensions,
		envOpenAIMaxBatchSize,
		envOpenAIMaxInputLength,
		envOllamaHost,
		envOllamaEmbeddingModel,
		envOllamaEmbeddingDimensions,
		envMilvusAddress,
		envMilvusUsername,
		envMilvusPassword,
		envMilvusCollection,
		envDefaultSearchTypes,
		envCustomExtensions,
		envCustomIgnorePatterns,
	} {
		t.Setenv(name, "")
	}
	for name := range defaultSearchStrategies() {
		t.Setenv(searchStrategyEnvName(name, "SEMANTIC_WEIGHT"), "")
		t.Setenv(searchStrategyEnvName(name, "KEYWORD_WEIGHT"), "")
	}
}

func containsString(values []string, want string) bool {
	return slices.Contains(values, want)
}
