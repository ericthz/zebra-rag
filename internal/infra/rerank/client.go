// Package rerank 提供 cross-encoder 重排（Rerank）客户端。
// 对接独立的 rerank 服务（如 bge-reranker 的 HTTP 服务），
// 协议与主流 rerank API（Cohere/Jina）对齐：POST /rerank {model, query, documents, top_n}。
package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// RerankItem 表示一条待重排的候选文档。
type RerankItem struct {
	DocID string `json:"doc_id"`
	Text  string `json:"text"`
}

// RerankResult 表示单条重排结果。
type RerankResult struct {
	DocID string  `json:"doc_id"`
	Score float64 `json:"score"`
}

// RerankRequest 重排请求体。
type RerankRequest struct {
	Model     string       `json:"model"`
	Query     string       `json:"query"`
	Documents []RerankItem `json:"documents"`
	TopN      int          `json:"top_n,omitempty"`
}

// RerankResponse 重排响应体。
type RerankResponse struct {
	Results []RerankResult `json:"results"`
}

// Client 是重排服务的客户端。
type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

// NewClient 创建一个重排客户端。
// timeoutSeconds <= 0 时默认 5 秒。
func NewClient(baseURL, model string, timeoutSeconds int) *Client {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 5
	}
	return &Client{
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
	}
}

// Rerank 对候选文档按与 query 的相关性重排，返回按分数降序的结果。
func (c *Client) Rerank(ctx context.Context, query string, items []RerankItem, topN int) ([]RerankResult, error) {
	if len(items) == 0 {
		return nil, nil
	}
	req := RerankRequest{
		Model:     c.model,
		Query:     query,
		Documents: items,
		TopN:      topN,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/rerank", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rerank service returned status %d", resp.StatusCode)
	}
	var rr RerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return nil, err
	}
	return rr.Results, nil
}
