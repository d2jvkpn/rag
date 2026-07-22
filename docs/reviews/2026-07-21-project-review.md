# 项目文档与代码评审（2026-07-21）

## 结论

项目整体结构清晰：Go 后端/Gin + GORM/Postgres + Asynq/Redis + Milvus，MCP 服务复用 `backend/pkg/rag`，前端为 Vue 3 + Vite + Naive UI。文档体系较完整，核心设计文档与代码同步程度较好。

当前最需要处理的是：

1. Markdown 上传解析链路不一致，导致后端测试在 HEAD 失败。
2. 镜像构建配置已过期：`deploy/Containerfile` 仍按 `backend/cmd/server` 构建，且版本注入包路径错误。
3. `/static/*filepath` 未纳入认证，私有文档抽取出的图片可被未授权访问。
4. 文档中存在少量陈旧/跨项目残留内容，会误导新贡献者。

本次评审首次产出时只产出报告，未修改业务代码；2026-07-22 完成一轮修复，详见下方各表格的「状态」列与
[修复记录](#修复记录2026-07-22)。

## 验证

在仓库根目录/模块目录执行：

```bash
cd backend && go test ./...
cd mcp && go test ./...
```

结果：

- `backend`：失败 3 个测试：
  - `internal/api.TestDocumentLifecycle`
  - `internal/api.TestDeleteDocumentPermissionOverridesOwnership`
  - `internal/service.TestCreateDocumentWithoutHumanReviewAutoApprovesAndIndexes`
- `mcp`：通过。

失败根因见 P0-1。

## 代码评审发现

### P0 / 高优先级

| ID | 问题 | 证据 | 影响 | 建议 | 状态 |
|---|---|---|---|---|---|
| P0-1 | Markdown 文件类型在 service 与 parser 之间命名不一致 | `backend/internal/service/document_service.go:1050-1063` 返回 `md`；`backend/pkg/rag/parser/parser.go:45-57` 只接受 `markdown` | `.md/.markdown` 上传会在 parse 阶段失败；后端测试套件当前红灯 | 统一类型枚举，建议 service 返回 `markdown` 或 parser 兼容 `md`；补充 file-type → parser-type 回归测试 | ✅ 已修复 |
| P0-2 | 容器镜像构建路径已过期 | `deploy/Containerfile:48,53-64` 使用 `backend/cmd` 和 `./cmd/server`；实际入口为 `backend/main.go:1-23`，且 `backend/cmd/**` 不存在 | `make build_image` 不能产出可运行镜像 | 改为复制 `backend/main.go`、`backend/internal`、`backend/pkg`，构建 `./`；在 CI 增加镜像构建冒烟 | ✅ 已修复 |
| P0-3 | 版本注入包路径错误 | 根 `Makefile:9,36-46` 与 `deploy/Containerfile:48` 使用 `backend/internal/infra`；实际版本变量在 `backend/pkg/infra/version.go:3-7` | 镜像内 `/version` 可能一直显示 `unknown` | 将 `version_pkg` 改为 `github.com/d2jvkpn/rag/backend/pkg/infra` | ✅ 已修复 |
| P0-4 | 静态媒体文件无认证暴露 | `backend/internal/api/handler.go:93-98` 中 `/static` 在 `withAuth()` 分组外 | 知道 URL 即可访问私有文档抽取图片；仅靠 UUID 不可作为授权机制 | 将静态资源纳入认证，或改为短时签名 URL/后端代理读取 | ⬜ 未处理 |
| P0-5 | 删除文档与索引任务存在竞态 | `backend/internal/service/document_service.go:402-425` 删除无状态守卫；异步索引仍可能回写 Milvus/文档状态 | Postgres 下删除后可能被后续 `Save` 复活；Milvus 可能残留向量 | 删除前校验状态并取消/标记进行中任务；存储层更新避免复活 tombstone；删除后做向量清理幂等化 | ✅ 已修复 |
| P0-6 | PDF 解析脚本路径依赖源码布局 | `backend/pkg/rag/parser/pdf.go:138-154` 用 `runtime.Caller` 推导 `../../scripts/parse_pdf.py`；镜像使用 `-trimpath` 且未设置 `PDF_PARSER_SCRIPT` | release/container 环境 PDF 解析大概率失败 | 优先读取显式配置/环境变量；镜像中设置 `PDF_PARSER_SCRIPT=/app/scripts/parse_pdf.py` | ✅ 已修复 |

### P1 / 中优先级

| ID | 问题 | 证据 | 建议 | 状态 |
|---|---|---|---|---|
| P1-1 | 默认安全配置偏弱 | `backend/internal/app/config.go:30-32` 默认 `jwt_secret=change-me-in-production`、`allow_origins=["*"]`；`backend/internal/api/cors.go:34-52` 允许凭证 | `--release` 下默认 secret 应直接拒绝启动；默认 CORS 应改为显式域名 | ⬜ 未处理 |
| P1-2 | 登录缺少限流/锁定 | `backend/internal/api/handler.go:96` 暴露 `/api/login`；未见限流 | 增加 IP/账号维度限流、失败锁定或验证码策略 | ⬜ 未处理 |
| P1-3 | JSON store 落盘非原子且权限过宽 | `backend/internal/repository/json_store.go:650-660` 直接 `os.WriteFile(..., 0644)` | 改为临时文件 + rename，权限 `0600`；状态文件包含密码哈希/TOTP secret，需按敏感文件处理 | ✅ 已修复 |
| P1-4 | 进程内队列可能阻塞或 panic | `backend/internal/queue/goroutine.go:22,35-42` 缓冲 32；`Enqueue` 无关闭状态判断 | 上传突发时可能阻塞 HTTP handler；shutdown 后 enqueue 会 panic。增加 context/关闭状态/错误返回 | ⬜ 未处理 |
| P1-5 | Embedding 结果缺少完整性校验 | `backend/pkg/rag/openai_embed.go:75-86` 仅按 index 填充，未校验 nil/数量/维度 | 在索引前校验 embeddings 数量、非空、维度一致；维度变化要在 KB 创建/配置变更时显式失败 | ✅ 已修复 |
| P1-6 | Milvus schema 不匹配会直接 drop collection | `backend/pkg/rag/milvus.go:535-545` | 启动期自动删库风险高；改为显式迁移命令或启动失败并提示人工确认 | ✅ 已修复 |
| P1-7 | 错误分类依赖字符串匹配 | 例如 `backend/internal/api/handler.go` 中对 `"unsupported file type"`、`"already exists"`、`"incorrect current password"` 的判断 | 引入 sentinel errors/typed errors，避免文案变更破坏 API 契约 | ✅ 已修复 |

### P2 / 低优先级

- `search_mode` 未严格校验，未知值会静默落到 dense；建议在 API 与 MCP 边界显式拒绝。
- 上传文件缺少明确大小限制，`io.ReadAll` 可能放大内存占用；建议限制大小并改流式/临时文件处理。
- 密码修改/重置后未撤销既有 token；内存 blacklist 重启后丢失；建议结合 Redis blacklist 或 token version。
- 测试缺口：`auth_service`、队列实现、Postgres store、Milvus client、CORS、前端均无有效覆盖；生命周期测试依赖 2s 轮询，存在 flaky 风险。

## 文档评审发现

### P1 / 高优先级

| ID | 问题 | 证据 | 建议 | 状态 |
|---|---|---|---|---|
| D-1 | 部署路径缺少文档且配置已漂移 | 根 `Makefile:36-50`、`deploy/Containerfile` 存在，但部署文档缺失；同时 `version_pkg` 错误 | 新增部署文档，覆盖 Containerfile、compose、镜像版本注入与运行前置条件 | ⬜ 未处理（`version_pkg` 已随 P0-3 修复，独立部署文档仍缺失） |
| D-2 | Go 风格指南包含跨项目残留 | `docs/Golang-Coding-Style.md:99-109` 提到 `pkg/llm`、`pkg/memory/mem0`、`cmd/adk` 等本仓库不存在内容 | 删除或标注为外部参考，避免贡献者按不存在包结构实现 | ⬜ 未处理 |

### P2 / 中优先级

| ID | 问题 | 证据 | 建议 | 状态 |
|---|---|---|---|---|
| D-3 | 后端 README 账号配置陈旧 | `backend/README.md:30,58-60` 写 `accounts[]`；实际为 `init_account`：`backend/internal/app/app.go:114-118`、`backend/examples/local.yaml:17-19` | 更新为 `init_account.username/password`，并说明权限如何初始化/同步 | ✅ 已修复 |
| D-4 | 前端 README 权限与配置字段陈旧 | `frontend/README.md:13,62` 写 `view_user_list`；实际 `manage_users`：`frontend/src/router/index.js:25`。`frontend/README.md:45-46` 写 `api_base/static_base`；实际 `frontend/public/app.json:2-3` 为 `api_base_url/static_base_url` | 更新权限名、配置字段，并补充 `/knowledge-bases` 路由 | ✅ 已修复 |
| D-5 | 架构/前端文档少量列表不一致 | 路由树缺少 `KnowledgeBasesPage.vue`、组件树缺少 `TotpCodeInput.vue`；`docs/Architecture.md` 写 axios，实际为 fetch | 做一次文档树与前端 `src/` 实际结构对齐 | ✅ 已修复 |

### P3 / 低优先级

- `docs/backend.md` 中的 `store_error` 未在代码中出现；错误码列表在 `docs/Architecture.md` 与 `docs/api.md` 之间不完全一致，建议以 `docs/api.md` 为唯一权威来源。
- `docs/ux.md` 中“删除 chunk”能力需要澄清：当前实际是 reject/restore，不是物理删除 chunk。
- `CLAUDE.md` 中 `docs/superpowers/` 规则在本仓库无对应目录，建议删除或改为条件适用。
- `docs/README.md` 索引缺少 `Golang-Coding-Style.md`。

## 修复记录（2026-07-22）

按用户要求修复了 P0-1、P0-2、P0-3、P0-5、P0-6、P1-3、P1-5、P1-6、P1-7、D-3、D-4、D-5，共 11 项（P0-4、P1-1、
P1-2、P1-4、D-1、D-2 及全部 P2/P3 条目本轮未处理，状态见上方各表）。

| ID | 改动 |
|---|---|
| P0-1 | `backend/pkg/rag/parser/parser.go`：`Parse()` 的 `case "markdown"` 改为 `case "md"`，与 `detectFileType()`/`file_type` 契约一致；`parser_test.go` 新增 `TestParseDispatchesFileTypeMd` 回归测试。此前红灯的 3 个测试全部转绿 |
| P0-2 / P0-3 | `deploy/Containerfile`：复制 `backend/main.go`+`internal`+`pkg` 并 `go build .`（原 `backend/cmd` 已不存在）；`version_pkg` 默认值与根 `Makefile` 均改为 `github.com/d2jvkpn/rag/backend/pkg/infra` |
| P0-5 | `internal/repository/json_store.go` 与 `postgres_store.go` 的 `UpdateDocument` 改为条件更新（不存在则返回 `ErrNotFound`），不再像 `gorm.Save()` 那样在 0 行受影响时回退为 INSERT 复活已删除文档；`document_service.go` 的 `processDocument`/`runIndex`/`failDocument` 在收到 `ErrNotFound` 时中止而非继续写 chunk / upsert 向量。新增 `TestJSONStoreUpdateDocumentAfterDeleteReturnsNotFound`、`TestRunIndexDoesNotReviveDeletedDocumentOrUpsertVectors` |
| P0-6 | `deploy/Containerfile` 新增 `ENV PDF_PARSER_SCRIPT=/app/scripts/parse_pdf.py` |
| P1-3 | `json_store.go` 的 `persistLocked` 改为临时文件 + `os.Rename`（同目录，保证原子性），权限从 `0644` 改为 `0600` |
| P1-5 | `pkg/rag/openai_embed.go` 新增 `validateEmbeddings`：校验返回向量数量、非空、维度一致，失败时在 `Embed`/`EmbedWithUsage` 直接报错；新增 3 个单元测试 |
| P1-6 | `pkg/rag/milvus.go` 的 `ensureCollection` 不再在 schema 不匹配时自动 drop+recreate，改为直接返回错误，要求运维手动 drop 后从 chunk 快照重新 upsert；`docs/Architecture.md` 同步更新该行为描述 |
| P1-7 | 新增 sentinel errors：`service.ErrUnsupportedFileType`、`service.ErrIncorrectPassword`、`repository.ErrDocumentExists`；`internal/api/handler.go` 改为 `errors.Is` 判断，不再匹配错误文案子串；新增 3 个 handler 回归测试锁定响应契约（415/409/400） |
| D-3 | `backend/README.md`：`accounts[]` → `init_account.username/password`，说明仅在 `users` 表为空时生效一次、自动拥有全部权限 |
| D-4 | `frontend/README.md`：`view_user_list` → `manage_users`；`api_base/static_base` → `api_base_url/static_base_url`；补充 `/knowledge-bases` 路由 |
| D-5 | `docs/Architecture.md`、`docs/frontend.md`：补充 `KnowledgeBasesPage.vue`、`TotpCodeInput.vue`；`http.js` 由「axios 实例」改为「fetch 客户端封装」 |

验证：`cd backend && go build ./... && go vet ./... && go test ./...`（78 个测试全部通过，13 个包）；
`cd mcp && go build ./... && go test ./...`（12 个测试全部通过，3 个包）；`gofmt -l` 无输出。

## 建议处理顺序

1. ✅ 修复 Markdown 类型映射并补回归测试，先让 `backend go test ./...` 变绿。
2. ✅ 修复镜像构建与版本注入：Containerfile、根 Makefile 已更新；独立部署文档（D-1）仍缺失。
3. ⬜ 处理安全暴露面：`/static` 认证、release 默认 secret、CORS 默认值、登录限流（本轮未处理）。
4. ✅ 消除删除/索引竞态（P0-5）与 JSON store 原子落盘问题（P1-3）。
5. 🟡 统一错误码判断改为 sentinel errors（P1-7 已修复）；清理陈旧 README（D-3/D-4/D-5 已修复），但跨项目残留（D-2）与错误码权威文档（P3）仍未处理。
6. ⬜ 补测试：auth、queue、Postgres store、Milvus 边界、前端关键路由/权限（本轮仅为已修复项补充针对性回归测试，未覆盖原有空白）。

## 可保留的优点

- 后端分层清晰，`api → service → repository/pkg` 边界明确，接口小且可替换。
- Noop embedder/vector store、JSON store、goroutine queue 让本地开发可以零外部依赖启动。
- 安全基础较完整：bcrypt、TOTP、JWT alg allow-list、MCP API key 常量时间比较、Milvus 查询使用模板参数。
- API 响应 envelope、字段级校验、分页 clamp、ownership/permission 中间件设计较一致。
- 核心设计文档维护较好，已有文档同步规则值得继续执行。
