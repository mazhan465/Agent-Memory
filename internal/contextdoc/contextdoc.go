// 文件说明：定义通用上下文文档、作用域、来源和 source manifest 模型。
// 实现原理：用 Scope 描述检索作用域，用 Source 描述数据来源，并提供稳定 namespace 和 ID 生成逻辑。
// 使用方式：索引器、SourceCatalog、SourceStore 和搜索器可复用本包模型组织代码、会话和长期记忆。
// 注意事项：本包只定义通用模型和确定性命名逻辑，不直接读写存储，也不处理 embedding。
// 交互模块：internal/indexer、internal/searcher、internal/vectorstore、internal/snapshot。

// Package contextdoc 定义通用语义上下文文档模型。
package contextdoc

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const (
	namespaceHashLength = 16
	defaultIDPrefix     = "doc"
)

// ScopeType 表示上下文作用域类型。
type ScopeType string

const (
	// ScopeTypeUser 表示用户全局作用域。
	ScopeTypeUser ScopeType = "user"
	// ScopeTypeWorkspace 表示工作区作用域。
	ScopeTypeWorkspace ScopeType = "workspace"
	// ScopeTypeCodebase 表示代码库作用域。
	ScopeTypeCodebase ScopeType = "codebase"
	// ScopeTypeTool 表示工具作用域。
	ScopeTypeTool ScopeType = "tool"
)

// SourceType 表示上下文文档来源类型。
type SourceType string

const (
	// SourceTypeCodebase 表示本地代码库来源。
	SourceTypeCodebase SourceType = "codebase"
	// SourceTypeConversation 表示历史会话来源。
	SourceTypeConversation SourceType = "conversation"
	// SourceTypeToolHistory 表示工具调用历史来源。
	SourceTypeToolHistory SourceType = "tool_history"
	// SourceTypePreference 表示用户稳定偏好来源。
	SourceTypePreference SourceType = "preference"
	// SourceTypeExperience 表示历史问题解决经验来源。
	SourceTypeExperience SourceType = "experience"
	// SourceTypeFact 表示项目事实、环境信息或架构决策来源。
	SourceTypeFact SourceType = "fact"
	// SourceTypeDocument 表示书籍、技术文档、SDK 文档或项目规范来源。
	SourceTypeDocument SourceType = "document"
	// SourceTypeExternalKnowledge 表示 Basic Memory 等外部知识库来源。
	SourceTypeExternalKnowledge SourceType = "external_knowledge"
)

// ChunkType 表示文档片段类型。
type ChunkType string

const (
	// ChunkTypeCode 表示代码片段。
	ChunkTypeCode ChunkType = "code"
	// ChunkTypeMessage 表示会话消息片段。
	ChunkTypeMessage ChunkType = "message"
	// ChunkTypeSummary 表示摘要片段。
	ChunkTypeSummary ChunkType = "summary"
	// ChunkTypePreference 表示用户偏好片段。
	ChunkTypePreference ChunkType = "preference"
	// ChunkTypeExperience 表示经验片段。
	ChunkTypeExperience ChunkType = "experience"
	// ChunkTypeFact 表示事实片段。
	ChunkTypeFact ChunkType = "fact"
	// ChunkTypeDocumentSummary 表示整篇文档摘要。
	ChunkTypeDocumentSummary ChunkType = "document_summary"
	// ChunkTypeChapterSummary 表示章节摘要。
	ChunkTypeChapterSummary ChunkType = "chapter_summary"
	// ChunkTypeSectionSummary 表示小节摘要。
	ChunkTypeSectionSummary ChunkType = "section_summary"
	// ChunkTypeDocumentChunk 表示文档原文片段。
	ChunkTypeDocumentChunk ChunkType = "document_chunk"
)

// Role 表示会话消息角色。
type Role string

const (
	// RoleUser 表示用户消息。
	RoleUser Role = "user"
	// RoleAssistant 表示助手消息。
	RoleAssistant Role = "assistant"
	// RoleSystem 表示系统消息或系统摘要。
	RoleSystem Role = "system"
	// RoleSummary 表示抽取后的摘要。
	RoleSummary Role = "summary"
)

// ExperienceKind 表示经验记忆类型。
type ExperienceKind string

const (
	// ExperienceKindGeneral 表示通用经验。
	ExperienceKindGeneral ExperienceKind = "general"
	// ExperienceKindDomain 表示领域专业经验。
	ExperienceKindDomain ExperienceKind = "domain"
	// ExperienceKindProject 表示项目经验。
	ExperienceKindProject ExperienceKind = "project"
	// ExperienceKindTool 表示工具经验。
	ExperienceKindTool ExperienceKind = "tool"
)

// KnowledgeKind 表示文档知识类型。
type KnowledgeKind string

const (
	// KnowledgeKindRule 表示规则、规范或强约束。
	KnowledgeKindRule KnowledgeKind = "rule"
	// KnowledgeKindAPI 表示 API、参数或配置说明。
	KnowledgeKindAPI KnowledgeKind = "api"
	// KnowledgeKindExample 表示示例或样例代码。
	KnowledgeKindExample KnowledgeKind = "example"
	// KnowledgeKindTroubleshooting 表示错误排查或 FAQ。
	KnowledgeKindTroubleshooting KnowledgeKind = "troubleshooting"
	// KnowledgeKindConcept 表示概念、原理或背景知识。
	KnowledgeKindConcept KnowledgeKind = "concept"
)

// Domain 表示动态创建的树形专业领域路径。
//
// 领域不是枚举值，而是类似标签的路径。当前最多支持两级，例如：
// programming/go、database/milvus、medicine/cardiology。
type Domain string

const (
	domainSeparator = "/"
	maxDomainDepth  = 2
)

// NormalizeDomain 将输入归一化为领域路径。
func NormalizeDomain(value string) (Domain, error) {
	levels := splitDomainLevels(value)
	if len(levels) == 0 {
		return "", errors.New("domain is empty")
	}
	if len(levels) > maxDomainDepth {
		return "", errors.New("domain depth exceeds supported limit")
	}
	return Domain(strings.Join(levels, domainSeparator)), nil
}

// Levels 返回领域路径层级。
func (d Domain) Levels() []string {
	return splitDomainLevels(string(d))
}

// Depth 返回领域路径深度。
func (d Domain) Depth() int {
	return len(d.Levels())
}

// Parent 返回当前领域的上级领域。
func (d Domain) Parent() Domain {
	levels := d.Levels()
	if len(levels) <= 1 {
		return ""
	}
	return Domain(strings.Join(levels[:len(levels)-1], domainSeparator))
}

// Match 判断当前领域是否等于或属于 target 领域。
func (d Domain) Match(target Domain) bool {
	if d == "" || target == "" {
		return false
	}
	currentLevels := d.Levels()
	targetLevels := target.Levels()
	if len(currentLevels) < len(targetLevels) {
		return false
	}
	for index, targetLevel := range targetLevels {
		if currentLevels[index] != targetLevel {
			return false
		}
	}
	return true
}

// Scope 描述一组可聚合检索的上下文作用域。
type Scope struct {
	Type ScopeType `json:"scope_type"`
	ID   string    `json:"scope_id"`
}

// Validate 校验 Scope 是否包含可用的类型和 ID。
func (s Scope) Validate() error {
	if !isSupportedScopeType(s.Type) {
		return errors.New("unsupported scope type")
	}
	if strings.TrimSpace(s.ID) == "" {
		return errors.New("scope id is empty")
	}
	return nil
}

// Source 描述一组上下文文档的来源。
type Source struct {
	Type     SourceType        `json:"source_type"`
	ID       string            `json:"source_id"`
	ToolName string            `json:"tool_name,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Validate 校验 Source 是否包含可用的类型和 ID。
func (s Source) Validate() error {
	if !isSupportedSourceType(s.Type) {
		return errors.New("unsupported source type")
	}
	if strings.TrimSpace(s.ID) == "" {
		return errors.New("source id is empty")
	}
	return nil
}

// CodeLocation 描述代码片段的位置，仅代码库来源需要设置。
type CodeLocation struct {
	RelativePath  string `json:"relative_path,omitempty"`
	StartLine     int    `json:"start_line,omitempty"`
	EndLine       int    `json:"end_line,omitempty"`
	FileExtension string `json:"file_extension,omitempty"`
	Language      string `json:"language,omitempty"`
}

// DocumentLocation 描述文档知识片段的章节位置和引用信息。
type DocumentLocation struct {
	DocumentID    string        `json:"document_id,omitempty"`
	SectionID     string        `json:"section_id,omitempty"`
	HeadingPath   string        `json:"heading_path,omitempty"`
	KnowledgeKind KnowledgeKind `json:"knowledge_kind,omitempty"`
	Version       string        `json:"version,omitempty"`
	Keywords      []string      `json:"keywords,omitempty"`
	Symbols       []string      `json:"symbols,omitempty"`
	RelativePath  string        `json:"relative_path,omitempty"`
	StartLine     int           `json:"start_line,omitempty"`
	EndLine       int           `json:"end_line,omitempty"`
}

// ContextDocument 表示可被索引和检索的通用上下文文档。
type ContextDocument struct {
	ID               string            `json:"id"`
	Namespace        string            `json:"namespace"`
	Scope            Scope             `json:"scope"`
	Source           Source            `json:"source"`
	ChunkType        ChunkType         `json:"chunk_type"`
	Vector           []float32         `json:"vector,omitempty"`
	Content          string            `json:"content"`
	Summary          string            `json:"summary,omitempty"`
	CodeLocation     *CodeLocation     `json:"code_location,omitempty"`
	DocumentLocation *DocumentLocation `json:"document_location,omitempty"`
	ConversationID   string            `json:"conversation_id,omitempty"`
	MessageID        string            `json:"message_id,omitempty"`
	Role             Role              `json:"role,omitempty"`
	Importance       float64           `json:"importance,omitempty"`
	CreatedAt        *time.Time        `json:"created_at,omitempty"`
	UpdatedAt        *time.Time        `json:"updated_at,omitempty"`
	Tags             []string          `json:"tags,omitempty"`
	ExperienceKind   ExperienceKind    `json:"experience_kind,omitempty"`
	Domain           Domain            `json:"domain,omitempty"`
	DomainConfidence float64           `json:"domain_confidence,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// Manifest 描述 source bundle 的版本、校验、embedding 和统计信息。
type Manifest struct {
	Scope              Scope             `json:"scope"`
	Source             Source            `json:"source"`
	Version            int64             `json:"version"`
	Checksum           string            `json:"checksum"`
	EmbeddingProvider  string            `json:"embedding_provider"`
	EmbeddingModel     string            `json:"embedding_model"`
	EmbeddingDimension int               `json:"embedding_dimension"`
	IndexedAt          time.Time         `json:"indexed_at"`
	DocumentCount      int               `json:"document_count"`
	MemoryCount        int               `json:"memory_count"`
	KnowledgeCount     int               `json:"knowledge_count"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

// Validate 校验 Manifest 的基础作用域、来源和 embedding 信息。
func (m Manifest) Validate() error {
	if err := m.Scope.Validate(); err != nil {
		return err
	}
	if err := m.Source.Validate(); err != nil {
		return err
	}
	if m.Version < 0 {
		return errors.New("manifest version is invalid")
	}
	if m.EmbeddingDimension < 0 {
		return errors.New("embedding dimension is invalid")
	}
	if m.DocumentCount < 0 {
		return errors.New("document count is invalid")
	}
	if m.MemoryCount < 0 {
		return errors.New("memory count is invalid")
	}
	if m.KnowledgeCount < 0 {
		return errors.New("knowledge count is invalid")
	}
	return nil
}

// NamespaceForScope 生成用于聚合作用域检索的稳定 namespace。
func NamespaceForScope(scope Scope) (string, error) {
	if err := scope.Validate(); err != nil {
		return "", err
	}
	return "context_" + string(scope.Type) + "_" + hashText(scope.ID), nil
}

// NamespaceForSource 生成用于 source shard 的稳定 namespace。
func NamespaceForSource(scope Scope, source Source) (string, error) {
	if err := scope.Validate(); err != nil {
		return "", err
	}
	if err := source.Validate(); err != nil {
		return "", err
	}
	key := strings.Join([]string{string(scope.Type), scope.ID, string(source.Type), source.ID}, "\x00")
	return "source_" + string(source.Type) + "_" + hashText(key), nil
}

// StableID 根据输入片段生成稳定 ID。
func StableID(prefix string, parts ...string) string {
	cleanPrefix := strings.TrimSpace(prefix)
	if cleanPrefix == "" {
		cleanPrefix = defaultIDPrefix
	}
	return cleanPrefix + "_" + hashText(strings.Join(parts, "\x00"))
}

func isSupportedScopeType(scopeType ScopeType) bool {
	switch scopeType {
	case ScopeTypeUser, ScopeTypeWorkspace, ScopeTypeCodebase, ScopeTypeTool:
		return true
	default:
		return false
	}
}

func isSupportedSourceType(sourceType SourceType) bool {
	switch sourceType {
	case SourceTypeCodebase, SourceTypeConversation, SourceTypeToolHistory,
		SourceTypePreference, SourceTypeExperience, SourceTypeFact,
		SourceTypeDocument, SourceTypeExternalKnowledge:
		return true
	default:
		return false
	}
}

func hashText(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])[:namespaceHashLength]
}

func splitDomainLevels(value string) []string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(value)), domainSeparator)
	levels := make([]string, 0, len(parts))
	for _, part := range parts {
		level := strings.TrimSpace(part)
		if level == "" {
			continue
		}
		level = strings.ReplaceAll(level, " ", "-")
		levels = append(levels, level)
	}
	return levels
}
