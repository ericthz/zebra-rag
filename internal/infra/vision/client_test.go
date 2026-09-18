package vision

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ericthz/zebra-rag/internal/config"
)

// mockTransport 返回固定响应，避免依赖网络端口（沙箱限制）。
type mockTransport struct {
	respBody string
}

func (m *mockTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	var req visionRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Model != "qwen2.5-vl" {
		return nil, &mockErr{msg: "unexpected model " + req.Model}
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Content) != 2 {
		return nil, &mockErr{msg: "unexpected message shape"}
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(m.respBody)),
	}, nil
}

type mockErr struct{ msg string }

func (e *mockErr) Error() string { return e.msg }

func TestCaption(t *testing.T) {
	c := NewClient(config.VisionConfig{Enabled: true, BaseURL: "http://mock", Model: "qwen2.5-vl", Prompt: "描述图片", Timeout: 5})
	c.http = &http.Client{Transport: &mockTransport{respBody: `{"choices":[{"message":{"content":"一只斑马在草原上"}}]}`}}

	capText, err := c.Caption(context.Background(), "photo.png", []byte("fake-image-bytes"))
	if err != nil {
		t.Fatalf("Caption error: %v", err)
	}
	if capText != "一只斑马在草原上" {
		t.Errorf("caption = %q", capText)
	}
}

func TestMimeByExt(t *testing.T) {
	if mimeByExt("a.png") != "image/png" {
		t.Error("png mime wrong")
	}
	if mimeByExt("a.jpg") != "image/jpeg" {
		t.Error("jpg mime wrong")
	}
}
