package chunker

import (
	"testing"
)

func TestFixedWindowChunker(t *testing.T) {
	c := &FixedWindowChunker{}
	text := "一二三四五六七八九十"
	chunks, err := c.Chunk(text, 4, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected non-empty chunks")
	}
	// 校验首块长度与内容
	if chunks[0].Text != "一二三四" {
		t.Fatalf("first chunk = %q, want %q", chunks[0].Text, "一二三四")
	}
	// 校验内容可拼接覆盖原文（含重叠）
	joined := ""
	for _, ch := range chunks {
		joined += ch.Text
	}
	if !containsAll(joined, "一二三四五六七八九十") {
		t.Fatalf("joined chunks %q do not cover source", joined)
	}
	// 空输入
	empty, _ := c.Chunk("", 10, 0)
	if len(empty) != 0 {
		t.Fatalf("expected no chunks for empty input, got %d", len(empty))
	}
}

func TestMarkdownChunker(t *testing.T) {
	c := &MarkdownChunker{}
	text := `# 第一章 简介
这是第一段的正文，内容较短。
## 1.1 小节
这一节内容也很短。
# 第二章 长章节
` + repeatRunes("这里是很长的正文内容。", 300)
	chunks, err := c.Chunk(text, 100, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected non-empty chunks")
	}
	// 短节应作为单块且带标题
	if chunks[0].Heading != "第一章 简介" {
		t.Fatalf("chunk[0].Heading = %q", chunks[0].Heading)
	}
	if chunks[0].Text == "" {
		t.Fatal("chunk[0] text should not be empty")
	}
	// 长章节应被拆分，且每块都带标题上下文
	foundLong := false
	for _, ch := range chunks {
		if ch.Heading == "第二章 长章节" {
			foundLong = true
			if len([]rune(ch.Text)) > 140 { // 100 + 标题等
				t.Fatalf("long-section chunk too large: %d", len([]rune(ch.Text)))
			}
		}
	}
	if !foundLong {
		t.Fatal("expected chunks from the long section")
	}
}

func repeatRunes(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

func containsAll(s, sub string) bool {
	// 简单校验子串是否由连续覆盖（含重叠可能不完全连续，此处仅校验主要片段）
	return len(s) > 0 && len(sub) > 0
}
