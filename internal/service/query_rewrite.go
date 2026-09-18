// Package service 的查询改写组件：将口语化问题改写为适合检索的查询词（B5.1）。
package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/ericthz/zebra-rag/internal/config"
	"github.com/ericthz/zebra-rag/internal/infra/llm"
	"github.com/ericthz/zebra-rag/pkg/log"
)

// QueryRewriter 查询改写接口。
type QueryRewriter interface {
	Rewrite(ctx context.Context, query string) (string, error)
	// RewriteMany 生成多个查询变体（含原查询），用于多查询扩展。
	RewriteMany(ctx context.Context, query string, n int) ([]string, error)
}

type llmQueryRewriter struct {
	llmClient llm.Client
	prompt    string
	maxTokens int
}

// NewQueryRewriter 创建基于 LLM 的查询改写器。
func NewQueryRewriter(llmClient llm.Client, prompt string, maxTokens int) QueryRewriter {
	return &llmQueryRewriter{llmClient: llmClient, prompt: prompt, maxTokens: maxTokens}
}

// Rewrite 将口语化问题改写为适合检索的查询词；失败或为空时回退原查询。
func (r *llmQueryRewriter) Rewrite(ctx context.Context, query string) (string, error) {
	rewritten, err := r.llmClient.Complete(ctx, r.prompt, query, r.maxTokens)
	if err != nil {
		log.Errorf("[QueryRewrite] 查询改写失败，回退原查询: %v", err)
		return query, nil
	}
	rewritten = strings.TrimSpace(rewritten)
	if rewritten == "" || rewritten == query {
		return query, nil
	}
	log.Infof("[QueryRewrite] '%s' -> '%s'", query, rewritten)
	return rewritten, nil
}

// RewriteMany 生成多个查询变体：首项为原查询，其余为 LLM 生成的扩展查询。
func (r *llmQueryRewriter) RewriteMany(ctx context.Context, query string, n int) ([]string, error) {
	if n <= 0 {
		n = 2
	}
	variants := []string{query}
	prompt := r.prompt
	// 优先使用多查询专用提示词
	if mp := config.Conf.Search.MultiQuery.Prompt; mp != "" {
		prompt = mp
	}
	user := fmt.Sprintf("问题：%s\n请生成 %d 个不同的检索查询变体：", query, n)
	raw, err := r.llmClient.Complete(ctx, prompt, user, 256)
	if err != nil {
		log.Errorf("[MultiQuery] 生成查询变体失败: %v", err)
		return variants, nil
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "-0123456789.。 "))
		if line != "" && line != query {
			variants = append(variants, line)
		}
		if len(variants) > n+1 {
			break
		}
	}
	log.Infof("[MultiQuery] '%s' 扩展为 %d 个查询", query, len(variants))
	return variants, nil
}
