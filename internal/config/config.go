// Package config 负责加载和管理应用程序的配置。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// 全局配置变量，存储从配置文件加载的所有设置。
var Conf Config

// Config 是整个应用程序的配置结构体，与 config.yaml 文件结构对应。
type Config struct {
	Server        ServerConfig        `mapstructure:"server"`
	Database      DatabaseConfig      `mapstructure:"database"`
	JWT           JWTConfig           `mapstructure:"jwt"`
	Log           LogConfig           `mapstructure:"log"`
	Kafka         KafkaConfig         `mapstructure:"kafka"`
	Tika          TikaConfig          `mapstructure:"tika"`
	Elasticsearch ElasticsearchConfig `mapstructure:"elasticsearch"`
	MinIO         MinIOConfig         `mapstructure:"minio"`
	Embedding     EmbeddingConfig     `mapstructure:"embedding"`
	LLM           LLMConfig           `mapstructure:"llm"`
	Search        SearchConfig        `mapstructure:"search"`
	Rerank        RerankConfig        `mapstructure:"rerank"`
	Chunking      ChunkingConfig      `mapstructure:"chunking"`
	Parser        ParserConfig        `mapstructure:"parser"`
	RateLimit     RateLimitConfig     `mapstructure:"rate_limit"`
	Summarize     SummarizeConfig     `mapstructure:"summarize"`
	Vision        VisionConfig        `mapstructure:"vision"`
	Sparse        SparseConfig        `mapstructure:"sparse"`
}

// ServerConfig 存储服务器相关的配置。
type ServerConfig struct {
	Port string `mapstructure:"port"`
	Mode string `mapstructure:"mode"`
}

// DatabaseConfig 存储所有数据库连接的配置。
type DatabaseConfig struct {
	MySQL MySQLConfig `mapstructure:"mysql"`
	Redis RedisConfig `mapstructure:"redis"`
}

// MySQLConfig 存储 MySQL 数据库的配置。
type MySQLConfig struct {
	DSN         string `mapstructure:"dsn"`
	AutoMigrate bool   `mapstructure:"auto_migrate"` // 启动时自动迁移表结构（增量），默认 false
}

// RedisConfig 存储 Redis 的配置。
type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// JWTConfig 存储 JWT 相关的配置。
type JWTConfig struct {
	Secret                 string `mapstructure:"secret"`
	AccessTokenExpireHours int    `mapstructure:"access_token_expire_hours"`
	RefreshTokenExpireDays int    `mapstructure:"refresh_token_expire_days"`
}

// LogConfig 存储日志相关的配置。
type LogConfig struct {
	Level      string `mapstructure:"level"`
	Format     string `mapstructure:"format"`
	OutputPath string `mapstructure:"output_path"`
}

// KafkaConfig 存储 Kafka 相关的配置。
type KafkaConfig struct {
	Brokers string `mapstructure:"brokers"`
	Topic   string `mapstructure:"topic"`
}

// TikaConfig 存储 Tika 服务器相关的配置。
type TikaConfig struct {
	ServerURL string `mapstructure:"server_url"`
}

// ElasticsearchConfig 存储 Elasticsearch 相关的配置。
type ElasticsearchConfig struct {
	Addresses string `mapstructure:"addresses"`
	Username  string `mapstructure:"username"`
	Password  string `mapstructure:"password"`
	IndexName string `mapstructure:"index_name"`
}

// MinIOConfig 存储 MinIO 对象存储的配置。
type MinIOConfig struct {
	Endpoint        string `mapstructure:"endpoint"`
	AccessKeyID     string `mapstructure:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key"`
	UseSSL          bool   `mapstructure:"use_ssl"`
	BucketName      string `mapstructure:"bucket_name"`
}

// EmbeddingConfig 存储 Embedding 模型相关的配置。
type EmbeddingConfig struct {
	APIKey          string `mapstructure:"api_key"`
	BaseURL         string `mapstructure:"base_url"`
	Model           string `mapstructure:"model"`
	Dimensions      int    `mapstructure:"dimensions"`
	CacheTTLSeconds int    `mapstructure:"cache_ttl_seconds"` // Embedding 结果缓存（秒），0 禁用
}

// LLMConfig 存储大语言模型相关的配置。
type LLMConfig struct {
	APIKey     string              `mapstructure:"api_key"`
	BaseURL    string              `mapstructure:"base_url"`
	Model      string              `mapstructure:"model"`
	Generation LLMGenerationConfig `mapstructure:"generation"`
	Prompt     LLMPromptConfig     `mapstructure:"prompt"`
}

// LLMGenerationConfig 配置生成相关参数（可选）。
type LLMGenerationConfig struct {
	Temperature float64 `mapstructure:"temperature"`
	TopP        float64 `mapstructure:"top_p"`
	MaxTokens   int     `mapstructure:"max_tokens"`
}

// LLMPromptConfig 配置系统提示与上下文包裹格式（可选）。
type LLMPromptConfig struct {
	Rules        string `mapstructure:"rules"`
	RefStart     string `mapstructure:"ref_start"`
	RefEnd       string `mapstructure:"ref_end"`
	NoResultText string `mapstructure:"no_result_text"`
}

// ResolvePath 根据环境变量解析配置文件路径（多环境，C4.2）：
//   - ZEBRA_CONFIG 指定完整路径时优先；
//   - ZEBRA_ENV=prod 时尝试 configs/config.prod.yaml；
//   - 否则返回默认路径。
func ResolvePath(defaultPath string) string {
	if p := os.Getenv("ZEBRA_CONFIG"); p != "" {
		return p
	}
	if env := os.Getenv("ZEBRA_ENV"); env != "" {
		dir := filepath.Dir(defaultPath)
		ext := filepath.Ext(defaultPath)
		name := strings.TrimSuffix(filepath.Base(defaultPath), ext)
		candidate := filepath.Join(dir, name+"."+env+ext)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return defaultPath
}

// Init 初始化配置加载，从指定的路径读取 YAML 文件并解析到 Conf 变量中。
// 支持环境变量覆盖（前缀 ZEBRA_，层级用 _ 分隔），便于生产环境注入密钥而不落盘：
//
//	ZEBRA_DATABASE_MYSQL_DSN=user:pass@tcp(...)/db
//	ZEBRA_DATABASE_REDIS_PASSWORD=...
//	ZEBRA_JWT_SECRET=...
//	ZEBRA_MINIO_ACCESS_KEY=...  ZEBRA_MINIO_SECRET_ACCESS_KEY=...
//	ZEBRA_EMBEDDING_API_KEY=... ZEBRA_LLM_API_KEY=...
func Init(configPath string) {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	// 环境变量覆盖（层级键用 "_" 连接，如 database.mysql.dsn -> ZEBRA_DATABASE_MYSQL_DSN）
	viper.SetEnvPrefix("ZEBRA")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
	for _, key := range []string{
		"database.mysql.dsn",
		"database.redis.password",
		"jwt.secret",
		"minio.access_key_id",
		"minio.secret_access_key",
		"embedding.api_key",
		"llm.api_key",
	} {
		_ = viper.BindEnv(key)
	}

	if err := viper.ReadInConfig(); err != nil {
		panic(fmt.Errorf("读取配置文件失败: %w", err))
	}

	if err := viper.Unmarshal(&Conf); err != nil {
		panic(fmt.Errorf("无法将配置解析到结构体中: %w", err))
	}
}

// SearchConfig 检索相关参数。
type SearchConfig struct {
	RecallK         int                 `mapstructure:"recall_k"`          // 每路召回候选数（KNN / BM25），默认 60
	RRFK            int                 `mapstructure:"rrf_k"`             // RRF 融合常数 k，默认 60
	QueryRewrite    QueryRewriteConfig  `mapstructure:"query_rewrite"`     // LLM 查询改写
	CacheTTLSeconds int                 `mapstructure:"cache_ttl_seconds"` // 检索结果缓存 TTL（秒），0 表示禁用
	Agentic         AgenticConfig       `mapstructure:"agentic"`           // Agentic RAG（自评重检索）
	MultiQuery      MultiQueryConfig    `mapstructure:"multi_query"`       // 多查询扩展
	IntentRouting   IntentRoutingConfig `mapstructure:"intent_routing"`    // 意图路由
}

// IntentRoutingConfig 意图路由配置（闲聊 vs 检索）。
type IntentRoutingConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Mode    string `mapstructure:"mode"` // rule | llm
}

// MultiQueryConfig 多查询扩展配置：生成多个查询变体分别检索后融合。
type MultiQueryConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Count   int    `mapstructure:"count"` // 额外生成的查询数
	Prompt  string `mapstructure:"prompt"`
}

// AgenticConfig Agentic RAG 配置：检索后自评是否充分，不充分则改写重检索。
type AgenticConfig struct {
	Enabled    bool    `mapstructure:"enabled"`
	MinScore   float64 `mapstructure:"min_score"`   // 最高检索分低于此值视为不充分（RRF 分数量级）
	MaxRetries int     `mapstructure:"max_retries"` // 最多重检索次数
}

// QueryRewriteConfig 查询改写配置。
type QueryRewriteConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	Prompt    string `mapstructure:"prompt"`
	MaxTokens int    `mapstructure:"max_tokens"`
}

// RerankConfig cross-encoder 重排服务配置。
type RerankConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	BaseURL string `mapstructure:"base_url"`
	Model   string `mapstructure:"model"`
	TopN    int    `mapstructure:"top_n"`   // 参与重排的候选数
	Timeout int    `mapstructure:"timeout"` // 重排请求超时（秒）
}

// ChunkingConfig 文本分块配置。
type ChunkingConfig struct {
	Strategy     string           `mapstructure:"strategy"` // fixed | markdown
	ChunkSize    int              `mapstructure:"chunk_size"`
	ChunkOverlap int              `mapstructure:"chunk_overlap"`
	Contextual   ContextualConfig `mapstructure:"contextual"` // Contextual Retrieval
}

// ContextualConfig 向量化时附加文档级上下文（Anthropic Contextual Retrieval 轻量版）。
type ContextualConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// ParserConfig 文档解析配置。
type ParserConfig struct {
	Mode      string `mapstructure:"mode"`       // tika | layout
	LayoutURL string `mapstructure:"layout_url"` // 布局解析服务地址（MinerU/Unstructured）
}

// RateLimitConfig 接口限流配置。
type RateLimitConfig struct {
	Enabled       bool `mapstructure:"enabled"`
	Limit         int  `mapstructure:"limit"`          // 窗口内允许的最大请求数
	WindowSeconds int  `mapstructure:"window_seconds"` // 窗口（秒）
}

// SummarizeConfig 长文档 Map-Reduce 总结配置。
type SummarizeConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	ChunkSize    int    `mapstructure:"chunk_size"`
	MapPrompt    string `mapstructure:"map_prompt"`
	ReducePrompt string `mapstructure:"reduce_prompt"`
}

// VisionConfig 多模态（图片理解）配置。
type VisionConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	BaseURL string `mapstructure:"base_url"` // OpenAI 兼容端点（Ollama 视觉模型）
	Model   string `mapstructure:"model"`
	Prompt  string `mapstructure:"prompt"`
	Timeout int    `mapstructure:"timeout"`
}

// SparseConfig 稀疏向量（BGE-M3 sparse / SPLADE）配置。
type SparseConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	BaseURL string `mapstructure:"base_url"`
	Model   string `mapstructure:"model"`
	Timeout int    `mapstructure:"timeout"`
}
