// Package service 的图片理解服务（B1.2 多模态入库）。
package service

import (
	"context"

	"github.com/ericthz/zebra-rag/internal/infra/vision"
)

// VisionService 图片理解（描述）接口。
type VisionService interface {
	CaptionImage(ctx context.Context, name string, data []byte) (string, error)
}

type visionService struct {
	client *vision.Client
}

// NewVisionService 创建一个图片理解服务。
func NewVisionService(client *vision.Client) VisionService {
	return &visionService{client: client}
}

// CaptionImage 生成图片描述。
func (s *visionService) CaptionImage(ctx context.Context, name string, data []byte) (string, error) {
	return s.client.Caption(ctx, name, data)
}
