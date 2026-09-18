// Package service 的长文档 Map-Reduce 总结组件（B8.3）：
// 将长文档按分块聚合为若干「段」，并行逐段总结（Map），再将各段摘要合成全文总结（Reduce）。
package service

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/ericthz/zebra-rag/internal/config"
	"github.com/ericthz/zebra-rag/internal/infra/llm"
	"github.com/ericthz/zebra-rag/internal/model"
	"github.com/ericthz/zebra-rag/internal/repository"
	"github.com/ericthz/zebra-rag/pkg/log"
)

// SummarizeService 文档摘要接口。
type SummarizeService interface {
	SummarizeDocument(ctx context.Context, fileMD5 string, user *model.User) (string, error)
}

type summarizeService struct {
	docVectorRepo repository.DocumentVectorRepository
	uploadRepo    repository.UploadRepository
	userService   UserService
	llmClient     llm.Client
}

// NewSummarizeService 创建一个 Map-Reduce 文档摘要服务。
func NewSummarizeService(docVectorRepo repository.DocumentVectorRepository, uploadRepo repository.UploadRepository, userService UserService, llmClient llm.Client) SummarizeService {
	return &summarizeService{docVectorRepo: docVectorRepo, uploadRepo: uploadRepo, userService: userService, llmClient: llmClient}
}

// SummarizeDocument 对文档分块执行 Map-Reduce 总结。
// 入口先做文件归属/可访问性校验，防止对任意 fileMD5 触发总结造成越权读取。
func (s *summarizeService) SummarizeDocument(ctx context.Context, fileMD5 string, user *model.User) (string, error) {
	if !s.isAccessible(ctx, fileMD5, user) {
		return "", errors.New("文件不存在或无权访问")
	}
	vectors, err := s.docVectorRepo.FindByFileMD5(fileMD5)
	if err != nil {
		return "", err
	}
	if len(vectors) == 0 {
		return "", errors.New("文档未处理或无分块")
	}

	// Map 阶段：将分块聚合成若干段（每段约 chunkSize 字符），并行总结
	chunkSize := config.Conf.Summarize.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 1500
	}
	segments := mergeSegments(vectors, chunkSize)
	segSummaries := make([]string, len(segments))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	errCh := make(chan error, len(segments))
	for i, seg := range segments {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, seg string) {
			defer wg.Done()
			defer func() { <-sem }()
			sum, err := s.llmClient.Complete(ctx, config.Conf.Summarize.MapPrompt, seg, 512)
			if err != nil {
				log.Errorf("[Summarize] 段 %d 总结失败: %v", i, err)
				errCh <- err
				return
			}
			segSummaries[i] = sum
		}(i, seg)
	}
	wg.Wait()
	close(errCh)
	if err := <-errCh; err != nil {
		return "", err
	}

	// Reduce 阶段：合成最终总结
	joined := strings.Join(segSummaries, "\n---\n")
	final, err := s.llmClient.Complete(ctx, config.Conf.Summarize.ReducePrompt, joined, 1024)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(final), nil
}

// isAccessible 校验 fileMD5 是否对用户可访问（本人 / 全局公开 / 组织标签命中）。
func (s *summarizeService) isAccessible(_ context.Context, fileMD5 string, user *model.User) bool {
	if user == nil || fileMD5 == "" {
		return false
	}
	if rec, err := s.uploadRepo.GetFileUploadRecord(fileMD5, user.ID); err == nil && rec != nil {
		return true
	}
	tags, err := s.userService.GetUserEffectiveOrgTags(user)
	if err != nil {
		log.Warnf("[Summarize] 获取有效组织标签失败: %v", err)
	}
	files, err := s.uploadRepo.FindAccessibleFiles(user.ID, tags)
	if err != nil {
		log.Warnf("[Summarize] 查询可访问文件失败: %v", err)
		return false
	}
	for i := range files {
		if files[i].FileMD5 == fileMD5 {
			return true
		}
	}
	return false
}

// mergeSegments 将分块按字符数聚合成段（用于 Map 阶段）。
func mergeSegments(vectors []*model.DocumentVector, chunkSize int) []string {
	var segments []string
	var buf strings.Builder
	for _, v := range vectors {
		if buf.Len()+len(v.TextContent) > chunkSize && buf.Len() > 0 {
			segments = append(segments, buf.String())
			buf.Reset()
		}
		buf.WriteString(v.TextContent)
		buf.WriteString("\n")
	}
	if buf.Len() > 0 {
		segments = append(segments, buf.String())
	}
	return segments
}
