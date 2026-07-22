# RAG Frontend

Vue 3 + Vite frontend for the RAG document processing console.

## Scope

The first version focuses on the operational document workflow:

- login and session bootstrap
- document upload, filtering, detail view, rechunk, ingest, and deletion
- chunk review and chunk-level actions
- semantic search page
- knowledge base management page
- user list page for accounts with `manage_users`

The UI is an internal operations console. It prioritizes clear document state, visible processing errors, and predictable lifecycle actions over complex editing or analytics.

## Stack

- Vue 3
- Vite
- JavaScript
- Naive UI
- Pinia
- vue-router
- dayjs

## Commands

Run commands from `frontend/`:

```bash
npm run dev
npm run build
npm run preview
```

Production build output is written to `frontend/target/dist/`.

## Runtime Config

The frontend does not use `.env` files for runtime behavior. It loads public runtime config from `frontend/public/app.json` at startup, resolved under Vite `BASE_PATH`/`BASE_URL` so `BASE_PATH="/ui/"` is the canonical value; `BASE_PATH="/ui"` is normalized to the same value, and both request `/ui/app.json`.

Current fields:

- `api_base_url`: backend API base URL
- `static_base_url`: backend static asset base URL
- `request_timeout_ms`: shared API request timeout in milliseconds, default `15000`
- `poll_interval_ms`: polling interval for processing document detail pages

Do not put secrets in `app.json`; it is served as a public static file.

## Routes

- `/login`
- `/documents`
- `/documents/:documentId`
- `/documents/:documentId/chunks`
- `/search`
- `/knowledge-bases`
- `/users`
- `/` redirects to `/documents`

Protected routes require the cookie-backed login session. `/users` additionally requires the `manage_users` permission returned by `GET /api/me`.

## Project Structure

- `src/pages`: route-level page containers
- `src/components`: layout and shared UI components
- `src/stores`: Pinia stores
- `src/services`: API clients by domain
- `src/utils`: formatting and status helpers
- `src/i18n`: locale messages
- `src/styles`: global styles and tokens

## Notes

- API requests are centralized under `src/services` and use the shared fetch client in `src/services/http.js`.
- Authentication uses JWT in an HttpOnly cookie; the frontend does not store tokens.
- Document detail pages poll while documents are processing and stop at terminal states such as `indexed`, `failed`, and `review_pending`.
- Uploads always enter the human review flow and submit `human_review=true`.
