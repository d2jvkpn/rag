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
npm run dev    # dev server
npm run build  # production build → frontend/target/dist/
```

## Architecture

See `docs/architecture.md` for request path, auth, ownership, Milvus, frontend structure, and config reference. See `docs/design.md` for document lifecycle, conventions, and component wiring.

## Documentation Sync

Any change that affects system behavior, API contracts, configuration, or architectural decisions **must** be reflected in the relevant docs before the task is considered complete:

| Change type | Files to update |
|---|---|
| New / changed API endpoint | `docs/api.md` |
| Request path, auth, ownership, cross-cutting design | `docs/architecture.md` |
| Document lifecycle, design decisions, conventions | `docs/design.md` |
| Document processing flow, parsing strategy, chunking, human review | `docs/workflow.md` |
| Backend implementation detail, middleware, data model | `docs/backend.md` |
| DB table / column added or changed | `docs/data-model.md` |
| Frontend page, component behavior, UI design decision | `docs/frontend-business.md`, `docs/frontend.md` |
| Config field added / changed | `docs/architecture.md` (Config 参考表) |

Do not add placeholder text or "TODO: document later" — write the actual description at the time of the change.

See `docs/design.md` for key conventions: knowledge_base_id scoping, query filter behavior, component wiring (Noop pattern), search modes, parser/chunker interfaces, migration rules, frontend constraints, test isolation, and observability patterns.

## Tooling Checks

Before answering questions like "why does X fail to install" or "why isn't X working", verify the actual state first:

```bash
which <tool>
<tool> --version
```

Do not give troubleshooting steps before confirming whether the tool is present.

## Commits & PRs

Never create a commit unless the user explicitly asks for one.

### Message language

Always write commit messages in English.

### Preferred prefixes

- `feat: add openclaw proxy handler`
- `fix: handle upstream timeout response`
- `docs: update backend decisions`

### AI attribution

Every AI-assisted commit must include:

```
Assisted-by: <agent_name>:<model_version>
```

- Do not add `Signed-off-by` on behalf of the author.
- List only specialized analysis tools; omit `git` or editors.
