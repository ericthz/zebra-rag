// Package embedding provides a client for interacting with embedding models.
package embedding

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ericthz/zebra-rag/internal/config"
	"github.com/ericthz/zebra-rag/internal/infra/database"
	"github.com/ericthz/zebra-rag/pkg/log"
)

// Client defines the interface for an embedding client.
type Client interface {
	CreateEmbedding(ctx context.Context, text string) ([]float32, error)
}

type openAICompatibleClient struct {
	cfg    config.EmbeddingConfig
	client *http.Client
}

// NewClient creates a new embedding client based on the provider in the config.
func NewClient(cfg config.EmbeddingConfig) Client {
	return &openAICompatibleClient{
		cfg: cfg,
		// 单次请求整体超时，避免模型服务无响应时挂死
		client: &http.Client{Timeout: 2 * time.Minute},
	}
}

type embeddingRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// CreateEmbedding calls the OpenAI-compatible API to get the vector for a given text.
func (c *openAICompatibleClient) CreateEmbedding(ctx context.Context, text string) ([]float32, error) {
	// Embedding 结果缓存（相同文本不重复调用，显著降低大文件处理成本）
	ttl := config.Conf.Embedding.CacheTTLSeconds
	if ttl > 0 {
		key := "embed:cache:" + md5Hex(text)
		if cached, ok := getCachedEmbedding(ctx, key); ok {
			return cached, nil
		}
	}

	log.Infof("[EmbeddingClient] 开始调用 Embedding API, model: %s, input_len: %d", c.cfg.Model, len(text))
	reqBody := embeddingRequest{
		Model:      c.cfg.Model, // Use model from config
		Input:      []string{text},
		Dimensions: c.cfg.Dimensions, // Use dimensions from config
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.BaseURL+"/embeddings", bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		log.Errorf("[EmbeddingClient] 调用 Embedding API 失败, error: %v", err)
		return nil, fmt.Errorf("failed to call embedding api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Errorf("[EmbeddingClient] Embedding API 返回非 200 状态码: %s", resp.Status)
		return nil, fmt.Errorf("embedding api returned non-200 status: %s", resp.Status)
	}

	var embeddingResp embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embeddingResp); err != nil {
		log.Errorf("[EmbeddingClient] 解析 Embedding API 响应失败, error: %v", err)
		return nil, fmt.Errorf("failed to decode embedding response: %w", err)
	}

	if len(embeddingResp.Data) == 0 || len(embeddingResp.Data[0].Embedding) == 0 {
		log.Warnf("[EmbeddingClient] Embedding API 返回了空的向量数据")
		return nil, fmt.Errorf("received empty embedding from api")
	}

	log.Infof("[EmbeddingClient] 成功从 Embedding API 获取向量, 维度: %d", len(embeddingResp.Data[0].Embedding))
	vector := embeddingResp.Data[0].Embedding
	if ttl > 0 {
		setCachedEmbedding(ctx, "embed:cache:"+md5Hex(text), vector, time.Duration(ttl)*time.Second)
	}
	return vector, nil
}

func md5Hex(text string) string {
	sum := md5.Sum([]byte(text))
	return hex.EncodeToString(sum[:])
}

func getCachedEmbedding(ctx context.Context, key string) ([]float32, bool) {
	data, err := database.RDB.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}
	var v []float32
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, false
	}
	return v, true
}

func setCachedEmbedding(ctx context.Context, key string, v []float32, ttl time.Duration) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	if err := database.RDB.Set(ctx, key, data, ttl).Err(); err != nil {
		log.Warnf("[EmbeddingClient] 写入 Embedding 缓存失败: %v", err)
	}
}
