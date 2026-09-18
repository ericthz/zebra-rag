// Package sparse 提供稀疏向量（BGE-M3 sparse / SPLADE）客户端，用于多路召回中的稀疏检索（B3.3）。
package sparse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// SparseVector 稀疏向量：token -> 权重。
type SparseVector map[string]float32

// Client 稀疏向量生成接口。
type Client interface {
	CreateSparse(ctx context.Context, text string) (SparseVector, error)
}

type sparseRequest struct {
	Model string   `json:"model"`
	Texts []string `json:"texts"`
}

type sparseResponse struct {
	Data []struct {
		Tokens []string  `json:"tokens"`
		Values []float32 `json:"values"`
	} `json:"data"`
}

type httpClient struct {
	baseURL string
	model   string
	http    *http.Client
}

// NewClient 创建一个稀疏向量 HTTP 客户端。
func NewClient(baseURL, model string, timeoutSeconds int) Client {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 10
	}
	return &httpClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		http:    &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
	}
}

// CreateSparse 对文本生成稀疏向量（token -> 权重）。
func (c *httpClient) CreateSparse(ctx context.Context, text string) (SparseVector, error) {
	body, err := json.Marshal(sparseRequest{Model: c.model, Texts: []string{text}})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/sparse", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sparse service returned %d", resp.StatusCode)
	}
	var sr sparseResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, err
	}
	if len(sr.Data) == 0 {
		return nil, fmt.Errorf("sparse service returned empty data")
	}
	out := make(SparseVector, len(sr.Data[0].Tokens))
	for i, tk := range sr.Data[0].Tokens {
		if i < len(sr.Data[0].Values) {
			out[tk] = sr.Data[0].Values[i]
		}
	}
	return out, nil
}
