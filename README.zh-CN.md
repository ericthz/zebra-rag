[English](README.md) | 简体中文

# Zebra RAG

Zebra RAG 是一个**企业级 AI 知识库管理系统**，采用 RAG（检索增强生成）技术，覆盖**文档摄取 → 混合检索 → 精排 → 生成 → 评测**的完整技术栈。Go + Vue3 全栈，多租户架构，支持本地 Ollama 或云端模型。

---

## 功能亮点（RAG 全技术栈）

### 文档摄取（Ingestion）

- **解析抽象**：Tika 纯文本 / 布局感知解析（MinerU/Unstructured 服务可配，`internal/parser/`）
- **多模态入库**：图片理解（Ollama 视觉模型）→ 描述文本 → 可检索；`POST /api/v1/vision/caption`
- **分块策略**：Markdown 结构分块 + 固定窗口兜底（`internal/chunker/`）
- **Parent-Child**：小块检索、父块生成（small-to-big）
- **Contextual Retrieval**：向量化时附加文档级上下文，提升召回
- **元数据抽取**：文档标题 / 类型入 ES，用于过滤与引用

### 检索与生成（Retrieval & Generation）

- **多路召回**：稠密 KNN + BM25 + 稀疏向量（BGE-M3 sparse 可配）三路
- **RRF 融合** + **可配 cross-encoder Reranker**（`pkg/rerank/`）
- **查询理解**：LLM 查询改写 + 多查询扩展（Multi-Query）+ 意图路由（闲聊跳过检索）
- **Agentic RAG**：检索后自评，不充分则改写重检索（`internal/service/chat_service.go`）
- **长文档 Map-Reduce 总结**：`POST /api/v1/documents/:fileMd5/summarize`
- **流式对话**：WebSocket 增量 + 多会话管理（列表/切换/删除）+ 引用溯源 `(来源#编号: 文件名)`

### 工程化（Engineering）

- **异步流水线**：Kafka 生产/消费（同消息会话内真实重试、指数退避、多消费者并行、幂等清理）
- **缓存**：检索结果缓存 + Embedding 结果缓存（Redis，TTL 可配）
- **性能**：向量化并发限流；外部 API 显式超时（Embedding 2m、LLM 流式 90s）；会话历史预算截断（≤20 条 / ≤8000 字）
- **安全**：WS 短时一次性令牌、per-session 停止、文件预览/下载凭证走 Authorization 头（不拼 URL）、登录/注册限流、密钥环境变量注入（`ZEBRA_*`）
- **可观测**：`/metrics` 运行指标 + 检索延迟直方图（Prometheus 文本格式）+ Request-ID 全链路关联日志
- **RAG 评测**：`cmd/eval` 输出 recall@k / MRR / hit-rate / faithfulness / context 指标（缺失标注样本透明跳过）
- **数据一致性**：同 MD5 跨用户隔离（ID/幂等清理带用户维度）、删除级联清理 ES+向量表（引用计数）
- **测试**：8 个单测包、21 个用例（分块、解析、评测、JWT、搜索、意图、多模态、稀疏、链路、会话截断）
- **部署**：Docker Compose 一键拉起依赖；AutoMigrate（可选）；多环境配置（`ZEBRA_ENV`）

---

## 架构与技术栈

- **后端**：Go 1.23+ / Gin，分层 `handler/service/repository`，依赖注入于 `cmd/server/main.go`
- **前端**：Vue 3 + TypeScript + Vite + Naive UI（`frontend/`）
- **数据库**：MySQL 8（GORM，元数据）、Redis 7（缓存/进度/会话）
- **搜索**：Elasticsearch 8.10（KNN 稠密向量 + BM25 + rank_features 稀疏向量）
- **消息队列**：Apache Kafka（异步文档处理）
- **对象存储**：MinIO（分片上传/合并）
- **文档解析**：Apache Tika（+ 可选布局服务）
- **AI 服务**：DeepSeek / Ollama（LLM）、DashScope / Ollama（Embedding）、可选视觉 / 稀疏向量服务
- **安全**：JWT（access/refresh）、`org_tag` 多租户层级过滤

## 核心流程

```
文档上传 → MinIO 分片合并 → Kafka → 解析(Tika/布局) → 结构分块 → 向量化 → ES 索引
                                                                        ↓
用户问题 → WS → 意图路由 → 查询改写/多查询 → 多路召回 → RRF → Reranker → LLM 流式作答
```

---

## 技术实现细节

> 本节按链路分层说明**做了什么、为什么这么做、关键实现怎么落地**。

### 1. 文档摄取流水线（Ingestion）

- **分片上传与秒传**：前端按 5MB 分片，`fileMd5` 作为文件唯一标识；`POST /upload/check` 先查库实现秒传，未完成则返回 Redis bitmap 中的已传分片列表，断点续传。分片落 MinIO `chunks/{md5}/{idx}`，并计算每片 MD5 写入 `chunk_info`（完整性校验）；合并时单分片走 `CopyObject`、多分片走 `ComposeObject` 生成 `merged/{md5}`，完成后发 Kafka 消息触发异步处理（`internal/service/upload_service.go`）。
- **解析抽象**：`parser.Parser` 接口化，`tika` 模式走 Tika 纯文本抽取，`layout` 模式可对接 MinerU/Unstructured 布局感知服务，解析失败自动回退（`internal/parser/parser.go`）。
- **多模态入库**：布局解析出的图片经 Ollama 视觉模型生成描述，作为「【图片】文件名：描述」追加为可检索分块，让图片内容进入向量库（`internal/pipeline/processor.go` 的 `captionImages`）。
- **分块策略**：`markdown` 结构分块（按标题层级切分，块过大回退固定窗口）或 `fixed` 固定窗口（默认 1000 字、overlap 100）。同时落地 **Parent-Child**：小块（`text_content`）参与检索，父块（`parent_text`）供生成上下文，兼顾召回精度与上下文完整性。
- **Contextual Retrieval（轻量版）**：向量化时给分块附加文档级标题前缀（如 `文档《XX》相关段落：…`），用很小的成本提升语义召回。
- **向量化**：OpenAI 兼容 `/embeddings` 接口，维度可配（默认 2048）；并发 4 路 + 信号量限流；Embedding 结果按文本 MD5 缓存到 Redis（TTL 可配），大文件去重显著降本。
- **稀疏向量**：可选 BGE-M3 sparse 服务，输出 `map[token]weight` 写入 ES `rank_features` 字段，作为第三路召回（`pkg/sparse/`）。

### 2. 混合检索与查询理解（Retrieval）

- **三路召回**：
  - 稠密 KNN：ES 8.10 顶层 `knn`（cosine）语义召回，`num_candidates = recall_k × 4`（满足 ES 对候选数 ≥ k 的要求，提升召回质量）；
  - BM25：关键词 `match` + `match_phrase` boost（短语加权），先对查询做去口语词/正则清洗（`normalizeQuery`）；
  - 稀疏：`rank_features` 上的 term query 按 token 权重 boost。
  - 每路独立召回 `recall_k=60` 个候选，互不压制。ES 索引中同 MD5 文档的向量以 `md5_userID_chunkID` 作为文档 ID，配合权限过滤实现跨用户隔离。
- **RRF 融合**：`score(id) = Σ 1/(k + rank)`（`k=60`），对多路召回结果做位置加权融合，无需跨路归一化分数，天然鲁棒。
- **可选 Reranker**：cross-encoder（bge-reranker-v2-m3）对 top-N 精排，服务不可用时回退 RRF 排序（`pkg/rerank/`）。
- **查询理解**（均可配置开关）：查询改写（失败回退原句）；Multi-Query（多查询变体分别检索后 RRF 融合）；意图路由（`rule`/`llm` 区分闲聊与检索，闲聊直接作答跳过检索）。
- **Agentic 自评重检索**：首轮检索后按最高分阈值自评（`min_score=0.02`），不充分则 LLM 改写查询重检索，最多重试 1 次。
- **权限过滤内置于召回**：KNN/BM25/稀疏三路都带 `filter: user_id=本人 OR is_public OR org_tag ∈ 有效组织标签`（`buildPermFilter`）。

### 3. 生成与对话（Generation）

- **流式对话**：WebSocket 增量下发，前端逐 chunk 渲染；上下文按「系统规则 + 检索片段 + 会话历史 + 当前问题」组装。
- **引用溯源**：检索片段以 `[编号] (文件名) 内容` 注入 prompt，LLM 按 `(来源#编号: 文件名)` 输出，前端正则解析为可点击文件链接。
- **多会话管理**：会话元信息与消息历史存 Redis（7 天 TTL），支持列表/切换/删除；无效会话 ID 自动降级为新会话。送入 LLM 的历史做预算截断（`boundHistory`：≤20 条且累计 ≤8000 字，从头部裁减）。
- **长文档 Map-Reduce 总结**：把分块聚合成段（每段约 1500 字），4 路并发逐段总结（Map），再合成全文总结（Reduce）；入口按 HTTP 身份校验文件归属/可访问性（`internal/service/summarize_service.go`）。

### 4. 多租户与权限模型

- **组织标签层级**：`organization_tags` 支持父子层级，用户可挂多个 org_tag；检索/列表时向上递归展开得到「有效标签集合」（`GetUserEffectiveOrgTags`，BFS 防环）。
- **三层可见性**：`is_public`（全局公开）/ `org_tag`（组织内）/ `user_id`（本人）。
- **角色控制**：JWT claims 携带角色，`AdminAuthMiddleware` 做管理员接口隔离。

### 5. 工程化与安全（Engineering）

- **异步流水线**：消费者组 3 实例并行（单分区保序）；同一条消息在会话内**真实重试**（指数退避 1→2→4s、上限 30s、至多 3 次）后再失败才提交 offset 跳过（等价简化 DLQ）；处理前按 `file_md5 + user_id` 幂等清理旧分块。
- **API 客户端超时**：Embedding 单次 2 分钟；LLM 流式用 `ResponseHeaderTimeout`/`IdleConnTimeout` 90s（不截断流式输出）。
- **两级缓存**：检索结果缓存（key 含 userID + topK + query MD5）与 Embedding 缓存，TTL 均可配。
- **安全设计**：JWT HS256 access(24h)/refresh(7d)，登出走 Redis 黑名单；WS 短时一次性令牌（1h TTL）；停止指令令牌与连接绑定；文件预览/下载凭证走 `Authorization` 头；登录/注册按 IP 限流（20 次/分钟）；密钥支持 `ZEBRA_*` 环境变量注入。
- **可观测性**：Request-ID 中间件贯穿 HTTP 与 WS，zap 结构化日志；`/metrics` 暴露 Prometheus 文本格式指标与检索延迟直方图。
- **配置与部署**：多环境配置（`ZEBRA_ENV=prod`）；Docker Compose 一键拉起依赖，ES 首次启动自动安装 IK 中文分词插件；`docs/ddl.sql` 初始化表结构。

### 6. 评测体系（Evaluation）

`cmd/eval` 离线评测工具，支持检索层（recall@k、MRR、hit-rate）、上下文层（context precision/recall）、生成层（faithfulness、answer relevancy，关键词启发式）三档指标，可作回归门禁；缺失 `expected_docs` 标注的样本透明跳过。

### 7. 设计取舍

**核心设计思想**：效果可量化（每环节可配开关 + 消融对比）；生产化考量（异步、缓存、限流、短时令牌、多租户、链路追踪）；面向 RAG 质量瓶颈（改写/多查询/意图路由/Agentic 自评 + 各自兜底）。

**当前边界与演进方向**：同 MD5 跨用户隔离已加固，MinIO 合并对象按引用计数删除；删除文档已级联清理 ES 与向量表；Kafka 仍为「重试耗尽 + 跳过」的简化语义，后续可接入 DLQ；摘要接口已补充归属校验；GraphRAG 为后续规划。

## 快速开始

```bash
# 1. 启动依赖
docker compose -f deployments/docker-compose.yaml up -d

# 2. 配置 configs/config.yaml（数据库/Redis/MinIO 凭据与模型）

# 3. 启动后端（默认 8081）
go run cmd/server/main.go

# 4. 启动前端
cd frontend && pnpm install && pnpm run dev
```

> 密钥可通过环境变量注入覆盖（不落盘）：`ZEBRA_DATABASE_MYSQL_DSN`、`ZEBRA_DATABASE_REDIS_PASSWORD`、`ZEBRA_JWT_SECRET`、`ZEBRA_MINIO_ACCESS_KEY`、`ZEBRA_MINIO_SECRET_ACCESS_KEY`、`ZEBRA_EMBEDDING_API_KEY`、`ZEBRA_LLM_API_KEY`。

## 测试与评测

```bash
go test ./...
go run ./cmd/eval -config ./configs/config.yaml -dataset ./eval/dataset.json -k 10
```
