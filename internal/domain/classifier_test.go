// 文件说明：测试基础领域分类器。
// 实现原理：构造不同 query、扩展名和 metadata，验证领域识别、经验类型和预算建议。
// 使用方式：执行 go test ./internal/domain 或 go test ./...。
// 注意事项：测试只依赖规则分类器，不调用外部模型。
// 交互模块：internal/domain、internal/contextdoc。

package domain

import (
	"testing"

	"github.com/mazhan465/Agent-Memory/internal/contextdoc"
)

func TestDefaultClassifierDetectsGolang(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Analyze(Input{
		Query:          "go test panic in goroutine channel",
		FileExtensions: []string{".go"},
	})

	wantDomain := contextdoc.Domain("programming/go")
	if result.Domain != wantDomain {
		t.Fatalf("Domain = %s, want %s", result.Domain, wantDomain)
	}
	if result.ExperienceKind != contextdoc.ExperienceKindDomain {
		t.Fatalf("ExperienceKind = %s, want %s", result.ExperienceKind, contextdoc.ExperienceKindDomain)
	}
	if result.Budget.DomainPercent != 65 {
		t.Fatalf("DomainPercent = %d, want 65", result.Budget.DomainPercent)
	}
}

func TestDefaultClassifierDetectsMilvus(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Analyze(Input{Query: "milvus collection vector store topk search"})

	wantDomain := contextdoc.Domain("database/milvus")
	if result.Domain != wantDomain {
		t.Fatalf("Domain = %s, want %s", result.Domain, wantDomain)
	}
	if result.Budget.DomainPercent == 0 {
		t.Fatal("DomainPercent = 0, want domain budget")
	}
}

func TestDefaultClassifierUsesMetadata(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Analyze(Input{Metadata: map[string]string{"component": "kubectl deployment pod"}})

	wantDomain := contextdoc.Domain("infrastructure/kubernetes")
	if result.Domain != wantDomain {
		t.Fatalf("Domain = %s, want %s", result.Domain, wantDomain)
	}
}

func TestDefaultClassifierFallsBackToGeneral(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Analyze(Input{Query: "please summarize the previous discussion"})

	if result.Domain != "" {
		t.Fatalf("Domain = %s, want empty domain", result.Domain)
	}
	if result.ExperienceKind != contextdoc.ExperienceKindGeneral {
		t.Fatalf("ExperienceKind = %s, want %s", result.ExperienceKind, contextdoc.ExperienceKindGeneral)
	}
	if result.Budget.GeneralPercent != 70 || result.Budget.ProjectToolPercent != 30 {
		t.Fatalf("Budget = %+v, want general fallback budget", result.Budget)
	}
}

func TestNormalizeExtension(t *testing.T) {
	cases := map[string]string{
		"go":              ".go",
		".TSX":            ".tsx",
		"/tmp/Dockerfile": "dockerfile",
		"handler_test.go": "handler_test.go",
	}
	for input, want := range cases {
		if got := normalizeExtension(input); got != want {
			t.Fatalf("normalizeExtension(%q) = %q, want %q", input, got, want)
		}
	}
}
