// Package vision 提供图片理解（视觉）客户端，用于多模态入库（B1.2）。
// 兼容 OpenAI Chat Completions 视觉协议（image_url + base64），Ollama 视觉模型可直接对接。
package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/ericthz/zebra-rag/internal/config"
)

// Client 图片理解客户端。
type Client struct {
	baseURL string
	model   string
	prompt  string
	http    *http.Client
}

// NewClient 创建一个视觉客户端。
func NewClient(cfg config.VisionConfig) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		model:   cfg.Model,
		prompt:  cfg.Prompt,
		http:    &http.Client{Timeout: time.Duration(timeout) * time.Second},
	}
}

type contentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

type visionRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string        `json:"role"`
		Content []contentPart `json:"content"`
	} `json:"messages"`
}

type visionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Caption 对图片字节生成描述（多模态入库：图片 -> 描述文本 -> 可检索）。
func (c *Client) Caption(ctx context.Context, imageName string, imageBytes []byte) (string, error) {
	mime := mimeByExt(imageName)
	dataURL := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(imageBytes)

	req := visionRequest{Model: c.model}
	req.Messages = make([]struct {
		Role    string        `json:"role"`
		Content []contentPart `json:"content"`
	}, 1)
	req.Messages[0].Role = "user"
	req.Messages[0].Content = []contentPart{
		{
			Type: "image_url",
			ImageURL: &struct {
				URL string `json:"url"`
			}{URL: dataURL},
		},
		{Type: "text", Text: c.prompt},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vision service returned %d", resp.StatusCode)
	}
	var vr visionResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		return "", err
	}
	if len(vr.Choices) == 0 {
		return "", fmt.Errorf("vision service returned empty choices")
	}
	return strings.TrimSpace(vr.Choices[0].Message.Content), nil
}

func mimeByExt(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "image/jpeg"
	}
}
