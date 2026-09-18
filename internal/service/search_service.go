// Package service 提供了搜索相关的业务逻辑。
package service

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ericthz/zebra-rag/internal/config"
	"github.com/ericthz/zebra-rag/internal/infra/database"
	"github.com/ericthz/zebra-rag/internal/infra/embedding"
	"github.com/ericthz/zebra-rag/internal/infra/rerank"
	"github.com/ericthz/zebra-rag/internal/infra/sparse"
	"github.com/ericthz/zebra-rag/internal/model"
	"github.com/ericthz/zebra-rag/internal/repository"
	"github.com/ericthz/zebra-rag/pkg/log"
	"github.com/ericthz/zebra-rag/pkg/metrics"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
)

// SearchService 接口定义了搜索操作。
type SearchService interface {
	HybridSearch(ctx context.Context, query string, topK int, user *model.User) ([]model.SearchResponseDTO, error)
}

type searchService struct {
	embeddingClient embedding.Client
	esClient        *elasticsearch.Client
	userService     UserService
	uploadRepo      repository.UploadRepository
	reranker        *rerank.Client // 可为 nil，未配置时跳过重排
	rewriter        QueryRewriter  // 可为 nil，未配置时跳过查询改写
	sparseClient    sparse.Client  // 可为 nil，未配置时跳过稀疏召回
}

// NewSearchService 创建一个新的 SearchService 实例。
func NewSearchService(embeddingClient embedding.Client, esClient *elasticsearch.Client, userService UserService, uploadRepo repository.UploadRepository, reranker *rerank.Client, rewriter QueryRewriter, sparseClient sparse.Client) SearchService {
	return &searchService{
		embeddingClient: embeddingClient,
		esClient:        esClient,
		userService:     userService,
		uploadRepo:      uploadRepo,
		reranker:        reranker,
		rewriter:        rewriter,
		sparseClient:    sparseClient,
	}
}

// esHit 是 Elasticsearch 单条命中（含 _source 与得分）。
type esHit struct {
	Score  float64          `json:"_score"`
	Source model.EsDocument `json:"_source"`
}

// esSearchResponse 封装 ES 搜索响应。
type esSearchResponse struct {
	Hits struct {
		Hits []esHit `json:"hits"`
	} `json:"hits"`
}

// HybridSearch 执行多路召回 + RRF 融合 + 可选重排的混合搜索。
// 召回路径：KNN（语义）+ BM25（关键词），两者互相独立，避免 must 强约束压制纯语义召回。
func (s *searchService) HybridSearch(ctx context.Context, query string, topK int, user *model.User) ([]model.SearchResponseDTO, error) {
	log.Infof("[SearchService] 开始混合搜索, query: '%s', topK: %d, user: %s", query, topK, user.Username)
	metrics.IncSearch()
	startAt := time.Now()

	// 0. 检索结果缓存（可配置 TTL，0 禁用）
	cacheEnabled := config.Conf.Search.CacheTTLSeconds > 0
	var cacheKey string
	if cacheEnabled {
		cacheKey = searchCacheKey(user.ID, topK, query)
		if cached, ok := s.getSearchCache(ctx, cacheKey); ok {
			log.Infof("[SearchService] 检索缓存命中: %s", cacheKey)
			return cached, nil
		}
	}

	// 1. 用户有效组织标签（含层级）
	userEffectiveTags, err := s.userService.GetUserEffectiveOrgTags(user)
	if err != nil {
		log.Errorf("[SearchService] 获取用户有效组织标签失败: %v", err)
		userEffectiveTags = []string{}
	}

	// 2. 可选 LLM 查询改写（口语 -> 检索友好查询；失败回退原查询）
	searchText := query
	if config.Conf.Search.QueryRewrite.Enabled && s.rewriter != nil {
		if rw, err := s.rewriter.Rewrite(ctx, query); err == nil && rw != "" {
			searchText = rw
		}
	}

	// 3. 检索查询集合：默认单查询，开启多查询扩展时生成多个变体（多路检索后 RRF 融合）
	searchQueries := []string{searchText}
	if config.Conf.Search.MultiQuery.Enabled && s.rewriter != nil {
		if variants, err := s.rewriter.RewriteMany(ctx, searchText, config.Conf.Search.MultiQuery.Count); err == nil && len(variants) > 1 {
			searchQueries = variants
		}
	}

	// 4. 多路召回（每个查询：KNN + BM25，均按访问权限过滤，各取 recall_k 个候选）
	recallK := config.Conf.Search.RecallK
	if recallK <= 0 {
		recallK = 60
	}
	var allLists [][]esHit
	for _, q := range searchQueries {
		queryVector, err := s.embeddingClient.CreateEmbedding(ctx, q)
		if err != nil {
			log.Errorf("[SearchService] 向量化查询 '%s' 失败: %v", q, err)
			if q == searchText {
				metrics.IncSearchError()
				return nil, fmt.Errorf("failed to create query embedding: %w", err)
			}
			continue
		}
		knnHits, err := s.knnSearch(ctx, queryVector, recallK, user, userEffectiveTags)
		if err != nil {
			log.Errorf("[SearchService] KNN 召回失败(%s): %v", q, err)
		} else {
			allLists = append(allLists, knnHits)
		}
		norm, phrase := normalizeQuery(q)
		bm25Hits, err := s.bm25Search(ctx, norm, phrase, recallK, user, userEffectiveTags)
		if err != nil {
			log.Errorf("[SearchService] BM25 召回失败(%s): %v", q, err)
		} else {
			allLists = append(allLists, bm25Hits)
		}
		// 稀疏向量召回（B3.3）：启用且服务可用时加入多路融合
		if s.sparseClient != nil && config.Conf.Sparse.Enabled {
			sv, svErr := s.sparseClient.CreateSparse(ctx, q)
			if svErr != nil {
				log.Warnf("[SearchService] 稀疏向量生成失败(%s): %v", q, svErr)
			} else {
				sparseHits, spErr := s.sparseSearch(ctx, sv, recallK, user, userEffectiveTags)
				if spErr != nil {
					log.Errorf("[SearchService] 稀疏召回失败(%s): %v", q, spErr)
				} else {
					allLists = append(allLists, sparseHits)
				}
			}
		}
	}

	// 5. RRF 融合多路结果（含多查询的多路列表）
	fused := rrfFuse(allLists, config.Conf.Search.RRFK)
	if len(fused) == 0 {
		log.Infof("[SearchService] 多路召回均无结果")
		return []model.SearchResponseDTO{}, nil
	}

	// 6. 可选 cross-encoder 重排（失败时回退到 RRF 排序）
	reranked := s.maybeRerank(ctx, query, fused)
	if len(reranked) > topK {
		reranked = reranked[:topK]
	}

	// 7. 批量获取文件名
	fileNameMap, err := s.buildFileNameMap(ctx, reranked)
	if err != nil {
		log.Errorf("[SearchService] 批量查询文件名失败: %v", err)
		metrics.IncSearchError()
		return nil, fmt.Errorf("批量查询文件名失败: %w", err)
	}

	// 8. 组装最终结果
	results := make([]model.SearchResponseDTO, 0, len(reranked))
	for _, hit := range reranked {
		fileName := fileNameMap[hit.Source.FileMD5]
		if fileName == "" {
			log.Warnf("[SearchService] 未找到 FileMD5 '%s' 对应的文件名，使用 '未知文件'", hit.Source.FileMD5)
			fileName = "未知文件"
		}
		results = append(results, model.SearchResponseDTO{
			FileMD5:     hit.Source.FileMD5,
			FileName:    fileName,
			ChunkID:     hit.Source.ChunkID,
			TextContent: hit.Source.TextContent,
			ParentText:  hit.Source.ParentText,
			Score:       hit.Score,
			UserID:      strconv.FormatUint(uint64(hit.Source.UserID), 10),
			OrgTag:      hit.Source.OrgTag,
			IsPublic:    hit.Source.IsPublic,
		})
	}
	if cacheEnabled {
		s.setSearchCache(ctx, cacheKey, results)
	}
	metrics.RecordSearchLatency(time.Since(startAt).Milliseconds())
	log.Infof("[SearchService] 混合搜索完成, 返回 %d 条", len(results))
	return results, nil
}

// searchCacheKey 生成检索缓存键。
func searchCacheKey(userID uint, topK int, query string) string {
	sum := md5.Sum([]byte(query))
	return fmt.Sprintf("search:cache:%d:%d:%s", userID, topK, hex.EncodeToString(sum[:]))
}

// getSearchCache 从 Redis 读取缓存结果。
func (s *searchService) getSearchCache(ctx context.Context, key string) ([]model.SearchResponseDTO, bool) {
	data, err := database.RDB.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}
	var res []model.SearchResponseDTO
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, false
	}
	return res, true
}

// setSearchCache 写入缓存结果。
func (s *searchService) setSearchCache(ctx context.Context, key string, res []model.SearchResponseDTO) {
	data, err := json.Marshal(res)
	if err != nil {
		return
	}
	ttl := time.Duration(config.Conf.Search.CacheTTLSeconds) * time.Second
	if err := database.RDB.Set(ctx, key, data, ttl).Err(); err != nil {
		log.Warnf("[SearchService] 写入检索缓存失败: %v", err)
	}
}

// sparseSearch 稀疏向量召回：rank_features 字段上的 token term 查询（带权重 boost）。
func (s *searchService) sparseSearch(ctx context.Context, sv sparse.SparseVector, recallK int, user *model.User, tags []string) ([]esHit, error) {
	if len(sv) == 0 || recallK <= 0 {
		return nil, nil
	}
	should := make([]map[string]interface{}, 0, len(sv))
	for token, w := range sv {
		should = append(should, map[string]interface{}{
			"term": map[string]interface{}{
				"sparse_vector." + token: map[string]interface{}{"value": 1, "boost": w},
			},
		})
	}
	body := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"filter": buildPermFilter(user, tags),
				"should": should,
			},
		},
		"size": recallK,
	}
	return s.execSearch(ctx, body)
}

// buildPermFilter 构建多租户访问过滤条件（本人 / 公开 / 组织标签命中其一）。
func buildPermFilter(user *model.User, tags []string) map[string]interface{} {
	return map[string]interface{}{
		"bool": map[string]interface{}{
			"should": []map[string]interface{}{
				{"term": map[string]interface{}{"user_id": user.ID}},
				{"term": map[string]interface{}{"is_public": true}},
				{"terms": map[string]interface{}{"org_tag": tags}},
			},
			"minimum_should_match": 1,
		},
	}
}

// knnSearch 语义召回：ES 8.10 顶层 knn + 权限过滤。
func (s *searchService) knnSearch(ctx context.Context, queryVector []float32, recallK int, user *model.User, tags []string) ([]esHit, error) {
	if recallK <= 0 {
		return nil, nil
	}
	// num_candidates 应显著大于 k，给 ANN 近似搜索留出候选余量（ES 要求 >= k）
	numCandidates := recallK * 4
	if numCandidates < recallK {
		numCandidates = recallK
	}
	body := map[string]interface{}{
		"knn": map[string]interface{}{
			"field":          "vector",
			"query_vector":   queryVector,
			"k":              recallK,
			"num_candidates": numCandidates,
			"filter":         buildPermFilter(user, tags),
		},
		"size": recallK,
	}
	return s.execSearch(ctx, body)
}

// bm25Search 关键词召回：BM25 match + 权限过滤 + 短语 boost。
func (s *searchService) bm25Search(ctx context.Context, normalized, phrase string, recallK int, user *model.User, tags []string) ([]esHit, error) {
	if recallK <= 0 {
		return nil, nil
	}
	body := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": map[string]interface{}{
					"match": map[string]interface{}{"text_content": normalized},
				},
				"filter": buildPermFilter(user, tags),
				"should": buildPhraseShould(phrase),
			},
		},
		"size": recallK,
	}
	return s.execSearch(ctx, body)
}

// execSearch 执行一次 ES 搜索并解码。
func (s *searchService) execSearch(ctx context.Context, body map[string]interface{}) ([]esHit, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, err
	}
	res, err := s.esClient.Search(
		s.esClient.Search.WithContext(ctx),
		s.esClient.Search.WithIndex(config.Conf.Elasticsearch.IndexName),
		s.esClient.Search.WithBody(&buf),
		s.esClient.Search.WithTrackTotalHits(true),
	)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, fmt.Errorf("es search error: %s", res.String())
	}
	var resp esSearchResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil, err
	}
	return resp.Hits.Hits, nil
}

// rrfFuse 使用 Reciprocal Rank Fusion 融合多路召回结果。
// score(id) = sum(1 / (k + rank))，rank 从 1 开始。
func rrfFuse(lists [][]esHit, k int) []esHit {
	if k <= 0 {
		k = 60
	}
	score := make(map[string]float64)
	byID := make(map[string]esHit)
	for _, list := range lists {
		for rank, hit := range list {
			id := hit.Source.VectorID
			score[id] += 1.0 / float64(k+rank+1)
			if _, ok := byID[id]; !ok {
				byID[id] = hit
			}
		}
	}
	out := make([]esHit, 0, len(score))
	for id, hit := range byID {
		hit.Score = score[id]
		out = append(out, hit)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// maybeRerank 若配置了重排服务，则对候选做 cross-encoder 精排；失败时回退到 RRF 排序。
func (s *searchService) maybeRerank(ctx context.Context, query string, candidates []esHit) []esHit {
	if s.reranker == nil || !config.Conf.Rerank.Enabled || len(candidates) == 0 {
		return candidates
	}
	topN := config.Conf.Rerank.TopN
	if topN <= 0 || topN > len(candidates) {
		topN = len(candidates)
	}
	items := make([]rerank.RerankItem, 0, topN)
	for i := 0; i < topN; i++ {
		items = append(items, rerank.RerankItem{DocID: candidates[i].Source.VectorID, Text: candidates[i].Source.TextContent})
	}
	results, err := s.reranker.Rerank(ctx, query, items, topN)
	if err != nil {
		log.Errorf("[SearchService] 重排失败，回退到 RRF 排序: %v", err)
		return candidates
	}
	scoreMap := make(map[string]float64)
	for _, r := range results {
		scoreMap[r.DocID] = r.Score
	}
	byID := make(map[string]esHit)
	for _, hit := range candidates {
		if sc, ok := scoreMap[hit.Source.VectorID]; ok {
			hit.Score = sc
		}
		byID[hit.Source.VectorID] = hit
	}
	out := make([]esHit, 0, len(byID))
	for _, hit := range byID {
		out = append(out, hit)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// buildFileNameMap 批量查询文件名（去重后一次查询，避免 N+1）。
func (s *searchService) buildFileNameMap(ctx context.Context, hits []esHit) (map[string]string, error) {
	unique := make(map[string]struct{})
	for _, h := range hits {
		unique[h.Source.FileMD5] = struct{}{}
	}
	md5s := make([]string, 0, len(unique))
	for m := range unique {
		md5s = append(md5s, m)
	}
	infos, err := s.uploadRepo.FindBatchByMD5s(md5s)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string)
	for _, info := range infos {
		m[info.FileMD5] = info.FileName
	}
	return m, nil
}

// normalizeQuery 对用户查询进行轻量去噪与短语提取。
// 返回值：规范化后的查询（用于 BM25）与核心短语（用于 match_phrase 兜底）。
func normalizeQuery(q string) (string, string) {
	if q == "" {
		return q, ""
	}
	lower := strings.ToLower(q)
	// 去除常见口语/功能词
	stopPhrases := []string{"是谁", "是什么", "是啥", "请问", "怎么", "如何", "告诉我", "严格", "按照", "不要补充", "的区别", "区别", "吗", "呢", "？", "?"}
	for _, sp := range stopPhrases {
		lower = strings.ReplaceAll(lower, sp, " ")
	}
	// 仅保留中文、英文、数字与空白
	reKeep := regexp.MustCompile(`[^\p{Han}a-z0-9\s]+`)
	kept := reKeep.ReplaceAllString(lower, " ")
	// 归一空白
	reSpace := regexp.MustCompile(`\s+`)
	kept = strings.TrimSpace(reSpace.ReplaceAllString(kept, " "))
	if kept == "" {
		return q, ""
	}
	return kept, kept
}

// buildPhraseShould 构建 match_phrase should 子句（带 boost），为空则返回 nil。
func buildPhraseShould(phrase string) interface{} {
	if phrase == "" {
		return nil
	}
	return []map[string]interface{}{
		{
			"match_phrase": map[string]interface{}{
				"text_content": map[string]interface{}{
					"query": phrase,
					"boost": 3.0,
				},
			},
		},
	}
}
