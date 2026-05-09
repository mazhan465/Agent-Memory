// 文件说明：测试多信号领域解析框架。
// 实现原理：构造显式信号、规则信号、领域画像信号和记忆分布信号，验证 DomainResolver 的融合决策。
// 使用方式：执行 go test ./internal/domain 或 go test ./...。
// 注意事项：测试不依赖 embedding 和外部模型。
// 交互模块：internal/domain、internal/contextdoc。

package domain

import (
	"context"
	"testing"

	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
)

func TestDomainResolverUsesExplicitMetadata(t *testing.T) {
	resolver := NewDefaultDomainResolver()
	decision, err := resolver.Resolve(context.Background(), Input{
		Metadata: map[string]string{"domain_path": "database/milvus"},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	wantDomain := contextdoc.Domain("database/milvus")
	if decision.Domain != wantDomain {
		t.Fatalf("Domain = %s, want %s", decision.Domain, wantDomain)
	}
	if decision.ParentDomain != contextdoc.Domain("database") {
		t.Fatalf("ParentDomain = %s, want database", decision.ParentDomain)
	}
	if len(decision.Evidence) == 0 {
		t.Fatal("Evidence is empty")
	}
}

func TestDomainResolverFallsBackToGeneral(t *testing.T) {
	resolver := NewDefaultDomainResolver()
	decision, err := resolver.Resolve(context.Background(), Input{Query: "summarize previous discussion"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if decision.Domain != "" {
		t.Fatalf("Domain = %s, want empty", decision.Domain)
	}
	if decision.ExperienceKind != contextdoc.ExperienceKindGeneral {
		t.Fatalf("ExperienceKind = %s, want %s", decision.ExperienceKind, contextdoc.ExperienceKindGeneral)
	}
	if decision.Budget.GeneralPercent != 70 {
		t.Fatalf("GeneralPercent = %d, want 70", decision.Budget.GeneralPercent)
	}
}

func TestMemoryDistributionSignal(t *testing.T) {
	resolver := NewDomainResolver(MemoryDistributionSignal{})
	decision, err := resolver.Resolve(context.Background(), Input{
		MemoryDomains: []contextdoc.Domain{
			contextdoc.Domain("programming/go"),
			contextdoc.Domain("programming/go"),
			contextdoc.Domain("database/milvus"),
		},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	wantDomain := contextdoc.Domain("programming/go")
	if decision.Domain != wantDomain {
		t.Fatalf("Domain = %s, want %s", decision.Domain, wantDomain)
	}
}

func TestStaticProfileStore(t *testing.T) {
	profile := DomainProfile{Domain: contextdoc.Domain("medicine/cardiology"), Description: "heart disease arrhythmia"}
	store := NewStaticProfileStore([]DomainProfile{profile})
	profiles, err := store.ListProfiles(context.Background())
	if err != nil {
		t.Fatalf("ListProfiles() error = %v", err)
	}
	if len(profiles) != 1 || profiles[0].Domain != profile.Domain {
		t.Fatalf("profiles = %+v, want %+v", profiles, profile)
	}
}

func TestProfileSignal(t *testing.T) {
	store := NewStaticProfileStore([]DomainProfile{
		{Domain: contextdoc.Domain("medicine/cardiology"), Description: "heart arrhythmia anticoagulation", Aliases: []string{"cardiology"}},
	})
	signal := NewProfileSignal(store)
	evidences, err := signal.Evaluate(context.Background(), Input{Query: "cardiology arrhythmia anticoagulation"})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(evidences) != 1 {
		t.Fatalf("len(evidences) = %d, want 1", len(evidences))
	}
	if evidences[0].Domain != contextdoc.Domain("medicine/cardiology") {
		t.Fatalf("Domain = %s, want medicine/cardiology", evidences[0].Domain)
	}
}
