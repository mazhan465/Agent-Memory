// 文件说明：定义领域画像和本地静态领域画像存储。
// 实现原理：领域画像保存领域路径、描述、别名、典型问题和可选向量，供 DomainResolver 进行语义或规则匹配。
// 使用方式：DomainResolver 可通过 ProfileStore 获取领域画像，后续可替换为文件或向量存储实现。
// 注意事项：当前 StaticProfileStore 是内存实现，后续会扩展为 source-sharded 文件存储。
// 交互模块：internal/domain、internal/contextdoc。

package domain

import (
	"context"
	"errors"

	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
)

// DomainProfile 表示一个动态领域的画像。
type DomainProfile struct {
	Domain      contextdoc.Domain `json:"domain_path"`
	Description string            `json:"description"`
	Aliases     []string          `json:"aliases,omitempty"`
	Examples    []string          `json:"examples,omitempty"`
	Embedding   []float32         `json:"embedding,omitempty"`
}

// Validate 校验领域画像。
func (p DomainProfile) Validate() error {
	if p.Domain == "" {
		return errors.New("domain profile path is empty")
	}
	if _, err := contextdoc.NormalizeDomain(string(p.Domain)); err != nil {
		return err
	}
	return nil
}

// Text 返回可用于匹配或 embedding 的领域画像文本。
func (p DomainProfile) Text() string {
	parts := make([]string, 0, 2+len(p.Aliases)+len(p.Examples))
	parts = append(parts, string(p.Domain), p.Description)
	parts = append(parts, p.Aliases...)
	parts = append(parts, p.Examples...)
	return stringsJoin(parts)
}

// ProfileStore 定义领域画像读取接口。
type ProfileStore interface {
	ListProfiles(ctx context.Context) ([]DomainProfile, error)
}

// StaticProfileStore 是内存领域画像存储。
type StaticProfileStore struct {
	profiles []DomainProfile
}

// NewStaticProfileStore 创建内存领域画像存储。
func NewStaticProfileStore(profiles []DomainProfile) *StaticProfileStore {
	return &StaticProfileStore{profiles: append([]DomainProfile(nil), profiles...)}
}

// ListProfiles 返回所有领域画像。
func (s *StaticProfileStore) ListProfiles(ctx context.Context) ([]DomainProfile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	profiles := append([]DomainProfile(nil), s.profiles...)
	return profiles, nil
}

// DefaultProfileStore 返回内置领域画像存储。
func DefaultProfileStore() *StaticProfileStore {
	return NewStaticProfileStore([]DomainProfile{
		{
			Domain:      mustDomain("programming/go"),
			Description: "Go 语言开发、goroutine、channel、go test、go mod、panic、defer、接口和性能分析相关经验",
			Aliases:     []string{"go", "golang", "goroutine", "channel", "go test", "go mod"},
			Examples: []string{
				"Go 单元测试失败如何排查",
				"goroutine 泄漏分析",
				"panic 和 recover 使用边界",
			},
		},
		{
			Domain:      mustDomain("database/milvus"),
			Description: "Milvus、向量数据库、collection、index、load、search、TopK、embedding 和 hybrid search 相关经验",
			Aliases:     []string{"milvus", "vector database", "vector store", "collection", "embedding", "hybrid search"},
			Examples: []string{
				"Milvus collection not loaded",
				"vector search returns empty result",
				"hybrid search ranking problem",
			},
		},
		{
			Domain:      mustDomain("infrastructure/kubernetes"),
			Description: "Kubernetes、k8s、pod、deployment、container、helm、kubectl 和 namespace 相关经验",
			Aliases:     []string{"kubernetes", "k8s", "pod", "deployment", "container", "helm", "kubectl"},
			Examples: []string{
				"Pod CrashLoopBackOff 排查",
				"Kubernetes deployment 发布失败",
				"kubectl 查看 namespace 下资源",
			},
		},
	})
}

func stringsJoin(parts []string) string {
	result := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		if result != "" {
			result += " "
		}
		result += part
	}
	return result
}
