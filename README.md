# RAG Document Processing Console

RAG Document Processing Console is a full-stack system for building searchable knowledge bases from `pdf`, `docx`, `pptx`, and Markdown documents.

It covers the operational RAG ingestion loop: upload documents, parse source files, split content into chunks, optionally review and edit chunks, generate embeddings, write vectors to Milvus, and search indexed knowledge bases.

## Features

- Cookie-based login with JWT sessions and optional TOTP.
- Knowledge base management with Milvus collections.
- Document upload, status tracking, detail views, rechunking, and deletion.
- Parsers for extractable PDF, DOCX, PPTX, and Markdown files.
- Structure-aware chunking with token-length fallback.
- Human review actions for chunk reject, restore, edit, merge, and approve.
- Embedding through OpenAI-compatible APIs.
- Milvus vector indexing and semantic search.
- Local development mode with JSON persistence and in-process jobs.
- Optional PostgreSQL persistence, Redis token blacklist, and Redis-backed Asynq jobs.

## Repository Layout

```text
backend/   Go backend, API, document processing, persistence, queues, embedding, Milvus
frontend/  Vue 3 + Vite frontend console
docs/      Architecture, API, workflow, data model, backend, and frontend docs
deploy/    Containerfile and compose example
```

## Architecture

Backend request flow:

```text
gin router
  -> auth middleware
  -> API handlers
  -> services
  -> repository store
  -> JSON store or PostgreSQL
```

Document lifecycle:

```text
uploaded
  -> processing/parse
  -> processing/chunk
  -> review_pending
  -> approved
  -> processing/embed
  -> processing/index
  -> indexed
```

When human review is disabled, documents skip `review_pending` and proceed directly to embedding and indexing after chunking.

## Tech Stack

Backend:

- Go
- Gin
- GORM
- PostgreSQL or local JSON store
- Redis and Asynq, optional
- Milvus, optional in local development
- OpenAI-compatible embedding APIs
- Python `pdfplumber` helper for PDF text extraction

Frontend:

- Vue 3
- Vite
- JavaScript
- Naive UI
- Pinia
- vue-router
- dayjs

## Requirements

- Go matching [backend/go.mod](backend/go.mod)
- Node.js and npm
- Python 3 for PDF parsing support
- PostgreSQL, optional
- Redis, optional
- Milvus, optional for local development but required for real vector indexing
- An OpenAI-compatible embedding endpoint for real embeddings

## Quick Start

Install frontend dependencies:

```bash
cd frontend
npm install
```

Install PDF parser dependencies:

```bash
cd backend
pip install -r scripts/requirements.txt
```

Start the backend:

```bash
cd backend
go run ./cmd/server --config examples/local.yaml
```

The backend listens on `:3061` by default. Override it with `--addr`:

```bash
go run ./cmd/server --config examples/local.yaml --addr :9000
```

Start the frontend in another terminal:

```bash
cd frontend
npm run dev
```

The example config defines a local account:

```text
username: admin
password: admin123
```

Change the JWT secret, account passwords, API keys, and service DSNs before using the project outside local development.

## Configuration

The backend loads YAML configuration with Viper. See [backend/examples/local.yaml](backend/examples/local.yaml).

Common backend fields:

- `app.name`: application name.
- `app.data_dir`: document, snapshot, and static asset root.
- `app.state_path`: JSON store path when PostgreSQL is not enabled.
- `accounts[]`: bootstrap users and permissions.
- `http.base_path`: optional route prefix.
- `http.jwt_secret`: JWT signing secret.
- `http.jwt_token_ttl`: session lifetime, such as `8h`.
- `http.session_cookie`: HttpOnly cookie name.
- `http.allow_origins`: CORS allowlist.
- `database.dsn`: enables PostgreSQL when set.
- `redis.dsn`: enables Redis token blacklist and Asynq queue when set.
- `embedder.base_url`, `embedder.api_key`, `embedder.model`, `embedder.batch_size`, `embedder.dim`: embedding settings.
- `milvus.addr`, `milvus.db`: Milvus connection.

Runtime behavior:

- If `database.dsn` is empty, the backend uses the local JSON store.
- If `redis.dsn` is empty, the backend uses an in-process queue and memory token blacklist.
- If embedding or Milvus settings are empty, the corresponding integration falls back to a Noop implementation.
- `embedder.dim` is required and must be positive.

The frontend loads public runtime configuration from [frontend/public/app.json](frontend/public/app.json). Do not put secrets in this file.

## Development Commands

Backend commands run from `backend/`:

```bash
go build ./...
go test ./...
go test ./internal/api/...
go run ./cmd/server --config examples/local.yaml
```

Frontend commands run from `frontend/`:

```bash
npm run dev
npm run build
npm run preview
```

Root Makefile shortcuts:

```bash
make backend_check
make backend_run
make frontend_run
make frontend_build
```

## Production Build

Build the frontend:

```bash
cd frontend
npm run build
```

The frontend output is written to `frontend/target/dist/`. The backend can serve the SPA when the built UI is available under the expected `ui` target path; see [docs/architecture.md](docs/architecture.md) for serving details.

Container and compose examples:

- [deploy/Containerfile](deploy/Containerfile)
- [deploy/compose.rag.yaml](deploy/compose.rag.yaml)

## Documentation

Detailed documentation lives under [docs/](docs):

- [Architecture](docs/architecture.md)
- [Design decisions](docs/design.md)
- [Workflow](docs/workflow.md)
- [Backend](docs/backend.md)
- [API](docs/api.md)
- [Data model](docs/data-model.md)
- [Frontend business design](docs/frontend-business.md)
- [Frontend technical design](docs/frontend.md)

## Current Scope

The current implementation targets the first complete RAG ingestion loop. OCR for scanned PDFs, advanced media understanding, and complex table recovery are outside the core path. Extractable PDFs, DOCX, PPTX, and Markdown are supported, with best-effort table extraction and Markdown table conversion where implemented.

## TODO
1. support document formats: html/url, epub/epub3, odt, odp
