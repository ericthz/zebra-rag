package service

import (
	"testing"

	"github.com/ericthz/zebra-rag/internal/model"
)

func TestNormalizeQuery(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"请问Zebra RAG是什么", "zebra rag"}, // 口语词 请问/是什么 被去除
		{"MySQL 索引优化", "mysql 索引优化"},
		{"  多余   空格  ", "多余 空格"},
		{"", ""},
	}
	for _, c := range cases {
		got, _ := normalizeQuery(c.in)
		if got != c.want {
			t.Errorf("normalizeQuery(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildPhraseShould(t *testing.T) {
	if buildPhraseShould("") != nil {
		t.Error("empty phrase should return nil")
	}
	s := buildPhraseShould("Zebra RAG")
	m, ok := s.([]map[string]interface{})
	if !ok || len(m) != 1 {
		t.Fatalf("unexpected phrase should: %#v", s)
	}
}

func TestRRFFuse(t *testing.T) {
	mk := func(id string) esHit {
		return esHit{Source: model.EsDocument{VectorID: id}}
	}
	lists := [][]esHit{
		{mk("a"), mk("b"), mk("c")},
		{mk("b"), mk("d"), mk("a")},
	}
	out := rrfFuse(lists, 60)
	// b 在两个列表均排第 2，RRF 分最高；a 次之
	if out[0].Source.VectorID != "b" {
		t.Errorf("top fused = %q, want b", out[0].Source.VectorID)
	}
	got := map[string]float64{}
	for _, h := range out {
		got[h.Source.VectorID] = h.Score
	}
	if len(got) != 4 { // a,b,c,d 均被召回
		t.Errorf("fused set size = %d, want 4", len(got))
	}
	if got["a"] <= got["c"] || got["a"] <= got["d"] {
		t.Errorf("a should rank above c/d, got %v", got)
	}
}
