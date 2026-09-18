// Package main 是 API 网关与文档处理 Worker 的入口。
package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ericthz/zebra-rag/internal/bootstrap"
	"github.com/ericthz/zebra-rag/internal/config"
	"github.com/ericthz/zebra-rag/internal/infra/kafka"
	"github.com/ericthz/zebra-rag/internal/middleware"
	"github.com/ericthz/zebra-rag/internal/module/admin"
	"github.com/ericthz/zebra-rag/internal/module/auth"
	"github.com/ericthz/zebra-rag/internal/module/chat"
	"github.com/ericthz/zebra-rag/internal/module/conversation"
	"github.com/ericthz/zebra-rag/internal/module/document"
	"github.com/ericthz/zebra-rag/internal/module/search"
	"github.com/ericthz/zebra-rag/internal/module/upload"
	"github.com/ericthz/zebra-rag/internal/module/user"
	"github.com/ericthz/zebra-rag/internal/module/vision"
	"github.com/ericthz/zebra-rag/internal/repository"
	"github.com/ericthz/zebra-rag/internal/service"
	"github.com/ericthz/zebra-rag/pkg/log"
	"github.com/ericthz/zebra-rag/pkg/metrics"

	"github.com/gin-gonic/gin"
)

func main() {
	// 1. 初始化配置与日志
	config.Init(config.ResolvePath("./configs/config.yaml"))
	cfg := config.Conf

	log.Init(cfg.Log.Level, cfg.Log.Format, cfg.Log.OutputPath)
	defer log.Sync()
	log.Info("日志记录器初始化成功")

	// 2. 装配应用（基础设施、仓储、服务、处理流水线）
	app, err := bootstrap.New(&cfg)
	if err != nil {
		log.Fatalf("应用装配失败: %v", err)
	}

	// 3. 后台启动 Kafka 消费者，异步处理文档入库任务
	go kafka.StartConsumer(cfg.Kafka, app.Processor)

	// 4. 导入 initfile 目录种子文件（复用标准上传+合并流程，幂等）
	initCtx, cancelInit := context.WithCancel(context.Background())
	defer cancelInit()
	go initSeedFiles(initCtx, "initfile", app.UserRepository, app.UploadService)

	// 5. 路由引擎与注册
	gin.SetMode(cfg.Server.Mode)
	r := gin.New()
	r.Use(middleware.RequestIDMiddleware(), middleware.RequestLogger(), gin.Recovery())
	registerRoutes(r, app)

	// 6. 启动 HTTP 服务并优雅停机
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", cfg.Server.Port),
		Handler: r,
	}

	go func() {
		log.Infof("服务启动于 %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP 服务监听失败: %s", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("接收到停机信号，正在关闭服务...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("HTTP 服务器关闭失败: %v", err)
	}
	log.Info("服务已优雅关闭")
}

// registerRoutes 通过各领域模块注册全部路由，并挂载指标端点。
func registerRoutes(r *gin.Engine, app *bootstrap.App) {
	apiV1 := r.Group("/api/v1")

	auth.RegisterRoutes(apiV1, app)
	user.RegisterRoutes(apiV1, app)
	upload.RegisterRoutes(apiV1, app)
	document.RegisterRoutes(apiV1, app)
	search.RegisterRoutes(apiV1, app)
	conversation.RegisterRoutes(apiV1, app)
	vision.RegisterRoutes(apiV1, app)
	chat.RegisterRoutes(apiV1, r, app)
	admin.RegisterRoutes(apiV1, app)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/metrics", func(c *gin.Context) {
		metrics.Handler(c.Writer, c.Request)
	})
}

// initSeedFiles 扫描目录下文件并通过标准上传流程导入（幂等）。
func initSeedFiles(ctx context.Context, dir string, userRepo repository.UserRepository, uploadSvc service.UploadService) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		log.Infof("initSeedFiles: 目录 '%s' 不存在或不可用，跳过初始化导入", dir)
		return
	}

	// 选择归属用户：优先 admin，不存在则取第一个
	var ownerUserID uint
	var ownerOrg string
	if admin, err := userRepo.FindByUsername("admin"); err == nil && admin != nil {
		ownerUserID = admin.ID
		ownerOrg = admin.PrimaryOrg
	} else {
		if users, err := userRepo.FindAll(); err == nil && len(users) > 0 {
			ownerUserID = users[0].ID
			ownerOrg = users[0].PrimaryOrg
		} else {
			log.Warnf("initSeedFiles: 未找到可用用户，跳过初始化导入")
			return
		}
	}

	walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}

		// 计算 MD5
		f, err := os.Open(path)
		if err != nil {
			log.Warnf("initSeedFiles: 打开文件失败: %s, err=%v", path, err)
			return nil
		}
		h := md5.New()
		size, copyErr := io.Copy(h, f)
		_ = f.Close()
		if copyErr != nil {
			log.Warnf("initSeedFiles: 读取文件失败: %s, err=%v", path, copyErr)
			return nil
		}
		fileMD5 := fmt.Sprintf("%x", h.Sum(nil))
		fileName := info.Name()

		// 幂等检查：已完成则跳过
		if uploaded, ferr := uploadSvc.FastUpload(ctx, fileMD5, ownerUserID); ferr == nil && uploaded {
			log.Infof("initSeedFiles: 已存在，跳过: %s (md5=%s)", fileName, fileMD5)
			return nil
		}

		// 分片上传
		const chunkSize int64 = 5 * 1024 * 1024
		totalChunks := int(math.Ceil(float64(size) / float64(chunkSize)))
		if totalChunks == 0 {
			log.Infof("initSeedFiles: 空文件跳过: %s", path)
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			log.Warnf("initSeedFiles: 重新打开文件失败: %s, err=%v", path, err)
			return nil
		}
		defer file.Close()

		for chunkIndex := 0; chunkIndex < totalChunks; chunkIndex++ {
			offset := int64(chunkIndex) * chunkSize
			if _, err := file.Seek(offset, io.SeekStart); err != nil {
				log.Warnf("initSeedFiles: Seek 失败: %s, chunk=%d, err=%v", path, chunkIndex, err)
				return nil
			}
			toRead := chunkSize
			if offset+toRead > size {
				toRead = size - offset
			}
			buf := make([]byte, toRead)
			if _, err := io.ReadFull(file, buf); err != nil {
				log.Warnf("initSeedFiles: 读取分片失败: %s, chunk=%d, err=%v", path, chunkIndex, err)
				return nil
			}
			// 适配 multipart.File
			cf := &chunkFile{Reader: bytes.NewReader(buf)}

			// 标记 is_public=true（全员可见），org 使用所有者主组织
			if _, _, err := uploadSvc.UploadChunk(ctx, fileMD5, fileName, size, chunkIndex, cf, ownerUserID, ownerOrg, true); err != nil {
				log.Warnf("initSeedFiles: 上传分片失败: %s, chunk=%d, err=%v", path, chunkIndex, err)
				return nil
			}
		}

		if _, err := uploadSvc.MergeChunks(ctx, fileMD5, fileName, ownerUserID); err != nil {
			log.Warnf("initSeedFiles: 合并失败: %s, err=%v", path, err)
			return nil
		}
		log.Infof("initSeedFiles: 导入完成并已触发向量化: %s", fileName)
		return nil
	})
	if walkErr != nil {
		log.Warnf("initSeedFiles: 遍历目录发生错误: %v", walkErr)
	}
}

// chunkFile 适配 bytes.Reader 到 multipart.File 所需接口
type chunkFile struct{ Reader *bytes.Reader }

func (c *chunkFile) Read(p []byte) (int, error)              { return c.Reader.Read(p) }
func (c *chunkFile) ReadAt(p []byte, off int64) (int, error) { return c.Reader.ReadAt(p, off) }
func (c *chunkFile) Seek(offset int64, whence int) (int64, error) {
	return c.Reader.Seek(offset, whence)
}
func (c *chunkFile) Close() error { return nil }
