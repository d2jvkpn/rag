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
- 配置文件固定使用 `backend/configs/local.yaml`
- 配置读取使用 `viper`，统一通过 `viper.GetString/GetXX` 获取
- 启动参数使用命令行 flag，不使用环境变量
- 存储双实现：`JSONStore`（本地 JSON 文件，`state_path` 指定）和 `PostgresStore`（`gorm` + `lib/pq`），通过 `database.dsn` 配置自动选择
- 异步处理先使用进程内 goroutine 队列（channel 容量 32）
- chunk 快照写入 `backend/data/chunks/`

当前启动参数：

- `--release bool`
- `--addr string`
- `--config configs/local.yaml`

当前实现取舍（第一阶段历史记录）：

- 先验证”上传 -> 解析 -> 切分 -> 快照 -> 查询 -> 删除”闭环
- 第一阶段未引入 `Asynq`，使用进程内 goroutine 队列；第三阶段已补充
- 第一阶段未接入 `Milvus` 和 embedding API；第三阶段已补充
- 第一阶段 `Logout` 为 no-op；现已实现 JTI + `TokenBlacklist`（内存/Redis 双实现）

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

## 日志约定

后端日志建议使用：

- `zap` 负责结构化日志输出
- `gopkg.in/natefinch/lumberjack.v2` 负责日志文件轮转

第一版要求：

- 日志文件写入 `backend/logs/`
- 同时保留控制台输出和文件输出
- worker 和 API 进程使用统一日志格式
- 不自行实现日志切片和归档逻辑，直接复用 `lumberjack`

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
      {knowledge_base_id}/
        {document_id}/
          source.pdf
    chunks/
      {knowledge_base_id}/
        {document_id}/
          chunks-v1.json
          chunks-v2.json
    resources/
      {knowledge_base_id}/
        {document_id}/
          images/
            img_001.png
          tables/
            tbl_001.json
```

目录说明：

- `backend/data/documents/...`：保存原始上传文件
- `backend/data/chunks/...`：保存 chunk 切分结果的 JSON 快照
- `backend/data/resources/...`：保存图片、表格 JSON 等派生资源

设计要求：

- 路径中带上 `knowledge_base_id` 和 `document_id`
- chunk 快照按版本保存，不直接覆盖旧版本
- 业务静态文件不要放进 `backend/logs/` 或 `backend/target/`

chunk 快照约定：

- chunk 切分完成后，先将当前版本保存为本地 JSON 快照
- JSON 快照建议按版本管理，例如 `chunks-v1.json`
- 快照中保存 `document_id`、`knowledge_base_id`、`chunk_version`、原文哈希、切分参数和完整 chunks 内容
- 后续如果命中有效快照，可直接复用，不再重新切分
- 用户主动触发 `rechunk` 时，应忽略旧快照并生成新版本

## 第一版实现边界

- 第一版支持 `pdf`、`docx`、`pptx`、`markdown`
- 第一版默认不强制人工审核，审核能力作为可选流程
- `documents` 初始状态为 `uploaded`
- 第一版不引入独立的 `document_resources` 表，图片、表格、链接引用先写入 `document_chunks.resource_refs`
- `pdf` 仅支持可提取文本的文件；扫描版 PDF 直接失败，不做 OCR
- embedding 输入只使用 `document_chunks.text`
- chunk 切分完成后，先落本地 JSON 快照，再写入 `document_chunks`
- 如果命中有效的 chunk 快照文件，可直接复用，不必重新切分

## Chunk 策略建议

- 采用“结构优先 + 长度兜底”的混合切分策略
- 第一版按字符数近似，不强依赖 token 计数器
- 默认参数：`chunk_size = 1000`、`chunk_overlap = 150`
- 短文档在清洗后正文不超过约 `3000` 中文字符时，可默认整篇作为一个 chunk
- 即使整篇只生成一个 chunk，也保留 `resource_refs`
- 如果存在有效的 `chunks-vN.json` 快照，且源文件哈希、切分参数、切分策略版本一致，则可直接复用

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

**账户初始化：** `local.yaml` 的 `accounts` 列表在启动时按用户名检查，不存在则插入。`password` 支持明文（启动时自动 bcrypt hash）或已有 bcrypt hash（以 `$2a$`/`$2b$`/`$2y$` 开头，直接存入）。已存在的账户不做修改。

```yaml
accounts:
  - username: admin
    password: "changeme"          # 明文，启动时自动 hash
  - username: ops
    password: "$2a$10$Xyz..."     # 已有 hash，直接写入
```

接口详见 [API 设计](./api.md)。

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
- `backend/migrations/sql/`: SQL migration 文件目录

当前已落地的最小目录结构：

```text
backend/
  cmd/server/
  configs/local.yaml
  data/
  logs/
  target/
  migrations/sql/
  internal/
    api/
    config/
    model/
    parser/
    repository/
    service/
    uuid/
```

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
- `configs/local.yaml` + `viper` 配置加载
- `--release`、`--addr`、`--config` 启动参数
- `users`、`documents`、`document_chunks` 初版 migration（000001）
- `sessions` migration（000002，未使用）
- `documents` 新增 `uploader_id`、`uploader_name` 列 migration（000003）
- `repository.Store` 接口；`JSONStore` 和 `PostgresStore` 双实现
- `PostgresStore`：`gorm` + `lib/pq` driver，支持 `TEXT[]` tags 和 `JSONB` resource_refs
- 启动时根据 `database.dsn` 配置自动选择 store
- 登录、退出、当前用户接口；密码使用 bcrypt 哈希
- 文档上传（记录 uploader）、列表、详情、删除接口
- 文档所有权中间件 `withDocumentOwner()`：非上传者操作变更接口返回 403
- `markdown`、`docx`、`pptx` 基础解析
- 简化版文本型 `pdf` 解析
- chunk 切分和 chunk JSON 快照写入
- `rechunk` 接口和 chunk 版本递增
- chunk 列表查询接口
- chunk 审核接口：approve / reject / merge / edit / index
- 结构化日志（zap + lumberjack，同时输出控制台和 backend/logs/app.log）
- `Embedder` 接口 + Noop 实现 + OpenAI-compatible 实现
- `VectorStore` 接口 + Noop 实现 + Milvus 官方 Go SDK v2 实现（gRPC，Milvus 2.5+）
  - 支持 dense / BM25 / hybrid 三种搜索模式
  - 支持 HNSW ef、BM25 drop_ratio、RRF k 调参
  - 支持按 document_ids 过滤
  - 每个 collection 可独立配置 analyzer（默认 `chinese`），启动时自动检测 schema 并按需重建
- `TaskQueue` 接口 + GoroutineQueue 实现（默认，无额外依赖）+ AsynqQueue 实现（需 Redis）
- 启动时根据 `redis.dsn`、`embedder.*`、`milvus.*` 配置自动选择实现
