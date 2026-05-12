// 文件说明：提供向量检索结果的本地重排能力。
// 实现原理：复用本地 BM25 风格关键词分和 RRF/线性融合逻辑，对外部向量库返回的候选集进行二次排序。
// 使用方式：Milvus 等远端 VectorStore 可先取回较大的 dense 候选集，再调用 rerankSearchResults 进行混合排序。
// 注意事项：该重排只基于已取回候选集，不替代服务端 sparse vector / BM25 全量召回。
// 交互模块：internal/vectorstore/local_store.go、internal/vectorstore/milvus_store.go。
package vectorstore

import "sort"

func rerankSearchResults(results []SearchResult, options SearchOptions, limit int) []SearchResult {
	if len(results) == 0 {
		return nil
	}
	queryTokens := tokenizeSearchText(options.Query)
	candidates := make([]localSearchCandidate, 0, len(results))
	for _, result := range results {
		candidates = append(candidates, localSearchCandidate{
			document:      result.Document,
			semanticScore: result.Score,
		})
	}
	applyKeywordScores(candidates, queryTokens, options.KeywordProfile)
	semanticWeight, keywordWeight := searchWeights(options)
	applyCandidateScores(candidates, semanticWeight, keywordWeight, searchFusionMode(options))
	sort.SliceStable(candidates, func(i int, j int) bool {
		return compareLocalSearchCandidates(candidates[i], candidates[j])
	})

	rerankedResults := make([]SearchResult, 0, len(candidates))
	for _, candidate := range candidates {
		rerankedResults = append(rerankedResults, SearchResult{
			Document: candidate.document,
			Score:    candidate.score,
		})
	}
	if limit > 0 && len(rerankedResults) > limit {
		return rerankedResults[:limit]
	}
	return rerankedResults
}

func shouldRerankSearchResults(options SearchOptions) bool {
	return len(tokenizeSearchText(options.Query)) > 0
}
