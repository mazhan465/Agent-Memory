// 文件说明：测试项目配置加载逻辑。
// 实现原理：通过 t.Setenv 设置环境变量，验证 Load 会读取并清理配置值。
// 使用方式：执行 go test ./internal/config 或 go test ./...。
// 注意事项：测试不会读取真实用户环境变量。
// 交互模块：internal/config。

package config

import "testing"

func TestLoadEmbeddingConfigFromEnv(t *testing.T) {
	t.Setenv(envStorageDir, "/tmp/agent-memory-test")
	t.Setenv(envEmbeddingProvider, "openai")
	t.Setenv(envVectorStoreProvider, "milvus")
	t.Setenv(envEmbeddingDimension, "512")
	t.Setenv(envOpenAIBaseURL, "https://example.com/v1")
	t.Setenv(envOpenAIAPIKey, " test-key ")
	t.Setenv(envOpenAIEmbeddingModel, "text-embedding-test")
	t.Setenv(envOllamaHost, " http://localhost:11434 ")
	t.Setenv(envOllamaEmbeddingModel, "nomic-embed-text")
	t.Setenv(envMilvusAddress, "127.0.0.1:19530")
	t.Setenv(envMilvusUsername, " root ")
	t.Setenv(envMilvusPassword, " password ")
	t.Setenv(envMilvusCollection, "test_collection")
	t.Setenv(envCustomExtensions, "vue,.svelte")
	t.Setenv(envCustomIgnorePatterns, "private/**,*.backup")

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
	if cfg.OpenAIAPIKey != "test-key" {
		t.Fatalf("OpenAIAPIKey was not trimmed")
	}
	if cfg.OpenAIEmbeddingModel != "text-embedding-test" {
		t.Fatalf("OpenAIEmbeddingModel = %s, want text-embedding-test", cfg.OpenAIEmbeddingModel)
	}
	if cfg.OllamaHost != "http://localhost:11434" {
		t.Fatalf("OllamaHost = %s, want http://localhost:11434", cfg.OllamaHost)
	}
	if cfg.OllamaEmbeddingModel != "nomic-embed-text" {
		t.Fatalf("OllamaEmbeddingModel = %s, want nomic-embed-text", cfg.OllamaEmbeddingModel)
	}
	if cfg.MilvusAddress != "127.0.0.1:19530" {
		t.Fatalf("MilvusAddress = %s, want 127.0.0.1:19530", cfg.MilvusAddress)
	}
	if cfg.MilvusUsername != "root" {
		t.Fatalf("MilvusUsername was not trimmed")
	}
	if cfg.MilvusPassword != "password" {
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
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
