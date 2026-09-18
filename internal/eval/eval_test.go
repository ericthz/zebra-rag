package eval

import (
	"testing"
)

func TestComputeRetrieval(t *testing.T) {
	retrieved := [][]string{
		{"a", "b", "c", "d"}, // 命中 b（第2位）、c（第3位）
		{"x", "y"},           // 完全未命中
		{"e", "f", "g"},      // 命中 e（第1位）
	}
	expected := [][]string{
		{"b", "c", "z"},
		{"m"},
		{"e"},
	}
	m := ComputeRetrieval(retrieved, expected, 4)
	if m.Queries != 3 {
		t.Fatalf("Queries = %d", m.Queries)
	}
	// query0: recall = 2/3, MRR = 1/2
	// query1: recall = 0, MRR = 0
	// query2: recall = 1, MRR = 1
	expRecall := (2.0/3 + 0 + 1) / 3
	if approx(m.RecallAtK, expRecall) {
		t.Errorf("RecallAtK = %v, want %v", m.RecallAtK, expRecall)
	}
	expMRR := (0.5 + 0 + 1) / 3
	if approx(m.MRR, expMRR) {
		t.Errorf("MRR = %v, want %v", m.MRR, expMRR)
	}
	expHit := 2.0 / 3
	if approx(m.HitRate, expHit) {
		t.Errorf("HitRate = %v, want %v", m.HitRate, expHit)
	}
}

func TestComputeGeneration(t *testing.T) {
	answers := []string{"Zebra RAG采用RAG技术构建企业级知识库"}
	expected := []string{"Zebra RAG采用RAG技术"}
	m := ComputeGeneration(answers, expected)
	if m.Queries != 1 {
		t.Fatalf("Queries = %d", m.Queries)
	}
	if m.AnswerRelevancy <= 0 {
		t.Errorf("AnswerRelevancy = %v, want > 0", m.AnswerRelevancy)
	}
	if m.Faithfulness <= 0 {
		t.Errorf("Faithfulness = %v, want > 0", m.Faithfulness)
	}
}

func TestComputeContext(t *testing.T) {
	retrieved := [][]string{{"a", "b", "c"}}
	expected := [][]string{{"b"}}
	cm := ComputeContext(retrieved, expected, 3)
	if cm.ContextRecall != 1.0 {
		t.Errorf("ContextRecall = %v, want 1", cm.ContextRecall)
	}
	if cm.ContextPrecision != 1.0/3 {
		t.Errorf("ContextPrecision = %v, want %v", cm.ContextPrecision, 1.0/3)
	}
}

func approx(got, want float64) bool {
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	return diff > 1e-9
}
