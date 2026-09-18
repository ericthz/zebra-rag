package service

import (
	"context"
	"testing"
)

func TestRuleIntentClassifier(t *testing.T) {
	c := &ruleIntentClassifier{}
	ctx := context.Background()
	cases := []struct {
		query string
		want  Intent
	}{
		{"你好", IntentChitchat},
		{"谢谢你的回答", IntentChitchat},
		{"再见", IntentChitchat},
		{"什么是RAG", IntentRetrieval},
		{"Zebra RAG如何做混合检索", IntentRetrieval},
		{"MySQL 索引优化", IntentRetrieval},
		{"", IntentRetrieval},
	}
	for _, cse := range cases {
		if got := c.Classify(ctx, cse.query); got != cse.want {
			t.Errorf("Classify(%q) = %v, want %v", cse.query, got, cse.want)
		}
	}
}
