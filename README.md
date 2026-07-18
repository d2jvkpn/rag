# RAG Document Processing Console

A full-stack system for building searchable knowledge bases from `pdf`, `docx`, `pptx`, and `md` documents — upload, parse, chunk, review, embed, index, and search, all from a web console.

## Quick Start

### Prerequisites

- Go matching [backend/go.mod](backend/go.mod)
- Node.js and npm
- Python 3 (for PDF parsing)

### Install dependencies

```bash
# frontend
cd frontend && npm install

# PDF parser
cd backend && pip install -r scripts/parse_pdf.pip.txt
```

### Run locally

```bash
# terminal 1: backend (listens on :3061)
cd backend
go run ./cmd/server --config examples/local.yaml

# terminal 2: frontend dev server
cd frontend
npm run dev
```

Default account: `admin` / `admin123`. Change the JWT secret, passwords, API keys, and DSNs before using outside local development.

Optional services (PostgreSQL, Redis, Milvus, embedding API) are configured in the YAML config — the project runs without them for local development.

### MCP retrieval server (optional)

`mcp/` is a standalone Go module exposing read-only search over a configured subset of Milvus
collections as an MCP tool, for use by AI agents. It requires a reachable Milvus + embedding
endpoint (no Noop fallback) and is independent of the main backend's auth/database:

```bash
cd mcp
go run . --config examples/mcp.yaml  # listens on :3062
```

See [Architecture · MCP 检索服务](docs/Architecture.md#mcp-检索服务) for config reference and design.

## Documentation

Detailed documentation lives under [docs/](docs):

- [Architecture](docs/Architecture.md)
- [Design decisions](docs/Design.md)
- [Workflow](docs/workflow.md)
- [Backend](docs/backend.md)
- [API](docs/api.md)
- [Data model](docs/data-model.md)
- [Frontend](docs/frontend.md) / [Frontend UX](docs/ux.md)

Development commands and conventions are in [CLAUDE.md](CLAUDE.md).
