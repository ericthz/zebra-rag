// Package service 的意图路由组件（B5.3）：区分「知识库检索问答」与「闲聊」，
// 闲聊可直接由 LLM 作答，跳过检索以省成本、提升响应。
package service

import (
	"context"
	"strings"

	"github.com/ericthz/zebra-rag/internal/infra/llm"
	"github.com/ericthz/zebra-rag/pkg/log"
)

// Intent 查询意图。
type Intent string

const (
	// IntentRetrieval 需要知识库检索的问答。
	IntentRetrieval Intent = "retrieval"
	// IntentChitchat 闲聊，无需检索。
	IntentChitchat Intent = "chitchat"
)

// IntentClassifier 查询意图分类器。
type IntentClassifier interface {
	Classify(ctx context.Context, query string) Intent
}

// NewIntentClassifier 根据配置创建分类器：rule（规则，默认）或 llm（LLM 判定，失败回退规则）。
func NewIntentClassifier(mode string, llmClient llm.Client) IntentClassifier {
	rule := &ruleIntentClassifier{}
	if strings.ToLower(strings.TrimSpace(mode)) == "llm" && llmClient != nil {
		return &llmIntentClassifier{llm: llmClient, fallback: rule}
	}
	return rule
}

// ruleIntentClassifier 基于规则的轻量分类。
type ruleIntentClassifier struct{}

func (c *ruleIntentClassifier) Classify(_ context.Context, query string) Intent {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return IntentRetrieval
	}
	// 闲聊关键词：问候/感谢/告别/身份/闲聊类
	chitchat := []string{"你好", "您好", "嗨", "hello", "hi ", "谢谢", "感谢", "再见", "拜拜", "你是谁", "介绍下你", "你会什么", "讲个笑话", "今天天气", "在吗"}
	for _, p := range chitchat {
		if strings.Contains(q, p) {
			return IntentChitchat
		}
	}
	return IntentRetrieval
}

// llmIntentClassifier 基于 LLM 的意图分类（失败回退规则）。
type llmIntentClassifier struct {
	llm      llm.Client
	fallback IntentClassifier
}

func (c *llmIntentClassifier) Classify(ctx context.Context, query string) Intent {
	prompt := "你是意图分类器。判断用户问题是需要知识库检索的问答(retrieval)，还是与知识库无关的日常闲聊(chitchat)。只输出 retrieval 或 chitchat。"
	out, err := c.llm.Complete(ctx, prompt, query, 16)
	if err != nil {
		log.Warnf("[Intent] LLM 分类失败，回退规则: %v", err)
		return c.fallback.Classify(ctx, query)
	}
	out = strings.ToLower(strings.TrimSpace(out))
	if strings.Contains(out, "chitchat") {
		return IntentChitchat
	}
	return IntentRetrieval
}
