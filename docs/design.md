# 设计决策与关键约定

相关文档：

- [系统架构总览](./architecture.md)
- [流程与业务规则](./workflow.md)
- [后端架构与技术方案](./backend.md)
- [API 设计](./api.md)
- [数据模型](./data-model.md)

## 文档生命周期

```
uploaded
  → processing/parse
  → processing/chunk
  → review_pending          ← human_review=true 时停在此处
  → (approve)
  → approved
  → processing/embed
  → processing/index
  → indexed
```

状态转换规则：

- **human_review=false**：切分完成后自动将所有 draft chunk 标记为 approved，跳过 `review_pending`，直接进入 embedding
- **approve**：自动触发 indexing，没有独立的手动 index 步骤
- **indexed**：文档不可再修改。rechunk 请求被阻止；如需重新处理，只能删除后重新上传
- **failed**：可通过 `POST /api/documents/:id/index` 重试，无需重新上传文件

`chunk_version` 在每次 `RechunkDocument` 时递增，embedding 成功后写入 `chunks-vN.json` 快照。快照用于 Milvus collection 重建时免重调 embedding API。

各阶段的业务处理细节（解析策略、清洗步骤、切分参数、删除操作）及 chunk 审核操作列表见 [流程与业务规则](./workflow.md)。

## 知识库与 knowledge_base_id

`knowledge_base_id` 不是普通的标签，它是文档归属边界，承担四个职责：

1. 标识文档所属知识库
2. 上传时校验目标 collection，并选择对应 chunk 参数和 analyzer 配置
3. 检索时的过滤边界，阻止跨知识库召回
4. 向量写入和删除时选择对应 Milvus collection

`knowledge_base_id` 在上传时验证，必须匹配 `milvus.collections[*].collection` 中已配置的 collection 名称。上传后不可修改。即使系统只有单一知识库，也必须保留此字段。

## 数据查询行为

**`ListDocuments(knowledgeBaseID, tag string)`**

- 两个参数均传空字符串时返回全部文档
- 两个过滤条件都推入 DB 查询（`WHERE` 子句），不在内存中二次过滤
- 不允许取全集后在应用层过滤，避免数据量增长后出现性能问题

**`ListDocumentTags(knowledgeBaseID string)`**

- 返回当前范围内去重后的标签列表，每项带 `count`
- 前端文档列表页的标签筛选下拉从此接口取数据，不从当前页表格数据临时聚合
- 传空字符串时返回所有知识库的标签

## 外部组件装配（Noop 回落模式）

`DocumentService` 通过 functional options 装配外部组件。未配置对应 DSN 或 base_url 时自动回落 Noop 实现，不需要改代码，也不会报错。这个设计保证了本地开发无需启动 Redis / Milvus / 外部 API 即可运行。

| 组件 | 包 | 装配函数 | 激活条件 |
|---|---|---|---|
| Embedder | `internal/llm/` | `WithEmbedder()` | `embedder.base_url` + `api_key` 均已配置 |
| VectorStore | `internal/llm/` | `WithVectorStore()` | `milvus.addr` 已配置 |
| TaskQueue | `internal/queue/` | `WithTaskQueue()` | `redis.dsn` 已配置（否则用 GoroutineQueue） |
| LLM | `internal/llm/` | `WithLLM()` | `llm.base_url` + `api_key` 均已配置 |

**Embedder**：`batch_size` 默认 10，DashScope 兼容端点不支持更大批次，不要调大。

**VectorStore** 接口方法：`ValidateKnowledgeBase`、`ListKnowledgeBases`、`Upsert`、`DeleteByDocument`、`Search(ctx, SearchRequest)`。

**TaskQueue**：`GoroutineQueue` 是单 worker goroutine；生产环境建议配置 `AsynqQueue` 以支持进程重启后任务恢复。

## 搜索模式

`POST /api/query` 的 `search_mode` 字段控制检索策略：

| 模式 | 值 | 说明 |
|---|---|---|
| Dense | `""` （默认） | 纯向量语义检索，调用 Embedder |
| BM25 | `"bm25"` | 全文检索，跳过 Embedder，不需要 embedding 配置 |
| Hybrid | `"hybrid"` | 两路并行后 RRF 重排，需要 Embedder |

`SearchRequest` 调参字段：`EF`（HNSW 搜索精度）、`DropRatio`（BM25 稀疏向量剪枝比例）、`RRFK`（Hybrid RRF k 值）。

dense / hybrid 模式未配置 Embedder 时返回 500；BM25 模式无此限制，可在 Noop Embedder 下使用。`answer` 字段在 LLM 为 Noop 时始终返回 `""`。

## 解析与切分接口

**Parser**：`Parse()` 返回 `ParseResult{Text, PageCount, Blocks}`。`Blocks []ParseBlock` 携带结构化单元（`Text`、`SectionTitle`、`PageStart`）；无 blocks 时回落到 flat `Text` 字段。`processDocument` 对每个 block 调用 `CleanText`，再整体传入 `BuildChunks`。

**Chunker**：`BuildChunks(documentID, filename string, blocks []ParseBlock, ...)` 逐 block 调用 `splitByLength`，每个 chunk 继承所在 block 的 `SectionTitle` / `PageStart`。切分完成后，若总 chunk 数 ≤ `min_chunks`，将整篇合并为一个 chunk。

`splitByLength` 细节：以 `\n\n` 为段落边界累积；代码围栏（`` ``` `` / `~~~`）内部的空行受保护；Markdown 表格按数据行拆分（表头重复）；普通超长段落用 `splitRunes` 按字符滑动窗口切分，cut 点向前搜索最近句末标点对齐；`overlapTail` 让 overlap 从句末之后开始，避免从句子中段携带上文。

各文件类型的 block 结构详见 [后端架构与技术方案](./backend.md)。

## 数据库与迁移约定

- schema 变更必须新增编号迁移对（`internal/migrations/sql/`，格式 `NNN_name.up.sql` / `NNN_name.down.sql`），禁止修改已有文件
- 不使用 gorm auto-migrate 作为正式 schema 管理
- 所有业务主表主键统一使用 `UUID PRIMARY KEY DEFAULT uuidv7()`
- 不设 `sessions` 表，认证完全基于 JWT，会话状态通过 `TokenBlacklist` 管理

## 前端约定

**技术栈约束**

- 不引入 TypeScript，保持纯 JavaScript
- 不引入 Sass / Less，使用普通 CSS（全局 token + 组件局部样式）
- Naive UI 全局注册，render function 内部按需 import 组件

**国际化**

前端 i18n 使用轻量自实现：

- 文案目录：`src/i18n/{zh,en}.js`，扁平 key/value 对象
- 组件通过 `useI18n().t(key)` 读取，不允许硬编码字符串
- 当前语言由 Pinia `locale` store 持有，写入 localStorage 持久化
- 新增页面或组件时，先在两个语言文件中补 key，再在组件中使用

**状态映射**

`utils/status.js` 是文档状态的唯一来源：`STATUS_LABEL`（状态 → 文案）、`STATUS_TYPE`（状态 → Naive UI tag type）、`isTerminal(status)`（是否为终态）。组件中不重复定义这些映射。

**Services 封装**

所有接口请求经 `services/http.js` 封装的 fetch 客户端发出（`credentials: 'include'`），错误统一抛出为 `HttpError`。不在组件内直接调用 `fetch`。

## 测试约定

- 测试使用 `t.TempDir()` + `JSONStore`，不 mock 数据库
- 测试不依赖任何外部服务（Redis、Milvus、embedding API）
- `infra.L` 默认 `zap.NewNop()`，测试跳过 `infra.Init()` 不会 panic
- `Init()` 仅在 `main.go` 中调用

## 可观测性约定

**请求日志**

`infra.RequestLogger`（`internal/infra/middleware.go`）在 `c.Next()` 完成后输出请求日志，按 HTTP status 分级：≥500 → Error，≥400 → Warn，其余 → Info。

字段：`ip`、`method`、`path`（路由模板，404 时回落 `URL.Path`）、`status`、`latency`；非空时追加 `params`、`query`；4xx/5xx 时追加 `err_origin`、`err_code`、`err_message`。

`writeError`（`internal/api/response.go`）通过 `runtime.Caller(1)` 捕获调用点（裁剪为 `internal/...`），通过 `c.Set(...)` 传递给中间件，handler 无需主动调日志 API。

**全局 logger**

`infra.L` 包级变量默认 `zap.NewNop()`，保证未调用 `Init()` 的测试不会 panic。正式运行时 `Init()` 写入 `backend/logs/app.log`（zap + lumberjack 轮转），同时保留控制台输出。
