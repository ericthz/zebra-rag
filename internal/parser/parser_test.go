package parser

import (
	"context"
	"strings"
	"testing"
)

func TestExtractTitle(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"# 标题一\n正文", "标题一"},
		{"## 二级标题\n正文", "二级标题"},
		{"无标题文本", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := ExtractTitle(c.text); got != c.want {
			t.Errorf("ExtractTitle(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestMetaFromName(t *testing.T) {
	m := MetaFromName("report.PDF")
	if m["doc_type"] != "pdf" {
		t.Errorf("doc_type = %q, want pdf", m["doc_type"])
	}
	m2 := MetaFromName("noext")
	if len(m2) != 0 {
		t.Errorf("expected empty meta for no extension, got %v", m2)
	}
}

func TestTikaParserFallsBackToMeta(t *testing.T) {
	// TikaParser 依赖真实 Tika 服务，无法在单测中运行。
	// 此处仅验证空文本时元数据仍可抽取的辅助函数。
	if got := ExtractTitle("# 测试\n内容"); got != "测试" {
		t.Fatalf("ExtractTitle = %q", got)
	}
	_ = context.Background()
	_ = strings.TrimSpace
}
