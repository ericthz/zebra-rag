package service

import (
	"testing"

	"github.com/ericthz/zebra-rag/internal/model"
)

func TestMergeSegments(t *testing.T) {
	vectors := []*model.DocumentVector{
		{ChunkID: 0, TextContent: "一二三四五"},
		{ChunkID: 1, TextContent: "六七八九十"},
		{ChunkID: 2, TextContent: "甲乙丙丁戊"},
	}
	// chunkSize 较小，确保能聚合出多段
	segs := mergeSegments(vectors, 6)
	if len(segs) < 2 {
		t.Fatalf("expected >=2 segments, got %d", len(segs))
	}
	// 内容覆盖全部文本
	joined := ""
	for _, s := range segs {
		joined += s
	}
	for _, v := range vectors {
		if !contains(joined, v.TextContent) {
			t.Errorf("segment content missing %q", v.TextContent)
		}
	}
	// 空输入
	if segs := mergeSegments(nil, 100); len(segs) != 0 {
		t.Errorf("expected 0 segments for empty input, got %d", len(segs))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
