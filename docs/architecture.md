# 系统架构总览

相关文档：

- [设计决策与关键约定](./design.md)
- [流程与业务规则](./workflow.md)
- [后端架构与技术方案](./backend.md)
- [前端技术方案](./frontend.md)
- [API 设计](./api.md)
- [数据模型](./data-model.md)

## 请求链路

```
gin router
  → withAuth() middleware（JWT HttpOnly Cookie）
  → handler（internal/api/）
  → DocumentService / AuthService（internal/service/）
  → Store（internal/repository/）
```

所有需要登录的接口都经过 `withAuth()`，认证通过后将当前用户写入 gin context，handler 通过 `c.MustGet("current_user")` 取用。

## 持久化层

`Store` 接口（`internal/repository/interface.go`）抽象所有关系数据库访问，两个实现共享同一接口：

- **JSONStore**：本地 JSON 文件，`database.dsn` 未配置时使用
- **PostgresStore**：gorm + lib/pq，`database.dsn` 配置后使用

`main.go` 通过 `initStore(cfg)` 根据配置自动选择。新增持久化方法必须同时写入接口定义和两个实现。

## 文档生命周期

```
uploaded
  → processing/parse
  → processing/chunk
  → review_pending          ← human_review=true 时停在此处等待审核
  → (approve)
  → approved
  → processing/embed
  → processing/index
  → indexed
```

关键规则：

- `human_review=false`：切分完成后自动 approve，跳过 `review_pending`，直接进入 embedding
- `approve` 自动触发 indexing，没有独立的手动 index 步骤
- `indexed` 后文档不可再修改，rechunk 请求被阻止
- `failed` 文档可通过 `POST /api/documents/:id/index` 重试，无需重新上传

## 异步处理与任务队列

`TaskQueue` 接口（`internal/queue/`）有两个实现：

- **GoroutineQueue**：进程内 buffered channel，单 worker goroutine，默认实现
- **AsynqQueue**：hibiken/asynq，Redis 支持，`redis.dsn` 配置后自动切换

任务流程：

1. `CreateDocument`：校验 KB ID → 保存原始文件 → 创建 DB 记录（`status=uploaded`）→ `TaskQueue.Enqueue`
2. `processDocument()`：parse → CleanText → BuildChunks → ReplaceChunks → `status=review_pending`
3. `RechunkDocument`：重入队（`rechunk=true`），`chunk_version` 递增；`status=indexed` 时被阻止

## Chunk 审核操作

仅在 `human_review=true` 时有效，操作列表：

| 操作 | 效果 |
|---|---|
| `reject` | `IsCurrent=false, status=rejected`；不改变文档状态 |
| `restore` | 已 rejected 的 chunk → `draft, IsCurrent=true` |
| `edit` | 更新文本内容，`source=manual` |
| `merge` | 后端强制校验 `chunk_index` 连续性，合并为一个 chunk |
| `approve` | 全部 `draft` chunk → `approved`，自动触发 embedding + indexing |

## Chunk 快照

快照在 embedding 成功后、Milvus upsert 前落盘，路径：

```
data/documents/{yyyy}/{mm}/{dd}/{yyyy-mm-dd}_{document_id}/chunks-vN.json
```

设计要点：

- 旧版本永不覆盖；rechunk 在重走到 `indexed` 时生成 `chunks-v{N+1}.json`
- 快照包含带 embedding 向量的完整 chunk 列表，以及 `chunk_version`、源文件 SHA256、切分参数 hash
- 用途：Milvus collection 因 schema 变更被重建时，从快照直接 re-upsert，不重调 embedding API
- embedding 向量仅存在于快照和 Milvus，Postgres 的 `document_chunks` 不存向量
- 写入失败仅记 Warn，不影响 `indexed` 状态

## 认证与会话

- **JWT**：HS256（`golang-jwt/jwt/v5`），存于 HttpOnly Cookie（名称由 `http.session_cookie` 配置）
- **Secure**：仅在 `--release` 模式下为 `true`，SameSite=Lax
- **TTL**：由 `http.jwt_token_ttl` 配置（默认 `8h`），cookie maxAge 与之保持一致
- **Logout**：cookie maxAge=-1 + JTI 写入 `TokenBlacklist`
  - `MemoryBlacklist`：进程级，重启后失效（默认）
  - `RedisBlacklist`：`redis.dsn` 配置后自动切换，TTL = token 剩余有效期
- **密码**：bcrypt `DefaultCost`（`golang.org/x/crypto/bcrypt`）

## TOTP 两步验证

用户可自行开启/关闭，接口：`POST /api/me/totp/{setup,enable,disable}`。

登录流程（`totp_enabled=true`）：

1. 第一次提交用户名+密码，不含 `totp_code`
2. 服务端验证凭据，返回 HTTP 200 `{"totp_required": true}`，不设置 Cookie
3. 前端展示验证码输入框，第二次提交附上 `totp_code`
4. 验证通过后正常登录并设置 Cookie

## 账户初始化与权限

- `accounts[]` 列表在启动时逐条检查：用户名不存在则插入
- `password`：明文自动 bcrypt hash；`$2a$`/`$2b$`/`$2y$` 前缀视为已 hash，直接存入
- `permissions[]`：纯配置态，不写入 `users` 表，每次从配置文件读取
- 已存在的账户不被修改；`disabled` 用户在登录和每次鉴权请求时均被拦截

支持的权限：`view_user_list` / `delete_documents` / `disable_users`

## 文档所有权

`withDocumentOwner()` 中间件应用于 rechunk / approve / merge / edit / reject / restore / index：

- `uploader_id != ""` 且与当前用户不符时返回 `403 forbidden`
- `uploader_id` 为空（存量数据）跳过所有权检查
- `DELETE /api/documents/:id`：文档上传者 **或** 持有 `delete_documents` 权限的用户均可执行
- 所有用户均可读取所有文档（GET 接口无所有权限制）

## API 响应规范

```jsonc
// 成功
{"data": <payload>}

// 错误
{"error": {"code": "...", "message": "...", "details": [{"field": "...", "reason": "..."}]}}

// 列表
{"data": {"items": [...], "page": 1, "page_size": N, "total": N}}
```

列表接口始终返回 200 + `items: []`，不返回 404。

错误码：`validation_error` / `unauthorized` / `forbidden` / `not_found` / `conflict` / `unsupported_file_type` / `processing_failed` / `internal_error`

## Milvus / 向量存储

每个知识库对应一个 Milvus collection。知识库元数据由后端 Store 持久化，并通过前端 UI 创建；`local.yaml` 中由 `embedder.dim` 配置 embedding 模型输出维度，并作为新建知识库的向量维度。

Collection schema：

- `embedding`：dense float 向量（dim 来自创建知识库时的服务端 `embedder.dim`）
- `sparse`：BM25 稀疏向量，由 Milvus 内置 BM25 function 从 `text` 字段自动生成
- 其余元数据字段（`chunk_id`、`document_id`、`knowledge_base_id`、`text` 等）

创建知识库时后端会同步创建或加载对应 collection。`ensureCollection` 检查 schema，发现 `sparse` 缺失或 `analyzer` 不匹配时 **drop + recreate**，数据丢失，需重新入库。完整 schema 见 [数据模型](./data-model.md)。

## 前端架构

### 配置加载

`main.js` 在 `mount()` 前调用 `loadConfig()`（GET `/app.json`），失败则显示致命错误，不继续渲染。所有 service 模块在每次请求时调用 `getConfig()`，不缓存 base URL。

运行时配置字段（`frontend/public/app.json`）：

| 字段 | 说明 |
|---|---|
| `api_base` | 后端 API 基础地址 |
| `static_base` | 后端静态资源基础地址 |
| `poll_interval_ms` | 文档详情页轮询间隔，默认 3000 |

Loader 将 snake_case 字段规范化为 camelCase，兼容旧版 camelCase 配置。

### 状态轮询

`DocumentDetailPage` 在 `status` 不属于 `['indexed', 'failed', 'review_pending']` 时持续轮询 `GET /api/documents/:id`，间隔为 `pollIntervalMs`。组件 `onUnmounted` 时清除定时器。

### 状态映射

`utils/status.js` 是文档状态的唯一来源：

- `STATUS_LABEL`：状态 → 显示文案
- `STATUS_TYPE`：状态 → Naive UI tag type（success / warning / error 等）
- `isTerminal(status)`：是否为终态

组件中直接导入消费，不在页面或列表列定义中重复写映射逻辑。

## 配置参考

默认配置路径为 `backend/configs/local.yaml`；仓库提供示例配置 `backend/examples/local.yaml`，通过 `--config` 指定后由 viper 加载。

| 键 | 默认值 | 说明 |
|---|---|---|
| `http.addr` | `:3061` | 监听地址，可被 `--addr` 覆盖 |
| `http.base_path` | `""` | 所有路由的 URL 前缀，如 `"/rag"` |
| `http.jwt_secret` | — | JWT 签名密钥（必填） |
| `http.jwt_token_ttl` | `8h` | Token 有效期，支持 `time.ParseDuration` 格式 |
| `http.session_cookie` | `rag_session` | HttpOnly Cookie 名称 |
| `http.allow_origins` | `["*"]` | CORS 允许的 Origin 列表，不能为空 |
| `app.data_dir` | `data` | 数据根目录（文档文件、快照、静态资源） |
| `app.state_path` | — | JSONStore 文件路径，默认 `{data_dir}/app-state.json` |
| `database.dsn` | — | PostgreSQL DSN，配置后切换为 PostgresStore |
| `redis.dsn` | — | Redis DSN，配置后切换为 AsynqQueue 和 RedisBlacklist |
| `accounts[].username` | — | 启动时自动创建的账户用户名 |
| `accounts[].password` | — | 明文或 bcrypt hash（`$2a$`/`$2b$`/`$2y$` 前缀） |
| `accounts[].permissions[]` | — | 配置态权限，不入库 |
| `embedder.base_url` | — | OpenAI-compatible Embedding 端点 |
| `embedder.api_key` | — | Embedding API Key |
| `embedder.model` | `text-embedding-3-small` | Embedding 模型名 |
| `embedder.batch_size` | `10` | 每次请求的最大 input 条数 |
| `milvus.addr` | — | Milvus gRPC 地址 |
| `milvus.db` | — | Milvus 数据库名 |
| `embedder.dim` | — | 必填。Embedding 模型输出维度，UI 新建知识库时写入 collection schema |
| `llm.base_url` | — | OpenAI-compatible LLM 端点 |
| `llm.api_key` | — | LLM API Key |
| `llm.model` | `gpt-4o-mini` | LLM 模型名 |

未配置 `database.dsn` 时使用 JSONStore；未配置 `redis.dsn` 时使用 GoroutineQueue 和 MemoryBlacklist；`embedder.dim` 必填；未配置 `embedder.base_url` / `embedder.api_key`、`milvus.addr`、`llm.*` 时对应外部组件回落 Noop 实现。

后端运行目录存在 `{app.data_dir}/ui/index.html` 或 `target/ui/index.html` 时自动托管前端 SPA：`/ui` 为前端入口，`/` 和 `/index.html` 重定向到 `/ui/index.html`；`/api`、`/healthz`、`/static` 保持后端路由，其中 `/static` 服务 `{app.data_dir}/static`。
