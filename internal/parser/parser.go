// Package parser 提供文档解析抽象：纯文本抽取（Tika）、布局感知解析（MinerU/Unstructured 等服务）与元数据抽取。
// 解析策略通过配置切换，布局服务不可用时自动回退到 Tika。
package parser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/ericthz/zebra-rag/internal/infra/tika"
	"github.com/ericthz/zebra-rag/pkg/log"
)

// ParsedImage 表示解析出的图片（多模态入库用，B1.2）。
type ParsedImage struct {
	Name string // 图片文件名
	Data []byte // 图片字节
}

// ParsedDoc 表示一次文档解析的结果。
type ParsedDoc struct {
	Text   string            // 解析出的文本（布局解析时为结构化 Markdown）
	Title  string            // 抽取的标题
	Meta   map[string]string // 元数据（如文档类型）
	Images []ParsedImage     // 解析出的图片（布局解析器可返回，用于多模态入库）
}

// Parser 文档解析器接口。
type Parser interface {
	Parse(ctx context.Context, reader io.Reader, fileName string) (*ParsedDoc, error)
}

// New 根据配置创建解析器：mode=tika（默认）或 layout（布局感知，回退 Tika）。
func New(mode, layoutURL string, tikaClient *tika.Client) Parser {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "layout":
		if tikaClient == nil {
			log.Warnf("[Parser] layout 模式缺少 tika 回退客户端，退回 tika 模式")
			return NewTikaParser(tikaClient)
		}
		return NewLayoutParser(layoutURL, NewTikaParser(tikaClient))
	default:
		return NewTikaParser(tikaClient)
	}
}

// TikaParser 基于 Apache Tika 的纯文本抽取。
type TikaParser struct {
	client *tika.Client
}

// NewTikaParser 创建 Tika 解析器。
func NewTikaParser(client *tika.Client) *TikaParser {
	return &TikaParser{client: client}
}

// Parse 实现 Parser 接口。
func (p *TikaParser) Parse(ctx context.Context, reader io.Reader, fileName string) (*ParsedDoc, error) {
	text, err := p.client.ExtractText(reader, fileName)
	if err != nil {
		return nil, err
	}
	return &ParsedDoc{
		Text:  text,
		Title: ExtractTitle(text),
		Meta:  MetaFromName(fileName),
	}, nil
}

// LayoutParser 布局感知解析：对接 MinerU/Unstructured 等 HTTP 服务（返回结构化 Markdown），
// 服务不可用时回退到 Tika 纯文本抽取。
type LayoutParser struct {
	layoutURL string
	fallback  Parser
	http      *http.Client
}

// NewLayoutParser 创建布局解析器。
func NewLayoutParser(layoutURL string, fallback Parser) *LayoutParser {
	return &LayoutParser{
		layoutURL: strings.TrimRight(layoutURL, "/"),
		fallback:  fallback,
		http:      &http.Client{Timeout: 60 * time.Second},
	}
}

// Parse 实现 Parser 接口（multipart 上传文件到布局服务）。
func (p *LayoutParser) Parse(ctx context.Context, reader io.Reader, fileName string) (*ParsedDoc, error) {
	doc, err := p.parseRemote(ctx, reader, fileName)
	if err != nil {
		log.Warnf("[Parser] 布局解析失败，回退到 Tika: %v", err)
		if p.fallback != nil {
			return p.fallback.Parse(ctx, reader, fileName)
		}
		return nil, err
	}
	return doc, nil
}

type layoutResponse struct {
	Text  string            `json:"text"`
	Title string            `json:"title"`
	Meta  map[string]string `json:"meta"`
}

func (p *LayoutParser) parseRemote(ctx context.Context, reader io.Reader, fileName string) (*ParsedDoc, error) {
	if p.layoutURL == "" {
		return nil, fmt.Errorf("layout_url 未配置")
	}
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	fw, err := w.CreateFormFile("file", fileName)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(fw, reader); err != nil {
		return nil, err
	}
	_ = w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.layoutURL+"/parse", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("layout service returned %d", resp.StatusCode)
	}
	var lr layoutResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return nil, err
	}
	if lr.Meta == nil {
		lr.Meta = map[string]string{}
	}
	for k, v := range MetaFromName(fileName) {
		lr.Meta[k] = v
	}
	if lr.Title == "" {
		lr.Title = ExtractTitle(lr.Text)
	}
	return &ParsedDoc{Text: lr.Text, Title: lr.Title, Meta: lr.Meta}, nil
}

// ExtractTitle 从文本首个 Markdown 标题抽取标题（B1.3 元数据抽取）。
func ExtractTitle(text string) string {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			return strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		}
	}
	return ""
}

// MetaFromName 从文件名抽取元数据（如文档类型扩展名）。
func MetaFromName(fileName string) map[string]string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(fileName), "."))
	if ext == "" {
		return map[string]string{}
	}
	return map[string]string{"doc_type": ext}
}
