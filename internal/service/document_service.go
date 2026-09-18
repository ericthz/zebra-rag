// Package service 包含了应用的业务逻辑层。
package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/ericthz/zebra-rag/internal/config"
	"github.com/ericthz/zebra-rag/internal/infra/storage"
	"github.com/ericthz/zebra-rag/internal/model"
	"github.com/ericthz/zebra-rag/internal/repository"
	"github.com/ericthz/zebra-rag/pkg/log"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/ericthz/zebra-rag/internal/infra/es"
	"github.com/ericthz/zebra-rag/internal/infra/tika"
	"github.com/minio/minio-go/v7"
)

// FileUploadDTO 是一个数据传输对象，用于在返回给前端时隐藏一些字段并添加额外信息。
type FileUploadDTO struct {
	model.FileUpload
	OrgTagName string `json:"orgTagName"`
}

// DownloadInfoDTO 封装了文件下载链接所需的信息。
type DownloadInfoDTO struct {
	FileName    string `json:"fileName"`
	DownloadURL string `json:"downloadUrl"`
	FileSize    int64  `json:"fileSize"`
}

// PreviewInfoDTO 封装了文件预览所需的信息。
type PreviewInfoDTO struct {
	FileName string `json:"fileName"`
	Content  string `json:"content"`
	FileSize int64  `json:"fileSize"`
}

// DocumentService 接口定义了文档管理相关的业务操作。
type DocumentService interface {
	ListAccessibleFiles(user *model.User) ([]FileUploadDTO, error)
	ListUploadedFiles(userID uint) ([]FileUploadDTO, error)
	DeleteDocument(fileMD5 string, user *model.User) error
	GenerateDownloadURL(fileName, fileMD5 string, user *model.User) (*DownloadInfoDTO, error)
	GetFilePreviewContent(fileName, fileMD5 string, user *model.User) (*PreviewInfoDTO, error)
	IsFileAccessible(fileMD5 string, user *model.User) (bool, error)
}

type documentService struct {
	uploadRepo    repository.UploadRepository
	userRepo      repository.UserRepository
	orgTagRepo    repository.OrgTagRepository // 新增依赖
	minioCfg      config.MinIOConfig
	tikaClient    *tika.Client // 新增依赖
	docVectorRepo repository.DocumentVectorRepository
	esClient      *elasticsearch.Client
}

// NewDocumentService 创建一个新的 DocumentService 实例。
func NewDocumentService(uploadRepo repository.UploadRepository, userRepo repository.UserRepository, orgTagRepo repository.OrgTagRepository, minioCfg config.MinIOConfig, tikaClient *tika.Client, docVectorRepo repository.DocumentVectorRepository, esClient *elasticsearch.Client) DocumentService {
	return &documentService{
		uploadRepo:    uploadRepo,
		userRepo:      userRepo,
		orgTagRepo:    orgTagRepo,
		minioCfg:      minioCfg,
		tikaClient:    tikaClient,
		docVectorRepo: docVectorRepo,
		esClient:      esClient,
	}
}

// ListAccessibleFiles 获取用户可访问的文件列表（附带组织标签名称，供前端展示）。
func (s *documentService) ListAccessibleFiles(user *model.User) ([]FileUploadDTO, error) {
	orgTags := strings.Split(user.OrgTags, ",")
	files, err := s.uploadRepo.FindAccessibleFiles(user.ID, orgTags)
	if err != nil {
		return nil, err
	}
	return s.mapFileUploadsToDTOs(files)
}

// ListUploadedFiles 获取用户自己上传的文件列表，并附加组织标签名称。
func (s *documentService) ListUploadedFiles(userID uint) ([]FileUploadDTO, error) {
	files, err := s.uploadRepo.FindFilesByUserID(userID)
	if err != nil {
		return nil, err
	}

	dtos, err := s.mapFileUploadsToDTOs(files)
	if err != nil {
		return nil, err
	}

	return dtos, nil
}

// DeleteDocument 删除一个文档，并级联清理索引与分块。
// 说明：同 MD5 文件可能被多个用户上传（秒传去重），MinIO 合并对象 / document_vectors / ES 索引均为
// 同 MD5 共享；因此仅当该 MD5 不再被任何用户引用时，才删除底层对象与全部派生数据。
func (s *documentService) DeleteDocument(fileMD5 string, user *model.User) error {
	record, err := s.uploadRepo.GetFileUploadRecord(fileMD5, user.ID)
	if err != nil {
		return errors.New("文件不存在或不属于该用户")
	}

	if record.UserID != user.ID && user.Role != "ADMIN" {
		return errors.New("没有权限删除此文件")
	}

	// 1. 删除上传记录
	if err := s.uploadRepo.DeleteFileUploadRecord(fileMD5, record.UserID); err != nil {
		log.Errorf("[DeleteDocument] 删除上传记录失败: fileMd5=%s user=%d err=%v", fileMD5, record.UserID, err)
		return err
	}

	// 2. 级联清理前检查该 MD5 是否还有其他用户引用（含未完成的记录）
	remaining, err := s.uploadRepo.FindBatchByMD5s([]string{fileMD5})
	if err != nil {
		log.Warnf("[DeleteDocument] 检查 fileMd5=%s 剩余引用失败: %v", fileMD5, err)
	}
	if len(remaining) > 0 {
		log.Infof("[DeleteDocument] fileMd5=%s 仍有 %d 个引用，跳过底层对象删除", fileMD5, len(remaining))
		return nil
	}

	// 3. 无其他引用：删除 MinIO 对象、document_vectors、ES 索引
	objectName := s.resolveMergedKey(context.Background(), record.FileMD5, record.FileName)
	if err := storage.MinioClient.RemoveObject(context.Background(), s.minioCfg.BucketName, objectName, minio.RemoveObjectOptions{}); err != nil {
		log.Warnf("[DeleteDocument] 删除 MinIO 对象失败(忽略继续): %s err=%v", objectName, err)
	}

	if err := s.docVectorRepo.DeleteByFileMD5(fileMD5); err != nil {
		log.Warnf("[DeleteDocument] 清理 document_vectors 失败: fileMd5=%s err=%v", fileMD5, err)
	}
	if s.esClient != nil {
		if _, err := es.DeleteByFileMD5(context.Background(), config.Conf.Elasticsearch.IndexName, fileMD5); err != nil {
			log.Warnf("[DeleteDocument] 清理 Elasticsearch 索引失败: fileMd5=%s err=%v", fileMD5, err)
		}
	}

	return nil
}

// IsFileAccessible 判断文件对当前用户是否可访问（本人 / 全局公开 / 组织标签命中）。
func (s *documentService) IsFileAccessible(fileMD5 string, user *model.User) (bool, error) {
	files, err := s.ListAccessibleFiles(user)
	if err != nil {
		return false, err
	}
	for i := range files {
		if files[i].FileMD5 == fileMD5 {
			return true, nil
		}
	}
	return false, nil
}

// GenerateDownloadURL 生成文件的临时下载链接。
// 优先使用 fileMD5 精确定位；否则按文件名匹配，重名时优先本人上传 + 最近合并的文件。
func (s *documentService) GenerateDownloadURL(fileName, fileMD5 string, user *model.User) (*DownloadInfoDTO, error) {
	files, err := s.ListAccessibleFiles(user)
	if err != nil {
		return nil, err
	}

	var targetFile *FileUploadDTO
	if fileMD5 != "" {
		for i := range files {
			if files[i].FileMD5 == fileMD5 {
				targetFile = &files[i]
				break
			}
		}
		if targetFile == nil {
			return nil, errors.New("文件不存在或无权访问")
		}
	} else {
		targetFile = s.pickFileByName(files, fileName, user)
	}

	if targetFile == nil {
		return nil, errors.New("文件不存在或无权访问")
	}

	// 生成预签名的 URL，有效期为1小时
	expiry := time.Hour
	objectName := s.resolveMergedKey(context.Background(), targetFile.FileMD5, targetFile.FileName)
	presignedURL, err := storage.MinioClient.PresignedGetObject(context.Background(), s.minioCfg.BucketName, objectName, expiry, url.Values{})
	if err != nil {
		return nil, err
	}

	return &DownloadInfoDTO{
		FileName:    targetFile.FileName,
		DownloadURL: presignedURL.String(),
		FileSize:    targetFile.TotalSize,
	}, nil
}

// GetFilePreviewContent 获取文件的纯文本预览内容。
func (s *documentService) GetFilePreviewContent(fileName, fileMD5 string, user *model.User) (*PreviewInfoDTO, error) {
	files, err := s.ListAccessibleFiles(user)
	if err != nil {
		return nil, err
	}

	var targetFile *FileUploadDTO
	if fileMD5 != "" {
		for i := range files {
			if files[i].FileMD5 == fileMD5 {
				targetFile = &files[i]
				break
			}
		}
		if targetFile == nil {
			return nil, errors.New("文件不存在或无权访问")
		}
	} else {
		targetFile = s.pickFileByName(files, fileName, user)
	}

	if targetFile == nil {
		return nil, errors.New("文件不存在或无权访问")
	}

	// 从 MinIO 获取文件对象（优先 fileMD5，回退旧 fileName，兼容历史对象）
	objectName := s.resolveMergedKey(context.Background(), targetFile.FileMD5, targetFile.FileName)
	object, err := storage.MinioClient.GetObject(context.Background(), s.minioCfg.BucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer object.Close()

	// 将文件流发送给 Tika 进行文本提取
	content, err := s.tikaClient.ExtractText(object, targetFile.FileName)
	if err != nil {
		return nil, err
	}

	return &PreviewInfoDTO{
		FileName: targetFile.FileName,
		Content:  content,
		FileSize: targetFile.TotalSize,
	}, nil
}

// pickFileByName 在可访问文件列表中按文件名匹配；存在重名时优先本人上传、其次最近合并的记录。
func (s *documentService) pickFileByName(files []FileUploadDTO, fileName string, user *model.User) *FileUploadDTO {
	if fileName == "" {
		return nil
	}
	var matches []*FileUploadDTO
	for i := range files {
		if files[i].FileName == fileName {
			matches = append(matches, &files[i])
		}
	}
	if len(matches) == 0 {
		return nil
	}
	if len(matches) > 1 {
		log.Warnf("[Document] 文件名 '%s' 存在 %d 个匹配，选择本人上传/最近合并的", fileName, len(matches))
		sort.SliceStable(matches, func(a, b int) bool {
			if (matches[a].UserID == user.ID) != (matches[b].UserID == user.ID) {
				return matches[a].UserID == user.ID
			}
			return mergedAtNewer(matches[a].MergedAt, matches[b].MergedAt)
		})
	}
	return matches[0]
}

// mergedAtNewer 比较合并时间，nil 视为最旧。
func mergedAtNewer(a, b *time.Time) bool {
	if a == nil {
		return false
	}
	if b == nil {
		return true
	}
	return a.After(*b)
}

func (s *documentService) mapFileUploadsToDTOs(files []model.FileUpload) ([]FileUploadDTO, error) {
	if len(files) == 0 {
		return []FileUploadDTO{}, nil
	}

	// To avoid N+1 queries, get all unique org tag IDs first
	tagIDs := make(map[string]struct{})
	for _, file := range files {
		if file.OrgTag != "" {
			tagIDs[file.OrgTag] = struct{}{}
		}
	}

	tagIDList := make([]string, 0, len(tagIDs))
	for id := range tagIDs {
		tagIDList = append(tagIDList, id)
	}

	tags, err := s.orgTagRepo.FindBatchByIDs(tagIDList)
	if err != nil {
		return nil, err
	}

	tagMap := make(map[string]string)
	for _, tag := range tags {
		tagMap[tag.TagID] = tag.Name
	}

	dtos := make([]FileUploadDTO, len(files))
	for i, file := range files {
		dtos[i] = FileUploadDTO{
			FileUpload: file,
			OrgTagName: tagMap[file.OrgTag], // Will be empty string if not found
		}
	}

	return dtos, nil
}

// resolveMergedKey 返回实际存在的 merged 对象 key：
// 优先新方案 merged/{fileMD5}（避免跨用户同名覆盖），回退旧方案 merged/{fileName}（兼容历史上传）。
func (s *documentService) resolveMergedKey(ctx context.Context, fileMD5, fileName string) string {
	candidates := []string{
		fmt.Sprintf("merged/%s", fileMD5),
		fmt.Sprintf("merged/%s", fileName),
	}
	for _, key := range candidates {
		if _, err := storage.MinioClient.StatObject(ctx, s.minioCfg.BucketName, key, minio.StatObjectOptions{}); err == nil {
			return key
		}
	}
	return candidates[0]
}
