// Package eval 提供 RAG 离线评测：数据集加载与检索/生成指标计算。
//
// 指标说明（轻量实现，无需 LLM 裁判，便于作为回归门禁）：
//   - 检索层：recall@k、MRR、hit-rate（命中是否召回相关文档）
//   - 生成层：faithfulness（答案内容是否来源于参考上下文）、answer_relevancy（答案是否覆盖参考答案）
//   - 上下文层：context_precision / context_recall
//
// 后续可扩展 LLM-as-Judge 以提升指标鲁棒性。
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// EvalItem 一条评测样本。
type EvalItem struct {
	Question         string   `json:"question"`
	ExpectedDocs     []string `json:"expected_docs"`     // 相关文档 ID（fileMd5 或 vector_id）
	ReferenceAnswer  string   `json:"reference_answer"`  // 参考答案（用于生成层指标）
	ReferenceContext string   `json:"reference_context"` // 参考上下文（用于 faithfulness）
}

// Dataset 评测数据集。
type Dataset struct {
	Name  string     `json:"name"`
	Items []EvalItem `json:"items"`
}

// LoadDataset 从 JSON 文件加载评测数据集。
func LoadDataset(path string) (*Dataset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ds Dataset
	if err := json.Unmarshal(data, &ds); err != nil {
		return nil, err
	}
	if len(ds.Items) == 0 {
		return nil, fmt.Errorf("评测数据集为空: %s", path)
	}
	return &ds, nil
}

// RetrievalMetrics 检索层指标。
type RetrievalMetrics struct {
	RecallAtK float64 // 平均 recall@k
	MRR       float64 // 平均 MRR（首个相关文档排位）
	HitRate   float64 // 至少命中一个相关文档的比例
	Queries   int
}

// GenerationMetrics 生成层指标（关键词启发式）。
type GenerationMetrics struct {
	Faithfulness    float64 // 答案中的有效词项在参考上下文中出现的比例
	AnswerRelevancy float64 // 参考答案关键词在答案中出现的比例
	Queries         int
}

// ContextMetrics 上下文层指标。
type ContextMetrics struct {
	ContextPrecision float64 // 相关文档在检索结果中的位置加权精度
	ContextRecall    float64 // 相关文档被召回的覆盖度
}

// ComputeRetrieval 计算检索层指标。
// retrieved：每个 query 按序返回的文档 ID 列表；expected：每个 query 的相关文档 ID 集合。
func ComputeRetrieval(retrieved [][]string, expected [][]string, k int) RetrievalMetrics {
	if k <= 0 {
		k = 10
	}
	m := RetrievalMetrics{Queries: len(retrieved)}
	if m.Queries == 0 {
		return m
	}
	var recallSum, mrrSum, hitSum float64
	for i := range retrieved {
		exp := toSet(expected[i])
		if len(exp) == 0 {
			continue
		}
		top := retrieved[i]
		if len(top) > k {
			top = top[:k]
		}
		// recall@k
		hitCount := 0
		for _, id := range top {
			if exp[id] {
				hitCount++
			}
		}
		recall := float64(hitCount) / float64(len(exp))
		recallSum += recall
		// MRR
		rr := 0.0
		for rank, id := range top {
			if exp[id] {
				rr = 1.0 / float64(rank+1)
				break
			}
		}
		mrrSum += rr
		if hitCount > 0 {
			hitSum++
		}
	}
	valid := float64(len(retrieved))
	m.RecallAtK = recallSum / valid
	m.MRR = mrrSum / valid
	m.HitRate = hitSum / valid
	return m
}

// ComputeGeneration 计算生成层指标（关键词启发式）。
// answers：每个 query 的生成答案；expected：每个 query 的参考答案。
func ComputeGeneration(answers []string, expected []string) GenerationMetrics {
	m := GenerationMetrics{Queries: len(answers)}
	if m.Queries == 0 {
		return m
	}
	var faithSum, relevSum float64
	for i := range answers {
		refKeywords := extractTerms(expected[i])
		answerTerms := extractTerms(answers[i])
		refSet := toSet(refKeywords)
		// faithfulness：答案词项中出现在参考上下文的比例
		if len(answerTerms) > 0 {
			covered := 0
			for _, t := range answerTerms {
				if refSet[t] {
					covered++
				}
			}
			faithSum += float64(covered) / float64(len(answerTerms))
		}
		// answer_relevancy：参考答案关键词在答案中出现的比例
		if len(refKeywords) > 0 {
			answerSet := toSet(answerTerms)
			covered := 0
			for _, t := range refKeywords {
				if answerSet[t] {
					covered++
				}
			}
			relevSum += float64(covered) / float64(len(refKeywords))
		}
	}
	m.Faithfulness = faithSum / float64(m.Queries)
	m.AnswerRelevancy = relevSum / float64(m.Queries)
	return m
}

// ComputeContext 计算上下文层指标。
func ComputeContext(retrieved [][]string, expected [][]string, k int) ContextMetrics {
	cm := ContextMetrics{}
	if len(retrieved) == 0 {
		return cm
	}
	if k <= 0 {
		k = 10
	}
	var precSum, recallSum float64
	for i := range retrieved {
		exp := toSet(expected[i])
		if len(exp) == 0 {
			continue
		}
		top := retrieved[i]
		if len(top) > k {
			top = top[:k]
		}
		// context recall：相关文档被召回的覆盖
		hit := 0
		for _, id := range top {
			if exp[id] {
				hit++
			}
		}
		recallSum += float64(hit) / float64(len(exp))
		// context precision（简化）：相关文档在 top-k 中的占比
		precSum += float64(hit) / float64(len(top))
	}
	n := float64(len(retrieved))
	cm.ContextPrecision = precSum / n
	cm.ContextRecall = recallSum / n
	return cm
}

// extractTerms 从文本抽取有效词项：连续中文片段（>=2）与英文单词（>=3），去停用词。
func extractTerms(text string) []string {
	text = strings.ToLower(text)
	// 中文片段
	reCN := regexp.MustCompile(`[\p{Han}]{2,}`)
	// 英文单词
	reEN := regexp.MustCompile(`[a-z]{3,}`)
	var terms []string
	for _, m := range reCN.FindAllString(text, -1) {
		terms = append(terms, m)
	}
	for _, m := range reEN.FindAllString(text, -1) {
		terms = append(terms, m)
	}
	stop := map[string]bool{"这个": true, "那个": true, "什么": true, "如何": true, "怎么": true, "以及": true, "进行": true, "一个": true, "我们": true, "the": true, "and": true, "for": true, "with": true}
	filtered := terms[:0]
	for _, t := range terms {
		if !stop[t] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func toSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, it := range items {
		set[strings.TrimSpace(it)] = true
	}
	return set
}
