# RAG 后端架构与技术方案

相关文档：

- [总览](./README.md)
- [流程与业务规则](./workflow.md)
- [API 设计](./api.md)
- [数据模型](./data-model.md)
- [前端业务设计](./frontend-business.md)
- [前端技术方案](./frontend.md)

## 推荐架构

建议拆成 3 个逻辑层：

1. `ingest api`
2. `parser/chunker worker`
3. `embedding/index worker`

## 后端技术选型基线

- Web 框架：`gin`
- ORM：`gorm`
- 数据库迁移：`github.com/golang-migrate/migrate/v4`
- 异步任务：`Asynq`
- 参数校验：`github.com/go-playground/validator/v10`
- JWT：`github.com/golang-jwt/jwt/v5`
- 测试：`github.com/stretchr/testify`
- 关系库：`PostgreSQL`
- 向量库：`Milvus`
- PDF 解析：Python 生态工具
- DOCX 解析：Go 内直接解析
- PPTX 解析：Go 内直接解析
- Markdown 解析：Go 内直接解析
- Embedding：外部 API
- 配置管理：`yaml + viper`
- 日志：`zap + gopkg.in/natefinch/lumberjack.v2`
- 监控：任务记录 + logging

## 当前第一阶段脚手架实现

当前仓库已经落了一版可运行的后端最小骨架，但它是”第一阶段验证实现”，不是最终技术栈终态。

当前已落地实现：

- HTTP 服务使用 `gin`
- 鉴权使用 `JWT`（HS256，`github.com/golang-jwt/jwt/v5`），令牌存 `HttpOnly Cookie`
- 密码哈希使用 **bcrypt**（`golang.org/x/crypto/bcrypt`，`DefaultCost`）
- TOTP 两步验证（`github.com/pquerna/otp/totp`，RFC 6238，30 秒窗口）
- 默认配置路径为 `backend/configs/local.yaml`；仓库示例配置为 `backend/examples/local.yaml`
- 配置读取使用 `viper`，统一通过 `viper.GetString/GetXX` 获取
- 启动参数使用命令行 flag，不使用环境变量
- 存储双实现：`JSONStore`（本地 JSON 文件，`state_path` 指定）和 `PostgresStore`（`gorm` + `lib/pq`），通过 `database.dsn` 配置自动选择；文档列表的 Postgres 查询使用 `5s` context timeout，超时或查询错误通过 API 返回 `store_error`
- 异步处理支持进程内 goroutine 队列（channel 容量 32）和 Redis-backed Asynq，通过 `redis.dsn` 自动选择
- 原始文件和 chunk 快照写入同一个 `backend/data/documents/{yyyy}/{mm}/{dd}/{yyyy-mm-dd}_{document_id}/` 目录

当前启动参数：

- `--release bool`
- `--addr string`
- `--config string`（示例：`examples/local.yaml`）

当前退出行为：

- 监听 `SIGINT` / `SIGTERM`
- 先对 HTTP 服务执行 `http.Server.Shutdown`，超时 `5s`
- 再执行应用级 shutdown，等待文档任务队列和进行中的索引任务结束，超时 `10s`
- 若启用了对应后端，退出时同时关闭 Milvus、Redis blacklist、Postgres 连接

当前实现取舍（第一阶段历史记录）：

- 先验证”上传 -> 解析 -> 切分 -> 快照 -> 查询 -> 删除”闭环
- 当前已支持 `GoroutineQueue` 和 Redis-backed `AsynqQueue`；未配置 `redis.dsn` 时使用进程内队列
- 当前已支持 OpenAI-compatible embedding 和 Milvus；未配置时使用 Noop 实现
- 第一阶段 `Logout` 为 no-op；现已实现 JTI + `TokenBlacklist`（内存/Redis 双实现）
- TOTP 两步验证：用户可自行开启/关闭；登录时若 `totp_enabled=true` 且未提交 `totp_code`，返回 `{"totp_required": true}`（HTTP 200），前端切换到验证码输入步骤后再次提交

## 目录约定

仓库不使用共享的根目录 `configs/`、`data/`、`logs/`、`target/`。

后端目录约定：

- `backend/configs`
- `backend/data`
- `backend/logs`
- `backend/target`

## 数据访问与迁移约定

后端第一版采用以下约定：

- 关系库访问使用 `gorm`
- schema 变更使用 `github.com/golang-migrate/migrate/v4`
- 关系库主键统一使用 `uuidv7`
- 表结构以 migration 为准，`gorm` 模型负责读写映射，不依赖自动建表

实现要求：

- 不使用 `gorm` 的自动迁移作为正式 schema 管理方式
- 每次表结构调整都补充显式 migration 文件
- `documents`、`document_chunks`、`users` 等主表主键统一使用 `uuidv7`
- repository 层基于 `gorm` 封装 `documents`、`document_chunks`、`users` 的数据访问

当前 migration 约定补充：

- `users.user_id`、`documents.document_id`、`document_chunks.chunk_id` 使用 `UUID PRIMARY KEY DEFAULT uuidv7()`
- `created_at`、`updated_at` 在表定义中统一前置
- `documents.tags` 使用 PostgreSQL `TEXT[]`
- `document_chunks` 不保留冗余的 `knowledge_base_id`
- `document_chunks` 不保留 `text_hash`

## 请求校验约定

后端第一版统一使用 `github.com/go-playground/validator/v10` 做请求参数校验。

建议约束：

- handler 层负责绑定请求并执行结构化校验
- 校验规则尽量写在请求 DTO 上，不分散在业务代码中
- 对外返回统一的参数错误格式，避免把底层校验错误直接暴露给前端

当前第一阶段脚手架里，请求校验暂时由 handler 手工完成；后续切到正式 DTO 和 `validator/v10` 时，保持对外错误格式不变。

## 版本注入约定

后端在 `internal/infra/version.go` 声明三个包级变量，默认值为 `"unknown"`：

```go
var (
    GitBranch  = "unknown"
    GitCommit  = "unknown"
    CommitTime = "unknown"
)
```

构建时通过 `-ldflags` 注入：

```makefile
ldflags := -X '$(version_pkg).GitBranch=$(git_branch)' \
           -X '$(version_pkg).GitCommit=$(git_commit)'  \
           -X '$(version_pkg).CommitTime=$(commit_time)'
```

- `backend/Makefile` 的 `build` 和 `run` 目标均通过 `-ldflags` 注入版本字段
- 直接使用 `go run ./cmd/server`（不带 `-ldflags`）时变量保持 `"unknown"`
- `GET /version` 接口直接读取这三个变量返回，无需登录
- 服务启动日志也会输出这三个字段

## 日志约定

后端日志使用：

- `zap` 负责结构化日志输出
- `gopkg.in/natefinch/lumberjack.v2` 负责日志文件轮转

约定：

- 日志文件写入 `backend/logs/app.log`
- 同时保留控制台输出和文件输出
- worker 和 API 进程使用统一日志格式
- 不自行实现日志切片和归档逻辑，直接复用 `lumberjack`
- 全局 logger `infra.L` 默认 `zap.NewNop()`；`infra.Init()` 只在 `main.go` 中调用

### 请求日志中间件

`infra.RequestLogger`（`internal/infra/middleware.go`）在每次请求 `c.Next()` 完成后输出一条 `request` 日志，按 status 分级：

| status | level |
|---|---|
| `>= 500` | `Error` |
| `>= 400` | `Warn` |
| 其他 | `Info` |

字段：

- `ip`、`method`、`status`、`latency`：基础信息
- `path`：路由模板（`c.FullPath()`），便于聚合统计；未匹配路由时回落到 `c.Request.URL.Path`
- `params`：路径参数（如 `/api/documents/:id` → `{"id":"..."}`），非空才写
- `query`：原始 query 字符串（`c.Request.URL.RawQuery`），非空才写
- `err_origin`、`err_code`、`err_message`：仅在 handler 调用 `writeError` 后由其写入 `c.Set(...)`，定位 4xx/5xx 的代码出处

中间件派生 logger 时关闭了 caller（`zap.WithCaller(false)`），避免每条请求日志都打 `infra/middleware.go:N`。

### `writeError` 错误来源捕获

`internal/api/response.go` 的 `writeError` 用 `runtime.Caller(1)` 记录调用点（裁剪为 `internal/...` 相对路径），连同 `code`、`message` 一起塞进 `c.Set(...)`。这样请求日志可以直接看到形如：

```text
WARN  request  status=401 err_origin=internal/api/handler.go:99 err_code=unauthorized err_message="invalid credentials"
```

无需 handler 主动调用日志 API。

## 静态文件存储约定

建议将本地静态文件按用途拆分存储，不要混放：

1. 原始上传文件
2. chunk 切分结果快照
3. 派生资源文件

推荐目录结构：

```text
backend/
  data/
    documents/
      {yyyy}/
        {mm}/
          {dd}/
            {yyyy-mm-dd}_{document_id}/
              source.pdf
              chunks-v1.json
              chunks-v2.json
    static/
      {yyyy}/
        {mm}/
          {dd}/
            {yyyy-mm-dd}_{document_id}/
              pdf-page-1-image-1.png
```

目录说明：

- `backend/data/documents/...`：按上传日期分层保存原始上传文件和 chunk JSON 快照
- `backend/data/static/...`：按上传日期分层保存 PDF/DOCX/PPTX 解析出的图片等派生静态资源

设计要求：

- documents/static 使用同一日期分层和 `{yyyy-mm-dd}_{document_id}` 末级目录名
- chunk 快照与原始文件放在同一个 documents 目录下，按版本保存，不直接覆盖旧版本
- 业务静态文件不要放进 `backend/logs/` 或 `backend/target/`

chunk 快照约定：

- 快照只在 embedding 成功之后、Milvus upsert 之前落盘；未走到 `indexed` 的文档不会有快照
- 文件按版本命名，例如 `chunks-v1.json`，旧版本永不覆盖；rechunk 在重新走到 indexed 时才生成 `chunks-v{N+1}.json`
- 快照中保存 `document_id`、`knowledge_base_id`、`chunk_version`、原文哈希、切分参数，以及带向量的完整 chunks 内容
- 快照的作用是：Milvus collection 因为 schema 变更被重建时，可以直接从快照 re-upsert，不必重新调用 embedding API
- embedding 向量只存在于快照和 Milvus，Postgres 的 `document_chunks` 表不存向量
- 快照写入失败只记 Warn，不影响 `indexed` 状态

## 第一版实现边界

- 第一版支持 `pdf`、`docx`、`pptx`、`markdown`
- 第一版默认不强制人工审核，审核能力作为可选流程
- `documents` 初始状态为 `uploaded`
- 第一版不引入独立的 `document_resources` 表，图片、表格、链接引用先写入 `document_chunks.resource_refs`
- 解析器会在正文中保留图片占位符：格式为 `[Image:ref_id]` 或 `[Image:ref_id label]`，`ref_id` 与对应 `resource_refs` 条目精确绑定；若 PPTX `p:cNvPr descr` 属性值为外部 URL，该 URL 存入 `ref.url`/`ref.is_external`，不作为 label；PPTX 备注以 `Notes: ...` 附加到幻灯片文本
- `docx` / `pptx` 遇到原生表格时，会转成 Markdown 表格文本并写入 `document_chunks.text`；正文段落、标题、列表等其他元素仅提取纯文本，不转 Markdown 格式；`docx` 表格单元格中的图片同样生成 `[Image:ref_id label]` 占位符并写入 `resource_refs`
- `docx` 中检测到 `w:pStyle` heading 样式（`Heading1`–N、`1`–`6`、`标题N`）时按标题边界拆分为结构化 blocks，每个 block 带 `SectionTitle`；无标题文档退回单 block
- `docx` 中相邻且列数一致的连续表会按续表处理并合并；若后一张表首行与前一张表表头一致，会自动去掉重复表头
- `pptx` 每张幻灯片作为独立 block，`SectionTitle` = "Slide N"，`PageStart` = 幻灯片编号
- `markdown` 中原有表格语法保持原样，不做二次转换；按 `#/##/…` 标题边界拆分为 blocks
- `pdf` 通过 Python 脚本调用 `pdfplumber` 提取文本，并将识别到的页内表格转成 Markdown 表格文本；正文抽取会尽量排除表格区域，减少重复内容；会尝试合并相邻页列数一致的续页表并去掉重复表头；内容尺寸的图片、表格和 PDF 超链接会写入 `resource_refs`，PDF 图片会渲染为 PNG 并保存到 `data/static/{yyyy}/{mm}/{dd}/{yyyy-mm-dd}_{document_id}`；每页作为独立 block，`PageStart` = 页码；要求运行环境可执行 `python3` 且安装 `pdfplumber`
- 扫描版 PDF 或其他无可提取文本的 PDF 仍会直接失败，不做 OCR
- embedding 输入只使用 `document_chunks.text`
- chunk 切分完成后直接写入 `document_chunks`；快照延后到 embedding 完成时再落盘

## Chunk 策略

采用”结构优先 + 长度兜底”的混合切分策略，使用 `github.com/pkoukk/tiktoken-go` 进行 token 计数，默认编码 `cl100k_base`（可通过 `service.SetTokenEncoding(name)` 在启动时切换）。

**默认参数**（可按 collection 覆盖）：`chunk_size = 800 tokens`、`chunk_overlap = 100 tokens`、`min_chunks = 2`

**处理流程**

1. **Parser 输出结构化 blocks**：`Parse()` 返回 `[]ParseBlock{Text, SectionTitle, PageStart, PageEnd}`
   - Markdown：按 `#/##/…` 标题边界拆分，每节一个 block
   - DOCX：检测 `w:pStyle` heading 样式拆分；无标题文档退回单 block；`PageStart`/`PageEnd` 均为 0
   - PPTX：每张幻灯片一个 block，`PageStart` = `PageEnd` = 幻灯片编号
   - PDF：每页一个 block，`PageStart` = `PageEnd` = 页码
2. **CleanText**：对每个 block 的文本做清洗（折叠多余空行、去除控制字符）
3. **mergeSmallBlocks**：将相邻小 block 累积合并，直到合并后超过 `chunk_size` 才另起一组；合并后 `PageEnd` 取最后一个 block 的 `PageEnd`；DOCX/Markdown（`PageStart=0`）和 PPTX/PDF 均参与合并
4. **BuildChunks**：逐 block 调用 `splitByLength`，每个 chunk 继承所在 block 的 `SectionTitle` / `PageStart` / `PageEnd`
5. **末尾碎片合并**：所有 chunk 生成后，若最后一个 chunk 的文本长度 < `chunk_size / 2`，将其追加到倒数第二个 chunk；`PageEnd` 取两者较大值，`ResourceRefs` 合并
   - **image ref 过滤**：每个 chunk 只继承其文本中实际包含 `[Image:<ref_id>` 占位符的 image ref；link 等其他类型 ref 仍全量继承（anchor text 无法精确定位到 segment）
6. **min_chunks 合并**：全部 block 切分完成后，若总 chunk 数 ≤ `min_chunks`，将整篇合并为单一 chunk；此路径同样保留首 block 的 `PageStart` 和末 block 的 `PageEnd`

**splitByLength 细节**

- 以 `\n\n` 为段落边界累积内容，超出 `chunk_size` 时落段
- Markdown 代码块（`` ``` ``/`~~~`）内部的空行受保护，不触发段落切分
- 单段落超出 `chunk_size` 时：
  - 若为 Markdown 表格：按数据行拆分，每部分保留完整表头
  - 否则：`splitByTokens` 按 token 滑动窗口切分
- `overlapTail` 取末尾 `overlap` 个 token，再向后找第一个句末符（`。.!?！？；;`），overlap 从该符号之后开始，避免从句子中段携带上文

**chunk 内容格式**

- `Text` 和 `NormalizedText` 存同一内容（原始文本，不做规范化）
- 表格转为 Markdown table 语法，正文为纯文本，Markdown 源文件保留原始语法
- `SectionTitle`、`PageStart`、`PageEnd` 由 parser 层填充；DOCX 无页码信息（始终为 0）

## 用户系统

建议认证方式：

- `JWT + HttpOnly Cookie`
- JWT 实现库使用 `github.com/golang-jwt/jwt/v5`

当前实现：

- 使用 `JWT + HttpOnly Cookie`（HS256，`github.com/golang-jwt/jwt/v5`）
- Cookie 属性：`HttpOnly=true`、`SameSite=Lax`、`Secure` 仅在 `--release` 模式下为 `true`
- Token 有效期由 `http.jwt_token_ttl` 配置（默认 `8h`，支持 `time.ParseDuration` 格式），cookie `maxAge` 与 token TTL 保持一致
- 密码使用 bcrypt 哈希（`golang.org/x/crypto/bcrypt`，`DefaultCost`）
- 每个 token 携带 JTI（UUID）
- `Logout` 清除客户端 cookie（`maxAge=-1`）并将 JTI 写入 `TokenBlacklist`，后续请求在 `withAuth()` 中被拦截
- 未配置 `redis.dsn` 时使用 `MemoryBlacklist`（进程级，重启后失效）；配置后自动切换为 `RedisBlacklist`（TTL = token 剩余有效期）

**账户初始化：** 配置文件的 `init_account` 为单个引导账户，**仅在 `users` 表为空时执行一次**，之后配置变更不产生任何副作用。`password` 支持明文（启动时自动 bcrypt hash）或已有 bcrypt hash（以 `$2a$`/`$2b$`/`$2y$` 开头，直接存入）。该账户在 DB 中被显式赋予全部权限（`manage_users` / `manage_knowledge_bases` / `manage_documents`）。

支持的权限（`repository.AllPermissions` 为唯一来源）：

- `manage_users`：管理用户（查看、创建、启用/禁用、编辑权限、重置密码）
- `manage_knowledge_bases`：创建、删除知识库
- `manage_documents`：删除非本人上传的文档

**权限判断逻辑**（零权限默认模型）：

- `users.permissions` 为 `NULL` 或空数组 → 无任何权限
- `users.permissions` 非空 → 仅持有列表中明确列出的权限
- 权限校验由 `AuthService.HasPermission` 执行，`slices.Contains` 精确匹配

`users.status` 运行态状态：`active` / `disabled`。账号一旦变成 `disabled`，即使有权限也无法登录，已有 JWT 在后续请求中会被拦截。

```yaml
init_account:
  username: admin
  password: "changeme"   # 明文，启动时自动 hash
```

接口详见 [API 设计](./api.md)。

## 组件接口与装配约定

`DocumentService` 通过 functional options 装配外部组件，未配置时回落 Noop 实现，无需代码改动。

| 组件 | 包 | 装配函数 | 激活条件 |
|---|---|---|---|
| Embedder | `internal/rag/` | `WithEmbedder()` | `embedder.base_url` + `api_key` 均已配置 |
| VectorStore | `internal/rag/` | `WithVectorStore()` | `milvus.addr` 已配置 |
| TaskQueue | `internal/queue/` | `WithTaskQueue()` | `redis.dsn` 已配置（否则用 GoroutineQueue） |

**Embedder** 实现：Noop（默认）和 OpenAI-compatible（`embedder.base_url` 指向任意兼容端点）。`batch_size` 默认 `10`，DashScope 兼容端点不支持更大批次，不要调大。

**VectorStore** 接口方法：`ValidateKnowledgeBase`、`CreateKnowledgeBase`、`DeleteKnowledgeBase`、`Upsert`、`DeleteByDocument`、`Search(ctx, SearchRequest)`。`SearchRequest` 携带 `KnowledgeBaseID`、`Embedding`、`Query`、`TopK`、`DocumentIDs`、`Mode`（`""` dense / `"bm25"` / `"hybrid"`）、`EF`、`DropRatio`、`RRFK`。BM25 模式跳过 Embedder；dense / hybrid 模式要求 Embedder 已配置。Milvus dense index/search 使用 `COSINE` metric；dense/BM25 单路查询使用 Milvus Search；hybrid 使用 Milvus HybridSearch + RRF reranker 融合 dense 与 BM25 排名，不直接混排两路原始分数。

**TaskQueue** 实现：`GoroutineQueue`（单 worker goroutine，默认）和 `AsynqQueue`（Redis 支持）。选择逻辑封装在 `NewDocumentService` 内部。

**Config 补充字段：** `http.base_path`（所有路由的 URL 前缀，默认 `""`，例如 `"/rag"`）；`milvus.api_key`（Milvus API key，默认 `""`）。后端运行目录存在 `target/ui/index.html` 时自动启用前端 SPA 托管，路径为 `/ui`，并将 `/` 和 `/index.html` 重定向到 `/ui/index.html`；`/static` 仍只服务 `{app.data_dir}/static`。

## 推荐目录结构

```text
backend/
  configs/
  data/
  logs/
  target/
  migrations/
    sql/
  internal/
    model/
    api/
      handler/
      router/
    ingest/
      service/
    parser/
      pdf_parser.go
      docx_parser.go
    cleaner/
      text_cleaner.go
    chunker/
      chunker.go
    embedder/
      embedder.go
    vectorstore/
      milvus_store.go
    repository/
      document_repository.go
      chunk_repository.go
    worker/
      tasks/
      handlers/
    config/
    logger/
```

目录约定：

- `backend/configs/`: 后端配置文件目录
- `backend/data/`: 后端数据目录，包含文档原文件、chunk 快照、派生资源等
- `backend/logs/`: 后端日志目录
- `backend/target/`: 后端编译产物目录
- `backend/internal/migrations/sql/`: SQL migration 文件目录

当前实际目录结构：

```text
backend/
  Makefile
  cmd/server/                # 入口
  examples/local.yaml         # 示例配置
  data/                      # 文档/快照（gitignore）
  logs/                      # 运行日志（gitignore）
  internal/
    migrations/sql/         # numbered up/down SQL
    api/                     # gin handler、middleware、response
    app/                     # App 装配（store/embedder/queue/milvus 等）
    infra/                   # 全局 logger、请求日志中间件
    rag/                     # Embedder、VectorStore (Milvus)
    parser/                  # Markdown/DOCX/PPTX/PDF parser
    model/                   # 领域模型
    queue/                   # TaskQueue（GoroutineQueue / AsynqQueue）
    repository/              # UserStore/KnowledgeBaseStore/DocumentStore/ChunkStore 接口、Store 组合接口、JSONStore、PostgresStore
    service/                 # AuthService、DocumentService、blacklist、chunker
```

> 历史 README/计划里出现过的 `config/`、`uuid/` 包已并入 `app/` / 相关内部包；parser 已迁移为 `internal/rag/parser`。

## 实现顺序

1. 定义 `documents` / `document_chunks` / `users` 数据模型
2. 接入文件上传和任务状态
3. 接入登录态校验
4. 接入 Asynq 任务投递和 worker
5. 实现 Python PDF 解析 + Go DOCX 解析
6. 实现 Go PPTX / Markdown 解析
7. 实现 chunker
8. 实现 chunk 草稿查询和审核通过机制
9. 接 embedding API
10. 接 Milvus 写入与删除
11. 增加重试、幂等、日志

## 当前完成情况

当前已完成：

- `backend/` Go 模块和最小目录骨架
- `gin` 路由与鉴权中间件骨架
- `examples/local.yaml` 示例配置 + `viper` 配置加载（默认仍支持 `configs/local.yaml`）
- `--release`、`--addr`、`--config` 启动参数
- `users`、`documents`、`document_chunks` 表落地，含 `human_review`、`uploader_id/name`、`totp_secret/enabled` 等字段
- `repository` 接口拆分为 `UserStore`、`KnowledgeBaseStore`、`DocumentStore`、`ChunkStore` 四个小接口，再组合为 `Store`；`AuthService.store` 类型为 `UserStore`，`DocumentService.store` 为 service 包内 unexported `docStore`（嵌入后三个）；`JSONStore` 和 `PostgresStore` 双实现均满足组合 `Store`
- `PostgresStore`：`gorm` + `lib/pq` driver，支持 `TEXT[]` tags 和 `JSONB` resource_refs
- 启动时根据 `database.dsn` 配置自动选择 store
- 登录、退出、当前用户接口；密码使用 bcrypt 哈希
- CORS 使用 `github.com/gin-contrib/cors`，由 `http.allow_origins` 控制；必须是非空列表，`"*"` 表示允许任意 Origin
- 文档上传（记录 uploader）、列表、详情、删除接口
- 文档所有权中间件 `withDocumentOwner()`：非上传者操作变更接口返回 403
- `markdown`、`docx`、`pptx` 基础解析
- `pdf` 通过 Python `pdfplumber` 解析，并将页内表格转成 Markdown 表格文本
- chunk 切分和 chunk JSON 快照写入
- `rechunk` 接口和 chunk 版本递增
- chunk 列表查询接口按 `page/page_size` 分页返回，默认每页 50 条并返回 `total_pages` / `has_next` / `has_prev`
- chunk 审核接口：approve / reject / merge / edit / index
- 结构化日志（zap + lumberjack，同时输出控制台和 backend/logs/app.log）
- `Embedder` 接口 + Noop 实现 + OpenAI-compatible 实现
- `VectorStore` 接口 + Noop 实现 + Milvus 官方 Go SDK v2 实现（gRPC，Milvus 2.5+）
  - 支持 dense / BM25 / hybrid 三种搜索模式
  - dense 使用 Milvus HNSW `COSINE` metric，search params 显式传 `metric_type=COSINE`
  - hybrid 使用 RRF reranker 融合两路排名，不直接混排原始分数
  - 支持 HNSW ef、BM25 drop_ratio、RRF k 调参
  - 支持按 document_ids 过滤
  - 每个 collection 可独立配置 analyzer（默认 `chinese`），启动时自动检测 schema 并按需重建
- `TaskQueue` 接口 + GoroutineQueue 实现（默认，无额外依赖）+ AsynqQueue 实现（需 Redis）
- 启动时根据 `redis.dsn`、`embedder.*`、`milvus.*` 配置自动选择实现
