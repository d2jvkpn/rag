# Repository Guidelines

## Project Structure & Module Organization

This repository is currently design-first. Use `AGENTS.md` as the navigation entry point. Main directories:

- `docs/`: planning documents for ingestion flow, architecture, API, and data model
- `configs/`: configuration files
- `data/`: local data, including stored source files
- `logs/`: runtime logs
- `target/`: backend build outputs and frontend packaged assets
- `backend/`: future Go service code
- `frontend/`: future Vue 3 + JavaScript UI code

Current planning docs in `docs/`:

- `business.md`: ingestion flow for `pdf`, `docx`, `pptx`, and `markdown`; chunk review; deletion; error handling
- `architecture.md`: stack, system boundaries, frontend, auth
- `api.md`: HTTP endpoints
- `data-model.md`: PostgreSQL and Milvus schema notes

When implementation starts, keep service code in `backend/` and UI code in `frontend/`. The planned backend layout is documented in [docs/architecture.md](/home/appuser/workspace/rag.git/docs/architecture.md:1).

## Build, Test, and Development Commands

There is no runnable application yet. For now, contributors mainly edit and review Markdown.

- `ls -la`: inspect repository contents
- `sed -n '1,120p' docs/rag-doc-ingestion/architecture.md`: read docs in chunks
- `git status --short`: review local changes

Once code is added, document project-specific build and test commands here and in the relevant README.

## Coding Style & Naming Conventions

Use concise Markdown with short sections and flat bullet lists. Keep filenames lowercase with hyphens or clear domain names, such as `data-model.md` and `rag-doc-ingestion.md`.

For planned implementation:

- use `gofmt`
- keep package names lowercase
- prefer explicit domain names like `document_repository.go`
- use `filename`, not `file_name`
- keep frontend code in JavaScript; do not introduce TypeScript unless the repository guidance changes

## Testing Guidelines

No automated test suite exists yet. Until implementation lands:

- review docs for consistency across `business.md`, `architecture.md`, `api.md`, and `data-model.md`
- verify field names and status names match across documents
- include example payloads when changing API contracts

When Go code is added, place `_test.go` files next to the code they cover.

## Commit & Pull Request Guidelines

Never create a commit unless the user explicitly asks for one.

Write commit messages in English and use short, imperative prefixes such as:

- `feat: add openclaw proxy handler`
- `fix: handle upstream timeout response`
- `docs: update backend decisions`

Every AI-assisted commit must include:

```text
Assisted-by: <agent_name>:<model_version> [tool1] [tool2]
```

Pull requests should include:

- a short summary of what changed
- affected document paths
- rationale for schema or API changes
- updated cross-links if files were split or renamed
