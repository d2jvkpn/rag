# 系统架构总览

相关文档：

- [设计决策与关键约定](./Design.md)
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

`internal/repository/interface.go` 将持久化访问拆分为四个小接口，再组合为 `Store`：

| 接口 | 方法数 | 消费方 |
|---|---|---|
| `UserStore` | 4 | `AuthService` |
| `KnowledgeBaseStore` | 5 | `DocumentService` |
| `DocumentStore` | 7 | `DocumentService` |
| `ChunkStore` | 6 | `DocumentService` |
| `Store`（组合以上四个） | — | `app/`，两个实现 |

`AuthService.store` 类型为 `UserStore`；`DocumentService.store` 为 service 包内的 unexported
`docStore`（嵌入后三个接口），不依赖 `UserStore`。两个实现均满足组合接口 `Store`，传入时自动满足所有子接口。

- **JSONStore**：本地 JSON 文件，`database.dsn` 未配置时使用
- **PostgresStore**：gorm + lib/pq，`database.dsn` 配置后使用

`main.go` 通过 `initStore(cfg)` 根据配置自动选择。新增持久化方法必须同时写入对应子接口定义和两个实现。

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

验证码输入框（`TotpCodeInput`）在补全第 6 位（手动输入或粘贴）时触发 `complete` 事件，登录、开启、关闭 TOTP 三处均监听该事件并自动提交，无需额外点击确认按钮。

## 账户初始化与权限

- `init_account` 为单个初始 admin 账户，**仅在 `users` 表为空时生效**（一次性引导）
- `password`：明文自动 bcrypt hash；`$2a$`/`$2b$`/`$2y$` 前缀视为已 hash，直接存入
- 该账户自动拥有全部权限（admin），无需显式列出
- 未配置 `init_account.username` 或 `init_account.password`，或表中已有用户时，跳过且不报错
- 系统已有用户后，`init_account` 配置不再产生任何副作用

支持的权限：`manage_users` / `manage_knowledge_bases` / `manage_documents`

**权限判断逻辑**（`AuthService.HasPermission`）采用零权限默认模型：

- `users.permissions` 为 `NULL` 或空数组 → 无任何权限
- `users.permissions` 非空 → 仅持有列表中明确列出的权限

`init_account` 创建的账户在 DB 中显式写入全部 3 项权限（不依赖 NULL 推断）。通过 `POST /api/users` 创建的用户初始权限由创建时指定的列表决定。权限列表由
`repository.AllPermissions` 统一维护，是 service 层校验与 store 层初始化的唯一来源。

## 文档所有权

`withDocumentOwner()` 中间件应用于 rechunk / approve / merge / edit / reject / restore / index：

- `uploader_id != ""` 且与当前用户不符时返回 `403 forbidden`
- `uploader_id` 为空（存量数据）跳过所有权检查
- `DELETE /api/documents/:id`：文档上传者 **或** 持有 `manage_documents` 权限的用户均可执行
- 所有用户均可读取所有文档（GET 接口无所有权限制）

## API 响应规范

业务 API 使用以下统一结构；`/healthz`、`/version`、静态文件和 SPA 响应不包裹 `data`。

```jsonc
// 成功
{"data": <payload>}

// 错误
{"error": {"code": "...", "message": "...", "details": [{"field": "...", "reason": "..."}]}}

// 列表
{"data": {"items": [...], "page": 1, "page_size": N, "total": N}}
```

列表接口始终返回 200 + `items: []`，不返回 404。

错误码：`validation_error` / `unauthorized` / `forbidden` / `not_found` / `route_not_found` /
`conflict` / `unsupported_file_type` / `internal_error`

## Milvus / 向量存储

每个知识库对应一个 Milvus collection。知识库元数据由后端 Store 持久化，并通过前端 UI 创建；`local.yaml` 中由 `embedder.dim` 配置
embedding 模型输出维度，并作为新建知识库的向量维度。

Collection schema：

- `embedding`：dense float 向量（dim 来自创建知识库时的服务端 `embedder.dim`），HNSW 索引使用 `COSINE` metric，dense search
  显式传 `metric_type=COSINE`
- `sparse`：BM25 稀疏向量，由 Milvus 内置 BM25 function 从 `text` 字段自动生成
- `tags`：文档标签数组（Array\<VarChar(256)\>，max\_capacity=20）
- `file_sha256`：文档原文 SHA-256（VarChar(64)）
- 其余元数据字段（`chunk_id`、`document_id`、`knowledge_base_id`、`text` 等）

创建知识库时后端会同步创建或加载对应 collection。`ensureCollection` 检查 schema，发现 `sparse`、`tags`、`file_sha256` 缺失或
`analyzer` 不匹配时**直接返回错误**，不会自动 drop + recreate（避免静默丢失该 collection 的全部向量）；创建知识库或应用启动时
hydrate 已有知识库都会因此失败并要求人工介入——运维需手动确认后 drop 该 collection，再从对应文档的 [chunk 快照](#chunk-快照)
（`chunks-vN.json`）重新 upsert，而不需要重调 embedding API。dense index metric 从旧版本 `L2` 改为
`COSINE` 后，既有 collection 同样需要人工重建索引或重建 collection 后重新入库。完整 schema 见 [数据模型](./data-model.md)。

## 后端模块结构

```
backend/
├── main.go              # 启动入口，flag 解析，调用 app.Run()
├── configs/             # 本地运行配置（local.yaml，不入库）
├── examples/            # 示例配置文件
├── scripts/             # 辅助脚本（如 PDF 解析 Python 脚本）
├── tests/               # 集成测试
├── pkg/
│   ├── rag/             # RAG 核心组件（导出包，供 mcp/ 模块复用，见下）
│   │   ├── parser/      # 文件解析器（PDF / DOCX / PPTX / Markdown / 媒体）
│   │   ├── embedder.go          # Embedder 接口
│   │   ├── openai_embed.go      # OpenAI-compatible Embedding 实现
│   │   ├── vectorstore.go       # VectorStore 接口
│   │   ├── milvus.go            # Milvus VectorStore 实现
│   │   ├── query.go             # 共享的 Query 辅助函数（topK 截断、search_mode 默认值、
│   │   │                        # embed-unless-bm25 调度），供 DocumentService.Query 和
│   │   │                        # mcp 的 search 工具共用
│   │   └── llm.go               # LLM 接口（预留）
│   └── infra/           # 日志、版本信息与 Gin SPA 托管（导出包，可供外部模块复用）
│       ├── logger.go
│       ├── spa.go
│       └── version.go
└── internal/
    ├── app/             # 应用初始化与依赖装配（wiring）
    │   ├── app.go       # 组装 Store/Queue/Service/Router，启动 HTTP Server
    │   └── config.go    # viper 配置结构体
    ├── api/             # HTTP handler 层
    │   ├── handler.go   # 所有路由注册与 handler 实现
    │   ├── response.go  # 统一响应工具函数
    │   └── cors.go      # CORS 中间件
    ├── service/         # 业务逻辑层
    │   ├── document_service.go  # 文档上传、处理、审核、索引全流程
    │   ├── auth_service.go      # 登录、JWT、TOTP
    │   ├── chunker.go           # 文本切分逻辑
    │   └── blacklist.go         # JWT 黑名单（Memory / Redis）
    ├── repository/      # 持久化层
    │   ├── interface.go         # UserStore / KnowledgeBaseStore / DocumentStore / ChunkStore / Store
    │   ├── json_store.go        # JSONStore 实现
    │   └── postgres_store.go    # PostgresStore 实现（gorm + lib/pq）
    ├── queue/           # 异步任务队列
    │   ├── queue.go             # TaskQueue 接口
    │   ├── goroutine.go         # GoroutineQueue（进程内 channel）
    │   └── asynq.go             # AsynqQueue（Redis + hibiken/asynq）
    ├── model/           # 数据模型定义（DB + 业务结构体）
    │   └── models.go
    ├── infra/           # backend 内部基础设施（logger.go 是 pkg/infra 的薄代理，见下）
    │   ├── logger.go            # 转发到 pkg/infra.Init/Sync/EnableFileLogging，保持 infra.L 包级变量不变
    │   └── middleware.go        # backend 请求日志中间件
    └── migrations/      # PostgreSQL 数据库迁移文件
```

`pkg/rag` 从 `internal/rag` 迁出，是 Go 语言层面允许跨模块导入的前提（`internal/`
包只能被同一模块树内的代码导入）——`mcp/` 是独立 Go module，
无法导入 `backend/internal/...`。`pkg/infra` 同理：日志初始化、构建版本变量和通用 Gin SPA 静态托管均位于
`pkg/infra`，可被外部模块直接导入。日志初始化（`Init`/`Sync`/`EnableFileLogging`/`L`）保留
`internal/infra/logger.go` 薄代理，backend 内部现有的 `infra.L`/`infra.Init(...)` 调用点无需改动；
版本变量（`GitBranch`/`GitCommit`/`CommitTime`）和 `GinSPA` 没有保留代理——`backend/main.go`、
`internal/api/handler.go` 按需以 `pkginfra` 别名导入 `backend/pkg/infra` 并直接使用。`mcp/` 同样直接导入
`backend/pkg/infra`，不再维护自己的日志实现。`middleware.go` 是 backend 专用请求日志中间件，仍留在
`internal/infra`。

`GitBranch`/`GitCommit`/`CommitTime` 通过 `-ldflags -X` 在构建时注入（见 [后端架构与技术方案](./backend.md)）；
`backend/Makefile` 的 `version_pkg` 指向 `github.com/d2jvkpn/rag/backend/pkg/infra`
（`-X` 只能对变量的真正声明处生效）。
`mcp/Makefile` 的 `build`/`run` 目标同样以相同的 `version_pkg` 注入这三个变量——两个模块各自独立编译，`-X` 分别写入各自二进制里那份
`pkg/infra` 的数据，互不影响。

## MCP 检索服务

`mcp/` 是仓库根目录下独立的 Go module（`github.com/d2jvkpn/rag/mcp`），提供一个只读的 Model Context
Protocol（MCP）服务，让 AI agent 通过 MCP 工具调用对**预先配置的一部分 Milvus collection** 做语义 /
BM25 / hybrid 检索。

设计要点：

- **独立进程，不依赖主后端**：直接复用 `backend/pkg/rag` 的 `Embedder` / `Milvus VectorStore`
  构造函数（`rag.NewOpenAIEmbedder`、`rag.NewMilvus`），不经过 `DocumentService` / `Store` / 登录鉴权，
  因此不依赖 Postgres、Redis、JWT 等主后端基础设施。
- **代码复用机制**：仓库根目录的 `go.work`（`use ./backend ./mcp`）让 `mcp` 模块在本地构建时直接使用
  `backend` 模块的源码；`mcp/go.mod` 中的 `replace github.com/d2jvkpn/rag/backend => ../backend`
  保证脱离 workspace（例如 CI 中单独 `go build`）时也能解析到同一份本地代码。
- **传输**：仅支持 Streamable HTTP（`github.com/modelcontextprotocol/go-sdk/mcp`），独立监听端口
  （默认 `:3062`，区别于主后端的 `:3061`）。
- **collections 白名单**：`mcp.yaml` 的 `milvus.collections` 显式列出该服务允许检索的 collection
  （name + description）；请求中 `collection` 不在该列表时直接拒绝，即使该 collection 在 Milvus 中真实存在。
- **单一工具 `search`**：输入 `collection`（enum 为配置中的 collection 名）、`query`、`top_k`
  （默认 5，最大 50）、`search_mode`（`dense`/`bm25`/`hybrid`，默认 `dense`）、`document_ids`
  （可选过滤）。校验与调度逻辑通过 `pkg/rag.Query`（`query.go`）与 `DocumentService.Query`
  （`docs/backend.md`）共用同一份实现：dense/hybrid 模式先调用 Embedder 取 query 向量，bm25 跳过；
  再调用 `VectorStore.Search`。`collection` 的可选值及描述直接体现在工具的 JSON Schema
  （`enum` + `description`），供 agent 发现，无需额外的 list-collections 工具。启动时（`New()`）对
  `milvus.collections` 中每个配置的 collection 调用一次 `ValidateKnowledgeBase`，配置有误（拼写错误、
  Milvus 中不存在）时启动即失败，而不是等到第一次搜索请求才暴露；`LoadConfig` 同时校验
  `milvus.collections` 内没有重复的 `name`。
- **鉴权**：可选的共享密钥（`auth.api_key`），复用 `go-sdk` 自带的
  `github.com/modelcontextprotocol/go-sdk/auth` 包（`auth.RequireBearerToken` 中间件）包裹
  `StreamableHTTPHandler`。配置后请求须携带 `Authorization: Bearer <api_key>`，`token` 用
  `crypto/subtle.ConstantTimeCompare` 做常量时间比较；未配置时不鉴权，启动时打印一条 Warn 日志。没有
  用户身份或多租户概念，仅用于限制谁能调用这个只读检索接口。
- **日志**：直接导入 `backend/pkg/infra`（zap + lumberjack），不维护独立的一份拷贝。`main.go` 中
  `infra.Init(config)` 只构建控制台 core（`New()` 内部的 auth 未配置 Warn 等初始化期日志因此只打印到终端），
  紧邻 "mcp server starting" 日志前调用 `infra.EnableFileLogging()` 升级为控制台+文件双写，此后（含每次
  `search` 请求日志）同时写入两处。`--release` flag
  控制日志级别（未开启为 debug，开启后为 info），未配置 `logging.path` 时 lumberjack 使用默认文件名。
  每次 `search` 工具调用在 `handleSearch` 返回前记一条日志（成功 Info / 失败 Warn），字段包括
  `collection`、`query`、`search_mode`、`top_k`、`document_ids`（数量）、`embedding_tokens`（embedder
  上报的 token 消耗，仅当 embedder 实现 `rag.EmbedderWithUsage` 时非零）、`embed_latency`、
  `milvus_latency`、`items`（返回条目数）、`latency`（总耗时），失败时追加 `error`。"mcp server starting"
  日志同样包含 `git_branch`、`git_commit`、`commit_time` 三个字段（与 backend 的 "server starting" 一致），
  由 `mcp/Makefile` 的 `-ldflags` 注入。

```
mcp/
├── go.mod                       # module github.com/d2jvkpn/rag/mcp
├── Makefile                     # check / build / run（build/run 通过 -ldflags 注入版本字段，与 backend/Makefile 同构）
├── main.go                      # 启动入口：flag、监听、优雅关闭（与 backend/main.go 同构）
├── configs/mcp.yaml             # 本地运行配置（不入库）
├── examples/mcp.yaml            # 示例配置
├── tests/                       # 手动 HTTP smoke-test 客户端（非 go test，go run 直接调用）
└── internal/
    └── mcpserver/
        ├── config.go             # viper 配置加载与校验
        └── server.go              # mcp.Server 构建、search 工具注册与实现（含请求日志）
```

### mcp.yaml 配置参考

默认配置路径为 `mcp/configs/mcp.yaml`；仓库提供示例配置 `mcp/examples/mcp.yaml`，通过 `--config` 指定。

| 键 | 默认值 | 说明 |
|---|---|---|
| `mcp.description` | `""` | 通过 MCP `Instructions` 暴露给客户端的服务说明 |
| `auth.api_key` | `""` | 共享密钥；配置后请求须携带 `Authorization: Bearer <api_key>`，未配置时不鉴权（启动时打印警告） |
| `embedder.base_url` | — | 必填。OpenAI-compatible Embedding 端点（无 Noop 回落） |
| `embedder.api_key` | — | 必填 |
| `embedder.model` | — | 必填 |
| `embedder.batch_size` | `10` | 每次请求的最大 input 条数 |
| `embedder.dim` | — | 必填。需与 `milvus.collections` 中各 collection 的实际向量维度一致 |
| `milvus.addr` | — | 必填。Milvus gRPC 地址 |
| `milvus.db` | — | Milvus 数据库名 |
| `milvus.api_key` | `""` | Milvus API key |
| `milvus.collections` | — | 必填，非空列表；每项 `name`（必填，对应 Milvus collection / 主后端的 knowledge_base_id）+ `description` |
| `logging.path` | — | 日志文件路径（lumberjack），为空时使用 lumberjack 默认文件名 |
| `logging.max_size_mb` | `0` | 单个日志文件的最大体积（MB），达到后触发切割 |
| `logging.max_backups` | `0` | 保留的历史日志文件数量 |
| `logging.max_age_days` | `0` | 历史日志文件的最大保留天数 |
| `logging.compress` | `false` | 是否 gzip 压缩切割后的历史日志文件 |

## 前端模块结构

```
frontend/src/
├── main.js              # 应用入口，loadConfig() 后挂载 Vue
├── App.vue              # 根组件
├── router/index.js      # 路由定义（vue-router）
├── pages/               # 页面级组件
│   ├── LoginPage.vue
│   ├── KnowledgeBasesPage.vue
│   ├── DocumentsPage.vue
│   ├── DocumentDetailPage.vue
│   ├── DocumentChunksPage.vue
│   ├── SearchPage.vue
│   └── UsersPage.vue
├── components/
│   ├── AppLayout.vue    # 全局布局（导航栏、侧边栏）
│   └── TotpCodeInput.vue # TOTP 六位验证码输入框（补全后触发 complete 事件）
├── services/            # HTTP 请求封装（对应后端各资源端点）
│   ├── http.js          # fetch 客户端封装，统一抛出 HttpError
│   ├── auth.js
│   ├── documents.js
│   ├── chunks.js
│   ├── knowledge-bases.js
│   ├── search.js
│   └── users.js
├── stores/              # Pinia 状态管理
│   ├── auth.js          # 当前用户 / 登录态
│   ├── locale.js        # 语言切换
│   └── document-filters.js  # 文档列表筛选条件
├── config/app-config.js # 运行时配置加载（GET /{CONFIG}，默认 app.json）
├── utils/
│   ├── status.js        # 文档状态 → 文案 / tag type（唯一来源）
│   └── format.js        # 通用格式化工具
└── i18n/                # 国际化文案（zh.js / en.js）
```

## 前端架构

### 配置加载

`main.js` 在 `mount()` 前调用 `loadConfig()`，失败则显示致命错误，不继续渲染。加载的文件名由构建时 `CONFIG` 环境变量决定（默认 `app.json`，
本地开发为 `app.local.json`）。所有 service 模块在每次请求时调用 `getConfig()`，不缓存 base URL。

运行时配置字段（`frontend/public/app.json` / `app.local.json`）：

| 字段 | 说明 |
|---|---|
| `api_base_url` | 后端 API 基础地址 |
| `static_base_url` | 后端静态资源基础地址 |
| `request_timeout_ms` | 请求超时，默认 15000 |
| `poll_interval_ms` | 文档详情页轮询间隔，默认 3000 |

Loader 将 snake_case 字段规范化为 camelCase。

### 状态轮询

`DocumentDetailPage` 在 `status` 不属于 `['indexed', 'failed', 'review_pending']` 时持续轮询
`GET /api/documents/:id`，间隔为 `pollIntervalMs`。组件 `onUnmounted` 时清除定时器。

### 状态映射

`utils/status.js` 是文档状态的唯一来源：

- `STATUS_LABEL`：状态 → 显示文案
- `STATUS_TYPE`：状态 → Naive UI tag type（success / warning / error 等）
- `isTerminal(status)`：是否为终态

组件中直接导入消费，不在页面或列表列定义中重复写映射逻辑。

## 配置参考

默认配置路径为 `backend/configs/local.yaml`；仓库提供示例配置 `backend/examples/local.yaml`，通过 `--config` 指定后由 viper
加载。

| 键 | 默认值 | 说明 |
|---|---|---|
| `http.base_path` | `""` | 所有路由的 URL 前缀，如 `"/rag"` |
| `http.jwt_secret` | `change-me-in-production` | JWT 签名密钥；生产环境必须覆盖默认值 |
| `http.jwt_token_ttl` | `8h` | Token 有效期，支持 `time.ParseDuration` 格式 |
| `http.session_cookie` | `rag` | HttpOnly Cookie 名称 |
| `http.allow_origins` | `["*"]` | CORS 允许的 Origin 列表，不能为空 |
| `app.data_dir` | `data` | 数据根目录（文档文件、快照、静态资源） |
| `app.state_path` | — | JSONStore 文件路径，默认 `{data_dir}/app-state.json` |
| `database.dsn` | — | PostgreSQL DSN，配置后切换为 PostgresStore |
| `redis.dsn` | — | Redis DSN，配置后切换为 AsynqQueue 和 RedisBlacklist |
| `init_account.username` | — | 初始 admin 账户用户名；留空则跳过 |
| `init_account.password` | — | 明文或 bcrypt hash（`$2a$`/`$2b$`/`$2y$` 前缀）；该账户自动拥有全部权限 |
| `embedder.base_url` | — | OpenAI-compatible Embedding 端点 |
| `embedder.api_key` | — | Embedding API Key |
| `embedder.model` | `text-embedding-v3` | Embedding 模型名 |
| `embedder.batch_size` | `10` | 每次请求的最大 input 条数 |
| `milvus.addr` | — | Milvus gRPC 地址 |
| `milvus.db` | — | Milvus 数据库名 |
| `milvus.api_key` | `""` | Milvus API key，默认空字符串，不启用认证 |
| `embedder.dim` | — | 必填。Embedding 模型输出维度，UI 新建知识库时写入 collection schema |
| `logging.path` | — | 日志文件路径（lumberjack），为空时使用 lumberjack 默认文件名 |
| `logging.max_size_mb` | `0` | 单个日志文件的最大体积（MB），达到后触发切割 |
| `logging.max_backups` | `0` | 保留的历史日志文件数量 |
| `logging.max_age_days` | `0` | 历史日志文件的最大保留天数 |
| `logging.compress` | `false` | 是否 gzip 压缩切割后的历史日志文件 |

未配置 `database.dsn` 时使用 JSONStore；未配置 `redis.dsn` 时使用 GoroutineQueue 和 MemoryBlacklist；`embedder.dim`
必填；未配置 `embedder.base_url` / `embedder.api_key`、`milvus.addr` 时对应外部组件回落 Noop 实现。真实 Embedder
返回空向量或同批次向量维度不一致时索引失败，不会继续写入 Milvus。日志输出到控制台和
`logging.path` 两处（服务启动运行后；初始化阶段仅输出到控制台，见"全局 logger"/"日志"小节）；控制台/文件日志级别由 `--release` 决定（未开启为 debug，
开启后为 info），配置文件中没有单独的日志级别开关。

后端启动时通过 `pkg/infra.ScanSPAs` 扫描 `target/spa/*/index.html`。每个目录名直接映射为 SPA
路由，例如 `target/spa/ui`、`target/spa/h5`、`target/spa/web` 分别对应 `/ui/*`、`/h5/*`、
`/web/*`；真实文件未命中时，由 `GinSPA` fallback 到对应应用的 `index.html`。仅当 `ui` 应用存在时，
`/` 和 `/index.html` 重定向到 `/ui/index.html`。`http.base_path` 会统一加到这些路由之前。
`/api`、`/healthz`、`/static` 保持后端路由，其中 `/static` 服务 `{app.data_dir}/static`。静态资源
缓存策略：`index.html` 和 `app.json` 返回 `Cache-Control: no-store`；`assets/` 下带 hash 的 JS/CSS
返回 `Cache-Control: public, max-age=31536000, immutable`；其余文件走默认协商缓存
（ETag/Last-Modified）。
