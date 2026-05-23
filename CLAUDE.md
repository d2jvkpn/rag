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
1. `CreateDocument` saves the file, creates the DB record at `status=uploaded`, then calls `taskQueue.Enqueue`
2. `processDocument()` runs parse → clean → chunk → write snapshot → `ReplaceChunks` → set `status=review_pending`
3. `RechunkDocument` re-enqueues with `rechunk=true`, which increments `chunk_version`

**Chunk snapshots** are written to `data/chunks/{knowledge_base_id}/{document_id}/chunks-vN.json`. Old versions are never overwritten; rechunk always creates a new version file.

**Auth:** JWT (HS256, `golang-jwt/jwt/v5`) stored in HttpOnly cookie `rag_session`. Each token carries a JTI (UUID). `withAuth()` reads the cookie, calls `authService.Me()` which parses the token and checks the blacklist, then sets `current_user` in the gin context. `Logout` extracts the JTI and adds it to the `TokenBlacklist` (`MemoryBlacklist` by default; `RedisBlacklist` when `redis.dsn` is set).

**API response envelope:**
- Success: `{"data": <payload>}`
- Error: `{"error": {"code": "...", "message": "...", "details": [...]}}`
- List: `{"data": {"items": [...], "page": 1, "page_size": N, "total": N}}`

Error codes: `validation_error`, `unauthorized`, `forbidden`, `not_found`, `conflict`, `unsupported_file_type`, `processing_failed`, `internal_error`.

**Migrations** live in `migrations/sql/` as numbered pairs (`*.up.sql` / `*.down.sql`). gorm auto-migrate is not used; schema changes always require a new migration file. Primary keys use `UUID PRIMARY KEY DEFAULT uuidv7()`.

**Config** (`internal/config/config.go`) reads `backend/configs/local.yaml` via viper. Key fields: `http_addr`, `data_dir`, `state_path`, `database.dsn`, `redis.dsn`, `jwt.secret`, `session_cookie`, `admin.username`, `admin.password`, `embedder.{base_url,api_key,model}`, `milvus.{addr,collection,dim}`. All optional sections are commented out in `local.yaml`; absent sections fall back to Noop implementations.

### Frontend

**Bootstrap sequence:** `main.js` calls `loadConfig()` (fetches `/app.json`) before mounting the app. If config fails, a fatal error is shown. All service modules call `getConfig()` at request time — they do not cache the base URL.

**Services** (`services/`) wrap `fetch` via `services/http.js`. All requests use `credentials: 'include'`. Errors are thrown as `HttpError` with `.status`, `.code`, `.message`, `.details`.

**Polling:** `DocumentDetailPage` polls `GET /api/documents/:id` every `pollIntervalMs` ms (from `app.json`, default 3000) while the document status is not in `['indexed', 'failed', 'review_pending']`. The timer is cleared on `onUnmounted`.

**Status/type mapping** is centralised in `utils/status.js`. Use `STATUS_LABEL`, `STATUS_TYPE`, `isTerminal()` — do not duplicate these mappings in components.

**Naive UI** is registered globally via `app.use(naive)` in `main.js`. Import components only when needed for render functions (e.g., inside `columns` definitions in `DocumentsPage`).

**Runtime config** lives in `frontend/public/app.json`. The frontend never reads build-time environment variables. Fields: `apiBase`, `appTitle`, `pollIntervalMs`, `humanReviewEnabled`.

## Key Conventions

- `knowledge_base_id` is a plain string (no UUID constraint), required on every document upload. It scopes deduplication (`knowledge_base_id + sha256` unique), file storage paths, and chunk snapshot paths.
- `ListDocuments(knowledgeBaseID string)` — pass empty string to return all documents; the filter is pushed to the DB query, not applied in memory.
- Tests use `t.TempDir()` + `JSONStore`. No database mocking; no external dependencies in tests.
- The `sessions` table (migration 000002) is unused — auth switched to JWT before it was needed.
- Frontend does not use TypeScript. Do not introduce it.
- `logger.L` defaults to `zap.NewNop()` at package level so tests that skip `logger.Init()` never panic. `Init()` is called only in `main.go`.
- `Embedder` (`internal/embedder/`) has `Noop` (default) and `OpenAI` implementations. `OpenAI` works against any OpenAI-compatible endpoint. Wire via `WithEmbedder()` on `NewDocumentService`. Activated when `embedder.base_url` + `embedder.api_key` are set in config.
- `VectorStore` (`internal/vectorstore/`) has `Noop` (default) and `Milvus` implementations. `Milvus` uses the Milvus REST API v2 (Milvus 2.4+, no SDK dependency). Wire via `WithVectorStore()`. Activated when `milvus.addr` is set in config.
- `TaskQueue` (`internal/queue/`) has `GoroutineQueue` (default, single worker goroutine) and `AsynqQueue` (Redis-backed, activated when `redis.dsn` is set). Wire via `WithTaskQueue()`. Queue selection happens inside `NewDocumentService`.
- Chunk review flow: `review_pending` → (approve) → `approved` → (index) → `processing/embed` → `processing/index` → `indexed`. `reject` marks individual chunks without changing document status.
- `POST /api/query` embeds the query, calls `VectorStore.Search`, then optionally calls `LLM.Complete` to generate an answer from the retrieved context. Returns `{ items, answer, query, knowledge_base_id }`. `answer` is `""` when LLM is Noop.
- `GET /api/knowledge-bases` returns distinct KB IDs with document counts by scanning the documents list.
- `LLM` interface (`internal/llm/`) has `Noop` (default) and `OpenAI` implementations. Wire via `WithLLM()`. Activated when `llm.base_url` + `llm.api_key` are set in config.
