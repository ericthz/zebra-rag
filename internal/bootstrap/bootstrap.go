// Package bootstrap 负责依赖装配：把配置、基础设施、仓储与服务组装为可运行的应用对象。
// API 网关与处理 Worker 均从这里拉取实例，避免重复初始化与组件发散。
package bootstrap

import (
	"fmt"
	"time"

	"github.com/ericthz/zebra-rag/internal/chunker"
	"github.com/ericthz/zebra-rag/internal/config"
	"github.com/ericthz/zebra-rag/internal/infra/database"
	"github.com/ericthz/zebra-rag/internal/infra/embedding"
	"github.com/ericthz/zebra-rag/internal/infra/es"
	"github.com/ericthz/zebra-rag/internal/infra/kafka"
	"github.com/ericthz/zebra-rag/internal/infra/llm"
	"github.com/ericthz/zebra-rag/internal/infra/rerank"
	"github.com/ericthz/zebra-rag/internal/infra/sparse"
	"github.com/ericthz/zebra-rag/internal/infra/storage"
	"github.com/ericthz/zebra-rag/internal/infra/tika"
	"github.com/ericthz/zebra-rag/internal/infra/vision"
	"github.com/ericthz/zebra-rag/internal/model"
	"github.com/ericthz/zebra-rag/internal/parser"
	"github.com/ericthz/zebra-rag/internal/pipeline"
	"github.com/ericthz/zebra-rag/internal/repository"
	"github.com/ericthz/zebra-rag/internal/service"
	"github.com/ericthz/zebra-rag/pkg/log"
	"github.com/ericthz/zebra-rag/pkg/token"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

// App 是网关与 Worker 共享的应用容器。
type App struct {
	Config *config.Config

	DB  *gorm.DB
	RDB *redis.Client

	JWTManager *token.JWTManager

	UserService         service.UserService
	AdminService        service.AdminService
	UploadService       service.UploadService
	DocumentService     service.DocumentService
	SummarizeService    service.SummarizeService
	SearchService       service.SearchService
	ConversationService service.ConversationService
	ChatService         service.ChatService
	VisionService       service.VisionService

	UserRepository      repository.UserRepository
	UploadRepository    repository.UploadRepository
	WSSessionRepository repository.WSSessionRepository

	Processor *pipeline.Processor

	RateLimit       int
	RateLimitWindow time.Duration
}

// New 按照固定顺序初始化配置依赖并装配完整的应用对象。
func New(cfg *config.Config) (*App, error) {
	// 基础设施
	database.InitMySQL(cfg.Database.MySQL.DSN)
	if cfg.Database.MySQL.AutoMigrate {
		if err := database.DB.AutoMigrate(
			&model.User{},
			&model.OrganizationTag{},
			&model.FileUpload{},
			&model.ChunkInfo{},
			&model.DocumentVector{},
		); err != nil {
			return nil, fmt.Errorf("数据库结构自动迁移失败: %w", err)
		}
		log.Info("数据库结构自动迁移完成（auto_migrate=true）")
	}
	database.InitRedis(cfg.Database.Redis.Addr, cfg.Database.Redis.Password, cfg.Database.Redis.DB)
	storage.InitMinIO(cfg.MinIO)
	if err := es.InitES(cfg.Elasticsearch); err != nil {
		return nil, fmt.Errorf("es 初始化失败: %w", err)
	}
	kafka.InitProducer(cfg.Kafka)

	// 数据访问层
	userRepository := repository.NewUserRepository(database.DB)
	orgTagRepo := repository.NewOrgTagRepository(database.DB)
	uploadRepo := repository.NewUploadRepository(database.DB, database.RDB)
	conversationRepo := repository.NewConversationRepository(database.RDB)
	wsSessionRepo := repository.NewWSSessionRepository(database.RDB)
	docVectorRepo := repository.NewDocumentVectorRepository(database.DB)

	// 基础设施客户端（Parser / 模型 / 向量等）
	jwtManager := token.NewJWTManager(cfg.JWT.Secret, cfg.JWT.AccessTokenExpireHours, cfg.JWT.RefreshTokenExpireDays)
	tikaClient := tika.NewClient(cfg.Tika)
	docParser := parser.New(cfg.Parser.Mode, cfg.Parser.LayoutURL, tikaClient)
	var visionClient *vision.Client
	var visionService service.VisionService
	if cfg.Vision.Enabled {
		visionClient = vision.NewClient(cfg.Vision)
		visionService = service.NewVisionService(visionClient)
		log.Infof("多模态已启用: %s (%s)", cfg.Vision.BaseURL, cfg.Vision.Model)
	}
	embeddingClient := embedding.NewClient(cfg.Embedding)
	llmClient := llm.NewClient(cfg.LLM)

	// 服务层
	userService := service.NewUserService(userRepository, orgTagRepo, conversationRepo, uploadRepo, jwtManager)
	adminService := service.NewAdminService(orgTagRepo, userRepository, conversationRepo)
	uploadService := service.NewUploadService(uploadRepo, userRepository, cfg.MinIO)
	documentService := service.NewDocumentService(uploadRepo, userRepository, orgTagRepo, cfg.MinIO, tikaClient, docVectorRepo, es.ESClient)
	summarizeService := service.NewSummarizeService(docVectorRepo, uploadRepo, userService, llmClient)
	var rerankClient *rerank.Client
	if cfg.Rerank.Enabled {
		rerankClient = rerank.NewClient(cfg.Rerank.BaseURL, cfg.Rerank.Model, cfg.Rerank.Timeout)
		log.Infof("Rerank 服务已启用: %s (%s)", cfg.Rerank.BaseURL, cfg.Rerank.Model)
	}
	var queryRewriter service.QueryRewriter
	if cfg.Search.QueryRewrite.Enabled {
		queryRewriter = service.NewQueryRewriter(llmClient, cfg.Search.QueryRewrite.Prompt, cfg.Search.QueryRewrite.MaxTokens)
	}
	var sparseClient sparse.Client
	if cfg.Sparse.Enabled {
		sparseClient = sparse.NewClient(cfg.Sparse.BaseURL, cfg.Sparse.Model, cfg.Sparse.Timeout)
		log.Infof("稀疏向量已启用: %s (%s)", cfg.Sparse.BaseURL, cfg.Sparse.Model)
	}
	searchService := service.NewSearchService(embeddingClient, es.ESClient, userService, uploadRepo, rerankClient, queryRewriter, sparseClient)
	conversationService := service.NewConversationService(conversationRepo)
	intentClassifier := service.NewIntentClassifier(cfg.Search.IntentRouting.Mode, llmClient)
	chatService := service.NewChatService(searchService, llmClient, conversationRepo, queryRewriter, intentClassifier)
	// visionService 在启用时赋值，未启用保持 nil，由路由处理器统一降级

	// 文档处理流水线
	processor := pipeline.NewProcessor(
		docParser,
		embeddingClient,
		cfg.Elasticsearch,
		cfg.MinIO,
		cfg.Embedding,
		uploadRepo,
		docVectorRepo,
		chunker.New(cfg.Chunking.Strategy),
		visionClient,
		sparseClient,
	)

	// 限流参数
	rlLimit := cfg.RateLimit.Limit
	rlWindow := time.Duration(cfg.RateLimit.WindowSeconds) * time.Second
	if !cfg.RateLimit.Enabled {
		rlLimit = 0
	}

	return &App{
		Config:              cfg,
		DB:                  database.DB,
		RDB:                 database.RDB,
		JWTManager:          jwtManager,
		UserService:         userService,
		AdminService:        adminService,
		UploadService:       uploadService,
		DocumentService:     documentService,
		SummarizeService:    summarizeService,
		SearchService:       searchService,
		ConversationService: conversationService,
		ChatService:         chatService,
		VisionService:       visionService,
		UserRepository:      userRepository,
		UploadRepository:    uploadRepo,
		WSSessionRepository: wsSessionRepo,
		Processor:           processor,
		RateLimit:           rlLimit,
		RateLimitWindow:     rlWindow,
	}, nil
}
