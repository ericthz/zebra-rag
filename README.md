# Zebra RAG

English | [简体中文](README.zh-CN.md)

Zebra RAG is an **enterprise-grade AI knowledge base** built on RAG (Retrieval-Augmented Generation). It covers the full stack — **ingestion → hybrid retrieval → reranking → generation → evaluation** — as a Go + Vue 3 monorepo with multi-tenant isolation, and works with either local Ollama models or hosted APIs.

---

## Highlights

### Ingestion

- **Parser abstraction** — plain-text extraction via Tika, or layout-aware parsing (MinerU / Unstructured are pluggable) in `internal/parser/`
- **Multimodal indexing** — image understanding (Ollama vision model) turns pictures into searchable captions; `POST /api/v1/vision/caption`
- **Chunking strategies** — Markdown structure-aware chunking with a fixed-window fallback (`internal/chunker/`)
- **Parent-child chunks** — retrieve small chunks, generate from parent chunks (small-to-big)
- **Contextual retrieval** — document-level context is prepended before embedding to improve recall
- **Metadata extraction** — document title and type are indexed into Elasticsearch for filtering and citation

### Retrieval and generation

- **Multi-path recall** — dense KNN + BM25 + sparse vectors (BGE-M3 sparse, optional)
- **RRF fusion** plus an optional **cross-encoder reranker** (`pkg/rerank/`)
- **Query understanding** — LLM query rewriting, multi-query expansion, and intent routing (chitchat skips retrieval)
- **Agentic RAG** — self-evaluates retrieval quality and rewrites the query when results are insufficient (`internal/service/chat_service.go`)
- **Long-document Map-Reduce summarization** — `POST /api/v1/documents/:fileMd5/summarize`
- **Streaming chat** — WebSocket streaming, multi-conversation management, and citation tracing `(source#n: filename)`

### Engineering

- **Async pipeline** — Kafka producer/consumer with in-session retry, exponential backoff, parallel consumers, and idempotent cleanup
- **Two-level caching** — retrieval result cache and embedding cache (Redis, configurable TTL)
- **Performance** — concurrency-limited embedding; explicit timeouts on external APIs (embedding 2m, LLM streaming 90s); conversation history budget truncation (≤20 messages / ≤8000 characters)
- **Security** — short-lived one-time WebSocket token, per-session stop, file preview/download credentials carried in the `Authorization` header instead of the URL, login/register rate limiting, secrets injected via `ZEBRA_*` environment variables
- **Observability** — `/metrics` runtime metrics plus a retrieval latency histogram (Prometheus text format), and Request-ID propagated across every log line
- **RAG evaluation** — `cmd/eval` reports recall@k / MRR / hit-rate / faithfulness / context metrics; samples without annotations are skipped transparently
- **Data consistency** — same-MD5 documents are isolated per user (ID and idempotent cleanup include the user dimension); deletes cascade to Elasticsearch and the vector table via reference counting
- **Testing** — 8 unit test packages, 21 cases (chunking, parsing, evaluation, JWT, search, intent, multimodal, sparse, tracing, history truncation)
- **Deployment** — one-command dependencies via Docker Compose; optional AutoMigrate; multi-environment config (`ZEBRA_ENV`)

---

## Architecture

- **Backend** — Go 1.23+ / Gin, layered `handler` / `service` / `repository`, dependencies wired in `cmd/server/main.go`
- **Frontend** — Vue 3 + TypeScript + Vite + Naive UI (`frontend/`)
- **Database** — MySQL 8 (GORM, metadata), Redis 7 (cache / progress / conversations)
- **Search** — Elasticsearch 8.10 (KNN dense vectors + BM25 + `rank_features` sparse vectors)
- **Message queue** — Apache Kafka (async document processing)
- **Object storage** — MinIO (chunked upload and merge)
- **Document parsing** — Apache Tika (plus optional layout service)
- **AI services** — DeepSeek / Ollama (LLM), DashScope / Ollama (embedding), optional vision and sparse-vector services
- **Security** — JWT (access / refresh), hierarchical `org_tag` multi-tenant filtering

## Core flow

```
Upload -> MinIO chunk merge -> Kafka -> Parse (Tika / layout) -> Chunk -> Embed -> ES index
                                                                          |
User question -> WS -> intent routing -> query rewrite / multi-query -> multi-path recall -> RRF -> Reranker -> LLM streaming answer
```

---

## Implementation details

### 1. Ingestion pipeline

- **Chunked upload and instant upload.** The frontend splits files into 5 MB chunks and uses `fileMd5` as the file identity. `POST /upload/check` queries the database first to enable instant upload; if the file is incomplete it returns the list of uploaded chunks from a Redis bitmap so the upload can resume. Chunks land in MinIO under `chunks/{md5}/{idx}` with their MD5 recorded in `chunk_info` for integrity checks. On merge, a single chunk uses `CopyObject` while multiple chunks use `ComposeObject` to produce `merged/{md5}`, after which a Kafka message triggers async processing (`internal/service/upload_service.go`).
- **Parser abstraction.** `parser.Parser` is an interface: `tika` mode extracts plain text through Tika, `layout` mode talks to a MinerU / Unstructured layout-aware service, and failures fall back automatically (`internal/parser/parser.go`).
- **Multimodal indexing.** Images produced by layout parsing are captioned by an Ollama vision model and appended as searchable chunks in the form `[image] filename: caption`, bringing image content into the vector store (`captionImages` in `internal/pipeline/processor.go`).
- **Chunking.** Either `markdown` structure-aware chunking (split by heading level, falling back to a fixed window when a section is too large) or `fixed` windowing (1000 characters by default, overlap 100). Parent-child chunking is applied at the same time: small chunks (`text_content`) participate in retrieval while parent chunks (`parent_text`) feed generation, balancing recall precision against context completeness.
- **Contextual retrieval (lightweight).** A document-level title prefix such as `Passage from <title>: ...` is prepended before embedding — a cheap way to improve semantic recall.
- **Embedding.** OpenAI-compatible `/embeddings` endpoint with configurable dimensions (2048 by default); 4-way concurrency limited by a semaphore; results cached in Redis keyed by text MD5 with a configurable TTL, which cuts cost significantly on large files.
- **Sparse vectors.** An optional BGE-M3 sparse service emits `map[token]weight` written into the Elasticsearch `rank_features` field as a third recall path (`pkg/sparse/`).

### 2. Hybrid retrieval and query understanding

- **Three recall paths.** Dense KNN uses the Elasticsearch 8.10 top-level `knn` query (cosine) with `num_candidates = recall_k × 4` to leave headroom for approximate search. BM25 uses a keyword `match` boosted by `match_phrase`, after the query is cleaned of colloquial filler by `normalizeQuery`. Sparse uses term queries over `rank_features` boosted by token weight. Each path recalls `recall_k=60` candidates independently so one path never suppresses another, and vector document IDs are `md5_userID_chunkID` so the permission filter isolates users cleanly.
- **RRF fusion.** `score(id) = Σ 1/(k + rank)` with `k=60` fuses the paths by rank position, which needs no cross-path score normalization and is naturally robust.
- **Optional reranker.** A cross-encoder (bge-reranker-v2-m3) reranks the top-N; if the service is unavailable the system falls back to the RRF ordering (`pkg/rerank/`).
- **Query understanding** (each independently switchable): LLM query rewriting with fallback to the original query; multi-query expansion that retrieves several variants and fuses them with RRF; and intent routing in `rule` or `llm` mode that separates chitchat from retrieval so casual messages skip retrieval entirely, saving cost and latency (`internal/service/intent.go`).
- **Agentic self-check.** After the first retrieval round the top score is compared against a threshold (`min_score=0.02`); if it looks insufficient the query is rewritten and retrieval retried at most once, forming a retrieve → evaluate → rewrite → retrieve loop (`chat_service.retrieveWithSelfCheck`).
- **Permissions inside recall.** All three paths carry `filter: user_id = self OR is_public OR org_tag in effective tags`, so multi-tenant isolation happens at the retrieval layer rather than as a post-filter (`buildPermFilter`).

### 3. Generation and conversation

- **Streaming chat.** Answers stream incrementally over WebSocket and the frontend renders chunk by chunk. Context is assembled as system rules + retrieved snippets + conversation history + current question, with the system prompt declaring highest priority and constraining the citation format.
- **Citation tracing.** Snippets are injected as `[n] (filename) content`, the LLM answers with `(source#n: filename)`, and the frontend parses that into clickable file links that download the original file — every answer stays traceable.
- **Multi-conversation management.** Conversation metadata and message history live in Redis with a 7-day TTL, supporting list / switch / delete. Invalid conversation IDs degrade gracefully into a new conversation. History sent to the LLM is truncated by budget (`boundHistory`: ≤20 messages and ≤8000 characters, trimmed from the head) to keep first-token latency and cost under control.
- **Long-document Map-Reduce summarization.** Chunks are aggregated into segments of roughly 1500 characters, summarized 4-way concurrently (map), then merged into a full summary (reduce). The HTTP entry point validates file ownership or accessibility (self / globally public / organization tag) to prevent summarizing documents you cannot read (`internal/service/summarize_service.go`).

### 4. Multi-tenancy and permissions

- **Hierarchical organization tags.** `organization_tags` supports parent-child nesting and users may carry several tags. Retrieval expands a user's tags upward through all ancestors to produce an "effective tag set", so members of a child organization can search ancestor documents — organization-level sharing via `GetUserEffectiveOrgTags` (BFS with cycle protection).
- **Three visibility tiers.** `is_public` (globally public), `org_tag` (within organization), and `user_id` (owner only), with Elasticsearch retrieval and file listing sharing the same semantics.
- **Role control.** JWT claims carry the role and `AdminAuthMiddleware` isolates admin endpoints (user management, tag management, viewing all conversations).

### 5. Engineering and security

- **Async pipeline.** Kafka decouples upload from processing; a consumer group runs 3 instances in parallel (single partition preserves ordering). A single message is genuinely retried within the session (exponential backoff 1→2→4s, capped at 30s, up to 3 attempts) before the offset is committed and the message skipped — a simplified DLQ. Before processing, stale chunks are cleaned idempotently by `file_md5 + user_id` so duplicates never inflate.
- **API client timeouts.** Every external dependency has an explicit timeout: embedding 2 minutes; LLM streaming uses `ResponseHeaderTimeout` / `IdleConnTimeout` of 90s (without truncating the stream) so slow dependencies cannot hang the worker.
- **Two-level caching.** Retrieval cache (key includes userID + topK + query MD5 so users never see each other's data) and embedding cache, both with configurable TTL.
- **Security.** JWT HS256 access (24h) / refresh (7d), logout via a Redis blacklist; WebSocket uses a short-lived one-time token (1h TTL) instead of putting a long-lived JWT in the URL, and the stop command token is bound to the connection for per-session isolation. File preview and download credentials travel in the `Authorization` header (with `fileMd5` locating the exact file) rather than in the query string. Login and register endpoints are rate limited per IP (20 requests / minute). Secrets support `ZEBRA_*` environment injection and never touch disk.
- **Observability.** A Request-ID middleware spans HTTP and WebSocket (including client-provided request IDs) with zap structured logging. `/metrics` exposes Prometheus text format counters and latencies for retrieval and chat, plus the retrieval latency histogram (`zebrarag_search_latency_ms_bucket`, buckets from 50ms to 5s).
- **Configuration and deployment.** Multi-environment config (`ZEBRA_ENV=prod` loads `configs/config.prod.yaml`); Docker Compose brings up MySQL / Redis / MinIO / Tika / Kafka / Elasticsearch with the IK Chinese analyzer installed on first boot; `docs/ddl.sql` initializes the schema and seed data.

### 6. Evaluation

`cmd/eval` is an offline evaluation tool covering retrieval metrics (recall@k, MRR, hit-rate), context metrics (context precision / recall), and generation metrics (faithfulness, answer relevancy — keyword heuristics, no LLM judge required). It works as a regression gate, and every retrieval stage has a config switch for ablation studies. Samples missing `expected_docs` annotations are skipped transparently (log plus a `skipped` counter in the report) so they never pollute the metrics.

### 7. Trade-offs

**Core design principles**

1. **Measurable quality.** Every retrieval stage has a config switch and can be enabled or disabled independently, enabling ablation comparisons (single path vs. multi-path RRF fusion) driven by metrics rather than intuition.
2. **Production thinking.** Async pipeline (Kafka), two-level caching, concurrency limits, short-lived tokens, multi-tenant permissions, and request tracing — each design maps to a concrete engineering problem and trade-off.
3. **Targeting RAG's quality bottlenecks.** Query rewriting / multi-query / intent routing / agentic self-check address query expression drift, insufficient retrieval, and wasteful retrieval on chitchat respectively, each with a fallback (original query, rule-based classification, threshold self-check).

**Current boundaries and next steps**

- Same-MD5 cross-user isolation is hardened: both Elasticsearch document IDs and `document_vectors` idempotent cleanup include the user dimension; MinIO merged objects are shared by MD5 with reference counting, deleted only when no other user references them.
- Document deletion cascades to the Elasticsearch index and vector table (reference-counted by `file_md5` so shared files are never wrongly removed).
- Kafka failure retry now performs genuine in-session retry with exponential backoff (up to 3 times); it is still "retry until exhausted, then skip" semantics, and a proper dead-letter queue (DLQ) is the natural next step.
- The summarize endpoint validates file ownership / accessibility; download and preview support precise location by `fileMd5`, preferring the current user's most recent upload when names collide.
- GraphRAG / knowledge graphs are future work (see Roadmap below).

## Quick start

### 1. Start dependencies (Docker Compose)

```bash
docker compose -f deployments/docker-compose.yaml up -d
```

### 2. Configure

Edit `configs/config.yaml`: database / Redis / MinIO credentials must match Compose; configure the embedding and LLM models.

> Secrets can be overridden by environment variables so they never touch disk: `ZEBRA_DATABASE_MYSQL_DSN`, `ZEBRA_DATABASE_REDIS_PASSWORD`, `ZEBRA_JWT_SECRET`, `ZEBRA_MINIO_ACCESS_KEY`, `ZEBRA_MINIO_SECRET_ACCESS_KEY`, `ZEBRA_EMBEDDING_API_KEY`, `ZEBRA_LLM_API_KEY`.
> Multi-environment: `ZEBRA_ENV=prod` automatically loads `configs/config.prod.yaml`; `ZEBRA_CONFIG` can point at any path.

### 3. Start the backend

```bash
go run cmd/server/main.go     # listens on 8081 by default
```

### 4. Start the frontend

```bash
cd frontend
pnpm install
pnpm run dev
```

---

## Testing and evaluation

```bash
# Unit tests
go test ./...

# Offline RAG evaluation (requires dependencies and an embedding service)
go run ./cmd/eval -config ./configs/config.yaml -dataset ./eval/dataset.json -k 10
```

