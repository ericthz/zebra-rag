// Package pipeline 定义了文件处理的核心流程。
package pipeline

import (
	"bytes" // 引入 bytes 包
	"context"
	"errors"
	"fmt"
	"github.com/ericthz/zebra-rag/internal/chunker"
	"github.com/ericthz/zebra-rag/internal/config"
	"github.com/ericthz/zebra-rag/internal/infra/embedding"
	"github.com/ericthz/zebra-rag/internal/infra/es"
	"github.com/ericthz/zebra-rag/internal/infra/sparse"
	"github.com/ericthz/zebra-rag/internal/infra/storage"
	"github.com/ericthz/zebra-rag/internal/infra/vision"
	"github.com/ericthz/zebra-rag/internal/model"
	"github.com/ericthz/zebra-rag/internal/parser"
	"github.com/ericthz/zebra-rag/internal/repository"
	"github.com/ericthz/zebra-rag/pkg/log"
	"github.com/ericthz/zebra-rag/pkg/tasks"
	"github.com/ericthz/zebra-rag/pkg/tokencount"
	"sync"
	"unicode/utf8"

	"github.com/minio/minio-go/v7"
)

// Processor 封装了文件处理的所有依赖和逻辑。
type Processor struct {
	parser          parser.Parser
	embeddingClient embedding.Client
	esCfg           config.ElasticsearchConfig
	minioCfg        config.MinIOConfig
	embeddingCfg    config.EmbeddingConfig
	uploadRepo      repository.UploadRepository
	docVectorRepo   repository.DocumentVectorRepository
	chunker         chunker.Chunker
	visionClient    *vision.Client // 可为 nil（多模态未启用）
	sparseClient    sparse.Client  // 可为 nil（稀疏向量未启用）
}

// NewProcessor 创建一个新的 Processor 实例。
func NewProcessor(
	parser parser.Parser,
	embeddingClient embedding.Client,
	esCfg config.ElasticsearchConfig,
	minioCfg config.MinIOConfig,
	embeddingCfg config.EmbeddingConfig,
	uploadRepo repository.UploadRepository,
	docVectorRepo repository.DocumentVectorRepository,
	chunker chunker.Chunker,
	visionClient *vision.Client,
	sparseClient sparse.Client,
) *Processor {
	return &Processor{
		parser:          parser,
		embeddingClient: embeddingClient,
		esCfg:           esCfg,
		minioCfg:        minioCfg,
		embeddingCfg:    embeddingCfg,
		uploadRepo:      uploadRepo,
		docVectorRepo:   docVectorRepo,
		chunker:         chunker,
		visionClient:    visionClient,
		sparseClient:    sparseClient,
	}
}

// Process 是文件处理的主函数。
// 处理结果（向量化状态、预估/实际 token 与切片数）会写回 file_upload，供知识库列表展示。
func (p *Processor) Process(ctx context.Context, task tasks.FileProcessingTask) (retErr error) {
	log.Infof("[Processor] 开始处理文件, FileMD5: %s, FileName: %s, UserID: %d", task.FileMD5, task.FileName, task.UserID)

	// 标记为处理中；失败时在 deferred 中记录失败原因
	_ = p.uploadRepo.UpdateVectorizedStatus(task.FileMD5, task.UserID, model.VectorizeProcessing)

	var estimatedTokens, estimatedChunks, actualTokens, actualChunks int

	defer func() {
		if retErr != nil {
			errMsg := retErr.Error()
			if len(errMsg) > 500 {
				errMsg = errMsg[:500]
			}
			_ = p.uploadRepo.UpdateVectorized(task.FileMD5, task.UserID, model.VectorizeFailed, errMsg, estimatedTokens, estimatedChunks, 0, 0)
		}
	}()

	// 1. 从 MinIO 下载文件
	objectName := fmt.Sprintf("merged/%s", task.FileMD5)
	log.Infof("[Processor] 步骤1: 从MinIO下载文件, Bucket: %s, Object: %s", p.minioCfg.BucketName, objectName)
	object, err := storage.MinioClient.GetObject(ctx, p.minioCfg.BucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		log.Errorf("[Processor] 从MinIO下载文件失败, Object: %s, Error: %v", objectName, err)
		return fmt.Errorf("从 MinIO 下载文件失败: %w", err)
	}
	defer object.Close()

	// 增加调试步骤：将文件内容读入内存缓冲区以检查大小
	buf := new(bytes.Buffer)
	size, err := buf.ReadFrom(object)
	if err != nil {
		log.Errorf("[Processor] 从MinIO对象流中读取内容到缓冲区失败, Error: %v", err)
		return fmt.Errorf("读取MinIO对象流失败: %w", err)
	}
	log.Infof("[Processor] 步骤1: 文件下载成功, 从MinIO流中读取到的文件大小为: %d字节", size)
	if size == 0 {
		log.Warnf("[Processor] 文件 '%s' 内容为空, 处理中止", task.FileName)
		return errors.New("文件内容为空")
	}

	// 2. 解析文档（策略：tika 纯文本 / layout 布局感知，失败回退）
	log.Info("[Processor] 步骤2: 解析文档内容, mode=" + config.Conf.Parser.Mode)
	parsed, err := p.parser.Parse(ctx, bytes.NewReader(buf.Bytes()), task.FileName)
	if err != nil {
		log.Errorf("[Processor] 解析文档失败, FileName: %s, Error: %v", task.FileName, err)
		return fmt.Errorf("解析文档失败: %w", err)
	}
	textContent := parsed.Text
	parsedTitle := parsed.Title
	if parsedTitle != "" {
		log.Infof("[Processor] 抽取到文档标题: %s", parsedTitle)
	}
	if textContent == "" {
		log.Warnf("[Processor] Tika提取的文本内容为空, 处理中止, FileName: %s", task.FileName)
		return errors.New("提取的文本内容为空")
	}
	log.Infof("[Processor] 步骤2: 文本提取成功, 内容长度: %d 字符", utf8.RuneCountInString(textContent))

	// 依据提取到的全文估算 token 数（知识库列表“预估向量化”）
	estimatedTokens = tokencount.Estimate(textContent)
	_ = p.uploadRepo.UpdateVectorized(task.FileMD5, task.UserID, model.VectorizeProcessing, "", estimatedTokens, 0, 0, 0)

	// 3. 文本切块（策略与尺寸来自配置：fixed | markdown）
	chunkSize := config.Conf.Chunking.ChunkSize
	chunkOverlap := config.Conf.Chunking.ChunkOverlap
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	if chunkOverlap < 0 {
		chunkOverlap = 100
	}
	log.Infof("[Processor] 步骤3: 进行文本分块, strategy=%s chunkSize=%d chunkOverlap=%d", config.Conf.Chunking.Strategy, chunkSize, chunkOverlap)
	chunks, err := p.chunker.Chunk(textContent, chunkSize, chunkOverlap)
	if err != nil {
		log.Errorf("[Processor] 文本分块失败: %v", err)
		return fmt.Errorf("文本分块失败: %w", err)
	}
	log.Infof("[Processor] 步骤3: 文本分块完成, 共生成 %d 个分块", len(chunks))
	if len(chunks) == 0 {
		log.Warnf("[Processor] 未生成任何文本分块, 处理中止, FileName: %s", task.FileName)
		return errors.New("未生成任何文本分块")
	}

	// 2.6 多模态：布局解析出的图片 -> 视觉描述 -> 追加为可检索分块（B1.2）
	if p.visionClient != nil && len(parsed.Images) > 0 {
		imgChunks := p.captionImages(ctx, parsed, len(chunks))
		if len(imgChunks) > 0 {
			chunks = append(chunks, imgChunks...)
			log.Infof("[Processor] 多模态入库 %d 张图片描述", len(imgChunks))
		}
	}

	// 依据最终切片数记录预估切片数
	estimatedChunks = len(chunks)
	_ = p.uploadRepo.UpdateVectorized(task.FileMD5, task.UserID, model.VectorizeProcessing, "", estimatedTokens, estimatedChunks, 0, 0)

	// 阶段一：将分块文本和元数据存入数据库
	log.Info("[Processor] 阶段一: 开始将分块文本存入数据库")
	// 为避免重复写入导致的累计膨胀，处理前先清理该文件在本用户下的既有分块记录（幂等，按用户隔离）
	if err := p.docVectorRepo.DeleteByFileMD5AndUser(task.FileMD5, task.UserID); err != nil {
		log.Warnf("[Processor] 清理 document_vectors 旧记录失败 (file_md5=%s, user_id=%d): %v", task.FileMD5, task.UserID, err)
	}
	dbVectors := make([]*model.DocumentVector, 0, len(chunks))
	for i, chunk := range chunks {
		dbVectors = append(dbVectors, &model.DocumentVector{
			FileMD5:     task.FileMD5,
			ChunkID:     i,
			TextContent: chunk.Text,
			UserID:      task.UserID,
			OrgTag:      task.OrgTag,
			IsPublic:    task.IsPublic,
		})
	}
	if err := p.docVectorRepo.BatchCreate(dbVectors); err != nil {
		log.Errorf("[Processor] 阶段一: 批量保存文本分块到数据库失败, Error: %v", err)
		return fmt.Errorf("批量保存文本分块失败: %w", err)
	}
	log.Infof("[Processor] 阶段一: 成功将 %d 个分块存入数据库", len(dbVectors))

	// 阶段二：从数据库读取，进行向量化，然后索引到ES
	log.Info("[Processor] 阶段二: 开始从数据库读取分块并进行向量化")
	savedVectors, err := p.docVectorRepo.FindByFileMD5AndUser(task.FileMD5, task.UserID)
	if err != nil {
		log.Errorf("[Processor] 阶段二: 从数据库读取分块失败, FileMD5: %s, Error: %v", task.FileMD5, err)
		return fmt.Errorf("从数据库读取分块失败: %w", err)
	}
	log.Infof("[Processor] 阶段二: 成功从数据库读取 %d 个分块", len(savedVectors))

	// 4. 向量化并索引到 ES（并发 + 信号量限流，加速大文件处理）
	log.Infof("[Processor] 步骤4: 开始并发向量化与索引，共 %d 个分块", len(savedVectors))
	const embedConcurrency = 4
	sem := make(chan struct{}, embedConcurrency)
	errCh := make(chan error, len(savedVectors))
	var wg sync.WaitGroup
	for _, docVector := range savedVectors {
		wg.Add(1)
		sem <- struct{}{}
		go func(docVector *model.DocumentVector) {
			defer wg.Done()
			defer func() { <-sem }()

			// 4a. 向量化（Contextual Retrieval：向量化时附加文档级上下文）
			embedText := docVector.TextContent
			if config.Conf.Chunking.Contextual.Enabled && parsedTitle != "" {
				embedText = "文档《" + parsedTitle + "》相关段落：" + docVector.TextContent
			}
			vector, err := p.embeddingClient.CreateEmbedding(ctx, embedText)
			if err != nil {
				log.Errorf("[Processor] 分块 %d 向量化失败, Error: %v", docVector.ChunkID, err)
				errCh <- fmt.Errorf("块 %d 向量化失败: %w", docVector.ChunkID, err)
				return
			}

			// 4b. 准备 ES 的 EsDocument 对象
			// VectorID 加入 user_id 维度，避免同 MD5 文档被不同用户上传时互相覆盖
			esDoc := model.EsDocument{
				VectorID:     fmt.Sprintf("%s_%d_%d", docVector.FileMD5, docVector.UserID, docVector.ChunkID),
				FileMD5:      docVector.FileMD5,
				DocTitle:     parsedTitle,
				ChunkID:      docVector.ChunkID,
				TextContent:  docVector.TextContent,
				ParentText:   docVector.ParentText,
				Vector:       vector,
				ModelVersion: p.embeddingCfg.Model,
				UserID:       docVector.UserID,
				OrgTag:       docVector.OrgTag,
				IsPublic:     docVector.IsPublic,
			}
			// 稀疏向量（B3.3）：启用且服务可用时计算并写入
			if p.sparseClient != nil && config.Conf.Sparse.Enabled {
				sv, sErr := p.sparseClient.CreateSparse(ctx, docVector.TextContent)
				if sErr != nil {
					log.Warnf("[Processor] 分块 %d 稀疏向量失败: %v", docVector.ChunkID, sErr)
				} else if len(sv) > 0 {
					esDoc.SparseVector = sv
				}
			}

			// 4c. 索引到 Elasticsearch
			if err := es.IndexDocument(ctx, p.esCfg.IndexName, esDoc); err != nil {
				log.Errorf("[Processor] 索引分块 %d 到Elasticsearch失败, Error: %v", docVector.ChunkID, err)
				errCh <- fmt.Errorf("索引块 %d 到 Elasticsearch 失败: %w", docVector.ChunkID, err)
				return
			}
			log.Infof("[Processor] 分块 %d 向量化并索引成功", docVector.ChunkID)
		}(docVector)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			log.Errorf("[Processor] 向量化/索引存在失败: %v", err)
			return err
		}
	}
	log.Info("[Processor] 步骤4: 所有分块处理完毕")

	// 依据最终入库分块统计实际 token 与切片数（知识库列表“实际向量化”）
	for _, docVector := range savedVectors {
		embedText := docVector.TextContent
		if config.Conf.Chunking.Contextual.Enabled && parsedTitle != "" {
			embedText = "文档《" + parsedTitle + "》相关段落：" + docVector.TextContent
		}
		actualTokens += tokencount.Estimate(embedText)
	}
	actualChunks = len(savedVectors)
	_ = p.uploadRepo.UpdateVectorized(task.FileMD5, task.UserID, model.VectorizeSuccess, "", estimatedTokens, estimatedChunks, actualTokens, actualChunks)

	log.Infof("[Processor] 文件处理成功完成, FileMD5: %s", task.FileMD5)
	return nil
}

// captionImages 对解析出的图片生成描述并转为可检索分块（多模态入库）。
func (p *Processor) captionImages(ctx context.Context, parsed *parser.ParsedDoc, startIndex int) []chunker.Chunk {
	var out []chunker.Chunk
	for _, img := range parsed.Images {
		caption, err := p.visionClient.Caption(ctx, img.Name, img.Data)
		if err != nil {
			log.Errorf("[Processor] 图片描述失败 %s: %v", img.Name, err)
			continue
		}
		text := "【图片】" + img.Name + "：" + caption
		out = append(out, chunker.Chunk{Index: startIndex + len(out), Text: text})
	}
	return out
}
