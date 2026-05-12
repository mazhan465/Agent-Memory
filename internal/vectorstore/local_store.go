// 文件说明：实现基于本地 JSON 文件的向量存储。
// 实现原理：每个 namespace 对应一个 JSON 文件，检索时加载全部文档并计算余弦相似度。
// 使用方式：MVP 默认使用 LocalStore，后续可替换为 MilvusVectorStore。
// 注意事项：本实现适合小规模开发验证，不适合大型代码库生产检索。
// 交互模块：internal/indexer、internal/searcher、internal/config。

package vectorstore

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const vectorDirName = "vectors"

const (
	defaultRRFK = 60
	bm25K1      = 1.2
	bm25B       = 0.75
)

const (
	metadataSymbolName = "symbol_name"
	metadataSymbolKind = "symbol_kind"
	metadataChunkKind  = "chunk_kind"
	metadataRole       = "role"
	metadataToolName   = "tool_name"
	metadataCommand    = "command"
	metadataStatus     = "status"
	metadataTags       = "tags"
)

// LocalStore 使用 JSON 文件持久化向量文档。
type LocalStore struct {
	storageDir string
}

// NewLocalStore 创建本地向量存储。
func NewLocalStore(storageDir string) *LocalStore {
	return &LocalStore{storageDir: storageDir}
}

// Put 覆盖写入指定 namespace 的向量文档。
func (s *LocalStore) Put(ctx context.Context, namespace string, documents []Document) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return s.save(namespace, documents)
}

// ReplaceFiles 替换指定 namespace 中若干文件对应的向量文档。
func (s *LocalStore) ReplaceFiles(
	ctx context.Context,
	namespace string,
	relativePaths []string,
	documents []Document,
) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	existingDocuments, err := s.load(namespace)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	deleteSet := makeStringSet(relativePaths)
	mergedDocuments := make([]Document, 0, len(existingDocuments)+len(documents))
	for _, document := range existingDocuments {
		if _, ok := deleteSet[document.RelativePath]; ok {
			continue
		}
		mergedDocuments = append(mergedDocuments, document)
	}
	mergedDocuments = append(mergedDocuments, documents...)
	sortDocuments(mergedDocuments)
	return s.save(namespace, mergedDocuments)
}

// Search 在指定 namespace 中执行本地混合检索。
func (s *LocalStore) Search(ctx context.Context, namespace string, queryVector []float32, options SearchOptions) ([]SearchResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	documents, err := s.load(namespace)
	if err != nil {
		return nil, err
	}

	extensionSet := makeStringSet(options.ExtensionFilters)
	metadataFilters := metadataFilterSets(options)
	queryTokens := tokenizeSearchText(options.Query)
	candidates := make([]localSearchCandidate, 0, len(documents))
	for _, document := range documents {
		if len(extensionSet) > 0 {
			if _, ok := extensionSet[document.FileExtension]; !ok {
				continue
			}
		}
		if !matchMetadataFilters(document.Metadata, metadataFilters) {
			continue
		}
		candidates = append(candidates, localSearchCandidate{
			document:      document,
			semanticScore: cosineSimilarity(queryVector, document.Vector),
		})
	}

	applyKeywordScores(candidates, queryTokens, options.KeywordProfile)
	semanticWeight, keywordWeight := searchWeights(options)
	applyCandidateScores(candidates, semanticWeight, keywordWeight, searchFusionMode(options))
	sort.SliceStable(candidates, func(i int, j int) bool {
		return compareLocalSearchCandidates(candidates[i], candidates[j])
	})
	results := make([]SearchResult, 0, len(candidates))
	for _, candidate := range candidates {
		results = append(results, SearchResult{
			Document: candidate.document,
			Score:    candidate.score,
		})
	}
	if options.Limit > 0 && len(results) > options.Limit {
		results = results[:options.Limit]
	}
	return results, nil
}

// Clear 删除指定 namespace 的本地向量数据。
func (s *LocalStore) Clear(ctx context.Context, namespace string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	err := os.Remove(s.namespacePath(namespace))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Count 返回指定 namespace 的文档数量。
func (s *LocalStore) Count(ctx context.Context, namespace string) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}
	documents, err := s.load(namespace)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return len(documents), nil
}

func (s *LocalStore) load(namespace string) ([]Document, error) {
	data, err := os.ReadFile(s.namespacePath(namespace))
	if err != nil {
		return nil, err
	}
	var documents []Document
	if err := json.Unmarshal(data, &documents); err != nil {
		return nil, err
	}
	return documents, nil
}

func (s *LocalStore) save(namespace string, documents []Document) error {
	if err := os.MkdirAll(s.vectorDir(), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(documents, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.namespacePath(namespace), data, 0644)
}

func sortDocuments(documents []Document) {
	sort.SliceStable(documents, func(i int, j int) bool {
		left := documents[i]
		right := documents[j]
		if left.RelativePath != right.RelativePath {
			return left.RelativePath < right.RelativePath
		}
		if left.StartLine != right.StartLine {
			return left.StartLine < right.StartLine
		}
		if left.EndLine != right.EndLine {
			return left.EndLine < right.EndLine
		}
		return left.ID < right.ID
	})
}

func (s *LocalStore) vectorDir() string {
	return filepath.Join(s.storageDir, vectorDirName)
}

func (s *LocalStore) namespacePath(namespace string) string {
	return filepath.Join(s.vectorDir(), namespace+".json")
}

func metadataFilterSets(options SearchOptions) map[string]map[string]struct{} {
	filters := map[string]map[string]struct{}{
		MetadataDomainPath:    makeStringSet(options.DomainFilters),
		MetadataDocumentID:    makeStringSet(options.DocumentFilters),
		MetadataSectionID:     makeStringSet(options.SectionFilters),
		MetadataHeadingPath:   makeStringSet(options.HeadingFilters),
		MetadataKnowledgeKind: makeStringSet(options.KnowledgeKindFilters),
		MetadataNodeKind:      makeStringSet(options.NodeKindFilters),
		MetadataVersion:       makeStringSet(options.VersionFilters),
	}
	return filters
}

func matchMetadataFilters(metadata map[string]string, filters map[string]map[string]struct{}) bool {
	for key, allowedValues := range filters {
		if len(allowedValues) == 0 {
			continue
		}
		if _, ok := allowedValues[metadata[key]]; !ok {
			return false
		}
	}
	return true
}

func searchWeights(options SearchOptions) (float64, float64) {
	if strings.TrimSpace(options.Query) == "" {
		return 1, 0
	}
	semanticWeight := options.SemanticWeight
	keywordWeight := options.KeywordWeight
	if semanticWeight <= 0 && keywordWeight <= 0 {
		return 0.75, 0.25
	}
	if semanticWeight < 0 {
		semanticWeight = 0
	}
	if keywordWeight < 0 {
		keywordWeight = 0
	}
	total := semanticWeight + keywordWeight
	if total == 0 {
		return 0.75, 0.25
	}
	return semanticWeight / total, keywordWeight / total
}

type localSearchCandidate struct {
	document      Document
	semanticScore float64
	keywordScore  float64
	semanticRank  int
	keywordRank   int
	score         float64
}

type keywordCorpus struct {
	documents        []map[string]float64
	documentLengths  []float64
	documentFreq     map[string]int
	averageDocLength float64
}

type keywordFieldWeights struct {
	Content         float64
	RelativePath    float64
	FileExtension   float64
	Language        float64
	DefaultMetadata float64
	Metadata        map[string]float64
}

type weightedSearchField struct {
	text   string
	weight float64
}

func combinedSearchScore(semanticScore float64, keywordScore float64, semanticWeight float64, keywordWeight float64) float64 {
	if keywordScore == 0 {
		return semanticScore * semanticWeight
	}
	return semanticScore*semanticWeight + keywordScore*keywordWeight
}

func applyKeywordScores(candidates []localSearchCandidate, queryTokens []string, profile string) {
	if len(candidates) == 0 || len(queryTokens) == 0 {
		return
	}
	corpus := newKeywordCorpus(candidates, normalizeKeywordProfile(profile))
	for index := range candidates {
		candidates[index].keywordScore = corpus.score(queryTokens, index)
	}
}

func applyCandidateScores(candidates []localSearchCandidate, semanticWeight float64, keywordWeight float64, fusionMode string) {
	if fusionMode == SearchFusionRRF {
		assignSemanticRanks(candidates)
		assignKeywordRanks(candidates)
		for index := range candidates {
			candidates[index].score = semanticWeight*rrfRankScore(candidates[index].semanticRank) +
				keywordWeight*rrfRankScore(candidates[index].keywordRank)
		}
		return
	}
	for index := range candidates {
		candidates[index].score = combinedSearchScore(
			candidates[index].semanticScore,
			candidates[index].keywordScore,
			semanticWeight,
			keywordWeight,
		)
	}
}

func assignSemanticRanks(candidates []localSearchCandidate) {
	indices := sortedCandidateIndices(candidates, func(candidate localSearchCandidate) float64 {
		return candidate.semanticScore
	})
	for rank, index := range indices {
		candidates[index].semanticRank = rank + 1
	}
}

func assignKeywordRanks(candidates []localSearchCandidate) {
	indices := sortedCandidateIndices(candidates, func(candidate localSearchCandidate) float64 {
		return candidate.keywordScore
	})
	rank := 1
	for _, index := range indices {
		if candidates[index].keywordScore <= 0 {
			continue
		}
		candidates[index].keywordRank = rank
		rank++
	}
}

func sortedCandidateIndices(candidates []localSearchCandidate, score func(localSearchCandidate) float64) []int {
	indices := make([]int, 0, len(candidates))
	for index := range candidates {
		indices = append(indices, index)
	}
	sort.SliceStable(indices, func(i int, j int) bool {
		left := candidates[indices[i]]
		right := candidates[indices[j]]
		if score(left) != score(right) {
			return score(left) > score(right)
		}
		return compareDocuments(left.document, right.document)
	})
	return indices
}

func rrfRankScore(rank int) float64 {
	if rank <= 0 {
		return 0
	}
	return 1 / float64(defaultRRFK+rank)
}

func compareLocalSearchCandidates(left localSearchCandidate, right localSearchCandidate) bool {
	if left.score != right.score {
		return left.score > right.score
	}
	return compareDocuments(left.document, right.document)
}

func compareDocuments(left Document, right Document) bool {
	if left.RelativePath != right.RelativePath {
		return left.RelativePath < right.RelativePath
	}
	if left.StartLine != right.StartLine {
		return left.StartLine < right.StartLine
	}
	if left.EndLine != right.EndLine {
		return left.EndLine < right.EndLine
	}
	return left.ID < right.ID
}

func newKeywordCorpus(candidates []localSearchCandidate, profile string) keywordCorpus {
	corpus := keywordCorpus{
		documents:       make([]map[string]float64, 0, len(candidates)),
		documentLengths: make([]float64, 0, len(candidates)),
		documentFreq:    make(map[string]int),
	}
	var totalLength float64
	for _, candidate := range candidates {
		frequencies, length := weightedDocumentTokens(candidate.document, profile)
		corpus.documents = append(corpus.documents, frequencies)
		corpus.documentLengths = append(corpus.documentLengths, length)
		totalLength += length
		for token := range frequencies {
			corpus.documentFreq[token]++
		}
	}
	if len(corpus.documentLengths) > 0 {
		corpus.averageDocLength = totalLength / float64(len(corpus.documentLengths))
	}
	return corpus
}

func (c keywordCorpus) score(queryTokens []string, documentIndex int) float64 {
	if c.averageDocLength <= 0 || documentIndex < 0 || documentIndex >= len(c.documents) {
		return 0
	}
	frequencies := c.documents[documentIndex]
	documentLength := c.documentLengths[documentIndex]
	if len(frequencies) == 0 || documentLength <= 0 {
		return 0
	}
	var score float64
	for _, token := range uniqueStrings(queryTokens) {
		termFrequency := frequencies[token]
		if termFrequency <= 0 {
			continue
		}
		documentFrequency := c.documentFreq[token]
		if documentFrequency <= 0 {
			continue
		}
		inverseDocumentFrequency := math.Log(1 + (float64(len(c.documents))-float64(documentFrequency)+0.5)/
			(float64(documentFrequency)+0.5))
		normalizedLength := documentLength / c.averageDocLength
		denominator := termFrequency + bm25K1*(1-bm25B+bm25B*normalizedLength)
		score += inverseDocumentFrequency * (termFrequency * (bm25K1 + 1) / denominator)
	}
	if score <= 0 {
		return 0
	}
	return score / (score + 1)
}

func weightedDocumentTokens(document Document, profile string) (map[string]float64, float64) {
	weights := keywordFieldWeightsForProfile(profile)
	fields := weightedFieldsForDocument(document, weights)
	frequencies := make(map[string]float64)
	var length float64
	for _, field := range fields {
		if field.weight <= 0 || strings.TrimSpace(field.text) == "" {
			continue
		}
		for _, token := range tokenizeSearchTextAll(field.text) {
			frequencies[token] += field.weight
			length += field.weight
		}
	}
	return frequencies, length
}

func weightedFieldsForDocument(document Document, weights keywordFieldWeights) []weightedSearchField {
	fields := []weightedSearchField{
		{text: document.Content, weight: weights.Content},
		{text: document.RelativePath, weight: weights.RelativePath},
		{text: document.FileExtension, weight: weights.FileExtension},
		{text: document.Language, weight: weights.Language},
	}
	usedMetadata := make(map[string]struct{}, len(weights.Metadata))
	for key, weight := range weights.Metadata {
		fields = append(fields, weightedSearchField{text: document.Metadata[key], weight: weight})
		usedMetadata[key] = struct{}{}
	}
	for key, value := range document.Metadata {
		if _, ok := usedMetadata[key]; ok {
			continue
		}
		fields = append(fields, weightedSearchField{text: value, weight: weights.DefaultMetadata})
	}
	return fields
}

func keywordFieldWeightsForProfile(profile string) keywordFieldWeights {
	switch profile {
	case KeywordProfileCode:
		return keywordFieldWeights{
			Content:         1.0,
			RelativePath:    2.0,
			FileExtension:   0.4,
			Language:        0.4,
			DefaultMetadata: 0.5,
			Metadata: map[string]float64{
				metadataSymbolName:     3.0,
				metadataSymbolKind:     1.2,
				metadataChunkKind:      0.5,
				MetadataDomainPath:     0.8,
				MetadataExperienceKind: 0.4,
			},
		}
	case KeywordProfileKnowledge:
		return keywordFieldWeights{
			Content:         1.0,
			RelativePath:    0.6,
			FileExtension:   0.2,
			Language:        0.2,
			DefaultMetadata: 0.4,
			Metadata: map[string]float64{
				MetadataHeadingPath:   2.0,
				MetadataDocumentID:    1.0,
				MetadataSectionID:     0.8,
				MetadataKnowledgeKind: 0.6,
				MetadataNodeKind:      0.3,
				MetadataVersion:       0.2,
			},
		}
	case KeywordProfileConversation:
		return keywordFieldWeights{
			Content:         1.0,
			RelativePath:    0.1,
			FileExtension:   0.0,
			Language:        0.0,
			DefaultMetadata: 0.2,
			Metadata: map[string]float64{
				metadataRole:       0.2,
				MetadataDomainPath: 0.4,
			},
		}
	case KeywordProfileExperience:
		return keywordFieldWeights{
			Content:         1.0,
			RelativePath:    0.2,
			FileExtension:   0.0,
			Language:        0.0,
			DefaultMetadata: 0.4,
			Metadata: map[string]float64{
				MetadataDomainPath:     1.4,
				MetadataExperienceKind: 1.2,
				metadataTags:           1.0,
			},
		}
	case KeywordProfilePreference:
		return keywordFieldWeights{
			Content:         1.2,
			RelativePath:    0.1,
			FileExtension:   0.0,
			Language:        0.0,
			DefaultMetadata: 0.3,
			Metadata: map[string]float64{
				MetadataDomainPath: 0.8,
				metadataTags:       0.6,
			},
		}
	case KeywordProfileToolHistory:
		return keywordFieldWeights{
			Content:         1.0,
			RelativePath:    0.2,
			FileExtension:   0.0,
			Language:        0.0,
			DefaultMetadata: 0.4,
			Metadata: map[string]float64{
				metadataToolName:   1.5,
				metadataCommand:    0.8,
				metadataStatus:     0.8,
				MetadataDomainPath: 0.6,
			},
		}
	case KeywordProfileFact:
		return keywordFieldWeights{
			Content:         1.1,
			RelativePath:    0.1,
			FileExtension:   0.0,
			Language:        0.0,
			DefaultMetadata: 0.4,
			Metadata: map[string]float64{
				MetadataDomainPath: 0.8,
			},
		}
	default:
		return keywordFieldWeights{
			Content:         1.0,
			RelativePath:    0.4,
			FileExtension:   0.1,
			Language:        0.1,
			DefaultMetadata: 0.3,
			Metadata:        map[string]float64{},
		}
	}
}

func normalizeKeywordProfile(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case KeywordProfileCode:
		return KeywordProfileCode
	case KeywordProfileKnowledge:
		return KeywordProfileKnowledge
	case KeywordProfileConversation:
		return KeywordProfileConversation
	case KeywordProfileExperience:
		return KeywordProfileExperience
	case KeywordProfilePreference:
		return KeywordProfilePreference
	case KeywordProfileToolHistory:
		return KeywordProfileToolHistory
	case KeywordProfileFact:
		return KeywordProfileFact
	default:
		return KeywordProfileDefault
	}
}

func searchFusionMode(options SearchOptions) string {
	if strings.EqualFold(strings.TrimSpace(options.FusionMode), SearchFusionRRF) {
		return SearchFusionRRF
	}
	return SearchFusionLinear
}

func tokenizeSearchText(text string) []string {
	return uniqueStrings(tokenizeSearchTextAll(text))
}

func tokenizeSearchTextAll(text string) []string {
	tokens := make([]string, 0)
	var builder strings.Builder
	flush := func() {
		if builder.Len() == 0 {
			return
		}
		token := strings.ToLower(builder.String())
		if len([]rune(token)) > 1 {
			tokens = append(tokens, token)
		}
		builder.Reset()
	}
	var previous rune
	for _, current := range text {
		if !unicode.IsLetter(current) && !unicode.IsDigit(current) {
			flush()
			previous = 0
			continue
		}
		if previous != 0 && unicode.IsLower(previous) && unicode.IsUpper(current) {
			flush()
		}
		builder.WriteRune(current)
		previous = current
	}
	flush()
	return tokens
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func makeStringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

func cosineSimilarity(left []float32, right []float32) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}

	var dotProduct float64
	var leftNorm float64
	var rightNorm float64
	for i := range left {
		leftValue := float64(left[i])
		rightValue := float64(right[i])
		dotProduct += leftValue * rightValue
		leftNorm += leftValue * leftValue
		rightNorm += rightValue * rightValue
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dotProduct / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}
