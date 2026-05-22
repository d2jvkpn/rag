# Repository Guidelines

## Project Structure & Module Organization

This repository is currently design-first. Use `AGENTS.md` for repository guidance and [docs/README.md](/home/appuser/workspace/rag.git/docs/README.md:1) as the documentation entry point. Main directories:

- `docs/`: planning documents for ingestion flow, architecture, API, and data model
- `backend/`: future Go service code, plus backend-local `configs/`, `data/`, `logs/`, and `target/`
- `frontend/`: future Vue 3 + JavaScript UI code, plus frontend-local `public/` and `target/`

Current planning docs in `docs/`:

- `README.md`: documentation index and scope summary
- `workflow.md`: ingestion flow for `pdf`, `docx`, `pptx`, and `markdown`; chunk review; deletion; error handling
- `backend.md`: backend stack, storage, auth, logging, and implementation boundaries
- `api.md`: HTTP endpoints
- `data-model.md`: PostgreSQL and Milvus schema notes
- `frontend-business.md`: frontend pages, flows, and interaction rules
- `frontend.md`: frontend technical baseline and implementation conventions

When implementation starts, keep service code in `backend/` and UI code in `frontend/`. Do not assume shared root-level `configs/`, `data/`, `logs/`, or `target/` directories. The planned backend layout is documented in [docs/backend.md](/home/appuser/workspace/rag.git/docs/backend.md:1).

## Build, Test, and Development Commands

There is no runnable application yet. For now, contributors mainly edit and review Markdown.

- `ls -la`: inspect repository contents
- `sed -n '1,120p' docs/backend.md`: read docs in chunks
- `git status --short`: review local changes

Once code is added, document project-specific build and test commands here and in the relevant README.

## Coding Style & Naming Conventions

Use concise Markdown with short sections and flat bullet lists. Keep filenames lowercase with hyphens or clear domain names, such as `data-model.md` and `frontend-business.md`.

For planned implementation:

- use `gofmt`
- keep package names lowercase
- prefer explicit domain names like `document_repository.go`
- use `filename`, not `file_name`
- keep frontend code in JavaScript; do not introduce TypeScript unless the repository guidance changes

## Documentation Sync Rules

When code changes introduce or confirm design, technical, schema, configuration, API, or architecture decisions, update the relevant docs in the same task. Do not leave implemented behavior only in code when it changes repository guidance or invalidates existing design docs.

Required sync behavior:

- if backend implementation changes config loading, startup flags, auth model, storage layout, async processing, logging, or framework choices, update `docs/backend.md`
- if schema or field definitions change, update `docs/data-model.md`
- if request or response contracts, status names, error codes, or auth behavior change, update `docs/api.md`
- if implementation sequencing or scope decisions change, update files under `docs/plans/`
- if implementation differs from an existing design decision, either align the code to the docs or update the docs in the same task so they are consistent again

Documentation updates should describe both:

- the target design, when it still applies
- the current implemented state, when the repository is still in a staged or scaffold phase

## Testing Guidelines

No automated test suite exists yet. Until implementation lands:

- review docs for consistency across `workflow.md`, `backend.md`, `api.md`, `data-model.md`, `frontend-business.md`, and `frontend.md`
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
