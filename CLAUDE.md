# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

### Backend

All commands run from `backend/`.

```bash
go build ./...                          # compile check
go test ./...                           # all tests
go test ./internal/api/...              # single package
go test -run TestDocumentLifecycle ./internal/api/  # single test
go run cmd/server/main.go               # dev server (JSONStore, configs/local.yaml)
go run cmd/server/main.go --addr :9000  # override port
```

### Frontend

All commands run from `frontend/`.

```bash
npm run dev    # dev server at :5173, proxies /api and /app.json to :8080
npm run build  # production build → frontend/target/dist/
```

## Architecture

### Backend

**Request path:** `gin router` → `withAuth()` middleware (JWT cookie) → handler → `DocumentService` / `AuthService` → `Store`

**Store interface** (`internal/repository/interface.go`) abstracts all persistence. Two implementations share this interface:
- `JSONStore` — file-backed, used when `database.dsn` is absent from config
- `PostgresStore` — gorm + lib/pq, used when `database.dsn` is set

`main.go` selects the store via `initStore(cfg)`. New persistence methods must be added to the interface and both implementations.

**Async document processing** is dispatched via the `queue.TaskQueue` interface. Two implementations: `GoroutineQueue` (buffered channel, default when `redis.dsn` is absent) and `AsynqQueue` (hibiken/asynq, used when `redis.dsn` is set). Selection happens in `NewDocumentService`. The flow is:
1. `CreateDocument` validates KB ID against configured Milvus collections, saves the file, creates the DB record at `status=uploaded`, then calls `taskQueue.Enqueue`
2. `processDocument()` runs parse → clean → chunk → write snapshot → `ReplaceChunks` → set `status=review_pending`
3. `RechunkDocument` re-enqueues with `rechunk=true`, which increments `chunk_version`. **Blocked if `status=indexed`.**

**Document lifecycle (with human review):**
```
uploaded → processing/parse → processing/chunk → review_pending
→ (approve) → approved → processing/embed → processing/index → indexed
```
- `approve` automatically triggers indexing immediately (no separate manual step).
- Once `indexed`, the document is immutable — rechunk is blocked. To reprocess, delete and re-upload.
- `failed` documents can be retried via `POST /api/documents/:id/index` without re-uploading.

**Chunk review operations** (only when `human_review=true`):
- `reject` — marks chunk `IsCurrent=false, status=rejected`; does not change document status
- `restore` — restores a rejected chunk to `draft`
- `edit` — updates chunk text, sets `source=manual`
- `merge` — merges adjacent (consecutive `chunk_index`) chunks into one; backend enforces adjacency
- `approve` — all `draft` chunks → `approved`, then auto-triggers indexing

**Chunk snapshots** are written to `data/chunks/{knowledge_base_id}/{document_id}/chunks-vN.json`.
- Written after chunking (text only, no vectors).
- Overwritten after embedding to include `embedding` vectors and `embedding_model` field.
- Old chunk versions are never overwritten; rechunk always creates a new version file.
- Embedding vectors are written to the snapshot **before** Milvus upsert, so Milvus can be rebuilt from snapshots without re-calling the embedding API.

**Auth:** JWT (HS256, `golang-jwt/jwt/v5`) stored in HttpOnly cookie `rag_session`. Each token carries a JTI (UUID). `withAuth()` reads the cookie, calls `authService.Me()` which parses the token and checks the blacklist, then sets `current_user` in the gin context. `Logout` extracts the JTI and adds it to the `TokenBlacklist` (`MemoryBlacklist` by default; `RedisBlacklist` when `redis.dsn` is set).

**API response envelope:**
- Success: `{"data": <payload>}`
- Error: `{"error": {"code": "...", "message": "...", "details": [...]}}`
- List: `{"data": {"items": [...], "page": 1, "page_size": N, "total": N}}`

List endpoints always return 200 with `items: []` when empty — never 404.

Error codes: `validation_error`, `unauthorized`, `forbidden`, `not_found`, `conflict`, `unsupported_file_type`, `processing_failed`, `internal_error`.

**Migrations** live in `migrations/sql/` as numbered pairs (`*.up.sql` / `*.down.sql`). gorm auto-migrate is not used; schema changes always require a new migration file. Primary keys use `UUID PRIMARY KEY DEFAULT uuidv7()`.

**Config** reads `backend/configs/local.yaml` via viper. Key fields: `http_addr`, `data_dir`, `state_path`, `database.dsn`, `redis.dsn`, `jwt.secret`, `session_cookie`, `admin.username`, `admin.password`, `embedder.{base_url,api_key,model}`, `milvus.{addr,db,collections[].{collection,dim}}`. All optional sections fall back to Noop implementations.

### Milvus / VectorStore

**Each knowledge base = one Milvus collection.** Collections are pre-configured in `local.yaml`:

```yaml
milvus:
  addr: "localhost:19530"
  db: rag
  collections:
  - collection: public
    dim: 1024
```

- On startup, `NewMilvus` calls `ensureDatabase` (creates `db` if absent, ignores already-exists) then `ensureCollection` for each configured collection.
- `knowledge_base_id` on a document must match a configured collection name; validated at upload time.
- `GET /api/knowledge-bases/available` returns the configured collection list (used by frontend dropdowns).
- `GET /api/knowledge-bases` returns KB IDs with document counts (from DB scan, not Milvus).
- All Milvus API calls include `dbName` in the request body.
- `DeleteByDocument` is called on document delete only when `status=indexed`.

### Frontend

**Bootstrap sequence:** `main.js` calls `loadConfig()` (fetches `/app.json`) before mounting the app. If config fails, a fatal error is shown. All service modules call `getConfig()` at request time — they do not cache the base URL.

**Services** (`services/`) wrap `fetch` via `services/http.js`. All requests use `credentials: 'include'`. Errors are thrown as `HttpError` with `.status`, `.code`, `.message`, `.details`.

**Polling:** `DocumentDetailPage` polls `GET /api/documents/:id` every `pollIntervalMs` ms (from `app.json`, default 3000) while the document status is not in `['indexed', 'failed', 'review_pending']`. The timer is cleared on `onUnmounted`.

**Status/type mapping** is centralised in `utils/status.js`. Use `STATUS_LABEL`, `STATUS_TYPE`, `isTerminal()` — do not duplicate these mappings in components.

**Naive UI** is registered globally via `app.use(naive)` in `main.js`. Import components only when needed for render functions (e.g., inside `columns` definitions in `DocumentsPage`).

**Runtime config** lives in `frontend/public/app.json`. The frontend never reads build-time environment variables. Fields: `apiBase`, `appTitle`, `pollIntervalMs`, `humanReviewEnabled`.

## Key Conventions

- `knowledge_base_id` must match a configured `milvus.collections[*].collection` name. Validated at `CreateDocument`. Scopes file storage paths and chunk snapshot paths.
- `ListDocuments(knowledgeBaseID string)` — pass empty string to return all documents; the filter is pushed to the DB query, not applied in memory.
- Tests use `t.TempDir()` + `JSONStore`. No database mocking; no external dependencies in tests.
- The `sessions` table (migration 000002) is unused — auth switched to JWT before it was needed.
- Frontend does not use TypeScript. Do not introduce it.
- `logger.L` defaults to `zap.NewNop()` at package level so tests that skip `logger.Init()` never panic. `Init()` is called only in `main.go`.
- `Embedder` (`internal/embedder/`) has `Noop` (default) and `OpenAI` implementations. `OpenAI` works against any OpenAI-compatible endpoint. Wire via `WithEmbedder()`. Activated when `embedder.base_url` + `embedder.api_key` are set in config. DashScope model: `text-embedding-v3` (dim=1024).
- `VectorStore` (`internal/vectorstore/`) has `Noop` (default) and `Milvus` implementations. `Milvus` uses the Milvus REST API v2 (Milvus 2.4+, no SDK dependency). Interface includes `ValidateKnowledgeBase`, `ListKnowledgeBases`, `Upsert`, `DeleteByDocument`, `Search`.
- `TaskQueue` (`internal/queue/`) has `GoroutineQueue` (default, single worker goroutine) and `AsynqQueue` (Redis-backed, activated when `redis.dsn` is set). Wire via `WithTaskQueue()`. Queue selection happens inside `NewDocumentService`.
- `POST /api/query` embeds the query, calls `VectorStore.Search`, then optionally calls `LLM.Complete` to generate an answer from the retrieved context. Returns `{ items, answer, query, knowledge_base_id }`. `answer` is `""` when LLM is Noop.
- `LLM` interface (`internal/llm/`) has `Noop` (default) and `OpenAI` implementations. Wire via `WithLLM()`. Activated when `llm.base_url` + `llm.api_key` are set in config.
- DOCX/PPTX parsing: `extractParagraphText` in `parser.go` groups `<w:t>` runs within the same `<w:p>` paragraph without separators — preserves words split across runs by mixed formatting.
