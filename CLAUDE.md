# CLAUDE.md

This file provides guidance to AI coding assistants when working with code in this repository.

## Commands

### Backend

All commands run from `backend/`.

```bash
go build ./...                          # compile check
go test ./...                           # all tests
go test ./internal/api/...              # single package
go test -run TestDocumentLifecycle ./internal/api/  # single test
go run cmd/server/main.go               # dev server (defaults to configs/local.yaml)
go run cmd/server/main.go --addr :9000  # override port
```

### Frontend

All commands run from `frontend/`.

```bash
npm run dev    # dev server
npm run build  # production build → frontend/target/dist/
```

## Doc formatting

Prose and list items in doc files wrap at 100 characters per line; continuation lines under a list
item are indented 2 spaces. Markdown table rows and shell/code blocks are exempt — they must stay
on one physical line to remain valid and copy-pasteable, even when doing so exceeds the limit.

## Docs maintenance

| File | Description |
|------|--------------|
| `docs/Architecture.md` | System architecture: request path, auth, ownership, Milvus, config reference |
| `docs/Design.md` | Design decisions and conventions: document lifecycle, Noop fallback, search modes |
| `docs/workflow.md` | Document processing flow: parsing strategy, chunking, human review |
| `docs/backend.md` | Backend implementation detail: middleware, component wiring, data model |
| `docs/api.md` | API design: endpoints and contracts |
| `docs/data-model.md` | Data model: DB tables and columns |
| `docs/ux.md` | UX design decisions |
| `docs/frontend.md` | Frontend technical design: pages, components, behavior |
| `backend/examples/local.yaml` | Example server config: all `local.yaml` fields |

@docs/Architecture.md
@docs/Design.md

- **IMPORTANT: Sync docs only when a change would make the existing docs misleading or wrong** —
  e.g. a behavior change, API contract change, config field, or architectural decision. This
  includes the files above and all docs they reference. Skip doc updates for minor implementation
  details that don't affect the documented picture. Do not add placeholder text or "TODO: document
  later" — write the actual description at the time of the change. No need to ask for confirmation
  when a sync is warranted.

- **`backend/examples/local.yaml`** must also be kept in sync: whenever a config key is added,
  removed, or renamed in `backend/configs/local.yaml`, update the example file in the same pass.

- When the user's message is exactly **`:sync docs`**, immediately sync the docs listed in the
  table above and example configs that are out of alignment with current code, regardless of
  whether a recent change was made. Do not rewrite historical records under `docs/plans/` unless
  the user explicitly asks or the plan's current status note would otherwise be misleading.

- **IMPORTANT: Whenever a doc file listed above is added or removed, update the table immediately
  and automatically.**

## Go Development

- Go version: **1.26**
- **Always** use `go doc` / `go doc -src` to look up package APIs, interfaces, and implementations.
  Only read source files directly when `go doc` genuinely cannot answer the question.
  ```bash
  go doc viper.GetString          # view doc and signature
  go doc -src viper.GetString     # view source implementation
  go doc github.com/spf13/viper   # list package exports
  go list google.golang.org/adk/... # list all packages in a third-party module
  ```

## Commits

When the user's message is exactly **`:git commit`**, create a git commit following the rules below.

The exact-match trigger is intentional and strict by design: looser phrasings like "please commit
this" or "commit and push" must **not** trigger a commit. This is a deliberate safeguard against
accidental/implicit commits, not an oversight — do not relax it to fuzzy matching.

Commit rules:
- **Never commit automatically.** Only commit when the user triggers `:git commit`.
- Write commit messages in **English**.
- Use standard prefixes: `feat:`, `fix:`, `docs:`, etc. (e.g. `feat: add pptx table extraction`,
  `fix: chunker overlap tail alignment`, `docs: update backend decisions`).
- Every AI-assisted commit must include: `Assisted-by: <agent_name>:<model_version>` (e.g.
  `Assisted-by: claude:claude-sonnet-5`).
- Do **not** add `Signed-off-by` or `Co-Authored-By` on behalf of the author.
- **Always include `docs/superpowers/` files** created during the task in the same commit (stage
  them alongside the code changes they document).
- **Gitignored config files** (e.g. `configs/`, `data/`): may be read and edited freely — they
  contain sensitive data and are intentionally excluded from version control. **Never add them to a
  commit.** Always verify with `git status` before committing and unstage any gitignored files if
  present.
