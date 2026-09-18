package service

import (
	"strings"
	"testing"

	"github.com/ericthz/zebra-rag/internal/model"
)

func mkHistory(n, size int) []model.ChatMessage {
	out := make([]model.ChatMessage, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, model.ChatMessage{Role: "user", Content: strings.Repeat("字", size)})
	}
	return out
}

func TestBoundHistoryCountCap(t *testing.T) {
	h := mkHistory(50, 10)
	got := boundHistory(h)
	if len(got) > 20 {
		t.Fatalf("boundHistory should cap at 20, got %d", len(got))
	}
	if len(got) < 2 {
		t.Fatalf("boundHistory should keep at least last 2 messages, got %d", len(got))
	}
	if got[len(got)-1] != h[len(h)-1] {
		t.Error("boundHistory should preserve the latest message")
	}
}

func TestBoundHistoryRuneCap(t *testing.T) {
	// 每条约 2000 字，12 条共 24000 字，超过 8000 上限；应从头部裁减到 <= 8000 且 >= 2 条
	h := mkHistory(12, 2000)
	got := boundHistory(h)
	if len(got) < 2 {
		t.Fatalf("boundHistory should keep at least last 2, got %d", len(got))
	}
	if historyRuneLen(got) > 8000 {
		t.Fatalf("boundHistory should cap total runes to 8000, got %d", historyRuneLen(got))
	}
	if got[len(got)-1] != h[len(h)-1] {
		t.Error("boundHistory should preserve the latest message")
	}
}
