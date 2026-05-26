# RAG 文档处理 API 设计

## 范围

第一版 API 只覆盖这些能力：

1. 登录
2. 文档上传
3. 文档状态查询
4. chunk 草稿查询
5. chunk 审核与确认入库
6. 文档删除

## 统一响应格式

成功返回统一使用：

```json
{
  "data": {
    "accepted": true
  }
}
```

分页列表返回统一使用：

```json
{
  "data": {
    "items": [],
    "page": 1,
    "page_size": 20,
    "total": 100
  }
}
```

错误返回统一使用：

```json
{
  "error": {
    "code": "validation_error",
    "message": "knowledge_base_id is required"
  }
}
```

字段级错误返回统一使用：

```json
{
  "error": {
    "code": "validation_error",
    "message": "invalid request parameters",
    "details": [
      {
        "field": "knowledge_base_id",
        "reason": "required"
      }
    ]
  }
}
```

响应约定：

- 成功响应统一放在 `data` 字段下
- 错误响应统一放在 `error` 字段下
- 列表集合字段统一命名为 `items`
- `details` 始终使用数组，不混用对象和数组
- 不在 JSON 中重复返回 `http_status`

## 错误码约定

第一版统一使用以下错误码：

- `validation_error`
- `unauthorized`
- `forbidden`
- `not_found`
- `conflict`
- `unsupported_file_type`
- `processing_failed`
- `internal_error`

## HTTP 状态码约定

- `200 OK`：普通成功读取
- `201 Created`：创建成功
- `202 Accepted`：异步任务已接受
- `204 No Content`：删除成功
- `400 Bad Request`：参数错误
- `401 Unauthorized`：未登录
- `403 Forbidden`：无权限
- `404 Not Found`：资源不存在
- `409 Conflict`：状态冲突、重复上传
- `415 Unsupported Media Type`：文件类型不支持
- `500 Internal Server Error`：服务端错误

## 认证接口

### `POST /api/login`

- 用户名密码登录

### `POST /api/logout`

- 退出登录
- 清除客户端 Cookie，服务端将 token JTI 写入 `TokenBlacklist`
- 已登出的 token 后续请求将返回 `401 unauthorized`

### `GET /api/me`

- 获取当前登录用户信息
- 响应包含 `totp_enabled` 字段，表示当前用户是否已开启两步验证
- 响应包含 `permissions` 字段；该字段来自配置文件 `accounts[].permissions`，不从数据库读取

### `POST /api/login`（TOTP 两步验证）

当账户已开启 TOTP 时，登录分两步：

**第一步**：提交用户名和密码（不含 `totp_code`）

```json
{ "username": "admin", "password": "..." }
```

服务端验证凭据后，若账户已开启 TOTP，返回：

```json
{ "data": { "totp_required": true } }
```

HTTP 状态码仍为 `200`，不设置 Cookie。

**第二步**：重新提交，附上动态验证码：

```json
{ "username": "admin", "password": "...", "totp_code": "123456" }
```

验证码正确后，正常登录并设置 Cookie。

### `PUT /api/me/password`

- 修改当前登录用户密码
- 请求体：`{ "old_password": "...", "new_password": "..." }`
- 旧密码校验失败返回 `400 validation_error`，`details[0]` 指向 `old_password`

### `POST /api/me/totp/setup`

- 为当前用户初始化 TOTP，生成 secret 和 QR Code URL
- 此时 `totp_enabled` 仍为 `false`，secret 存入数据库但未激活
- 响应：`{ "secret": "...", "qr_url": "otpauth://totp/..." }`

### `POST /api/me/totp/enable`

- 验证用户输入的 6 位验证码，正确后将 `totp_enabled` 设为 `true`
- 请求体：`{ "code": "123456" }`
- 已验证后续登录需要提供 TOTP 验证码

### `POST /api/me/totp/disable`

- 验证用户输入的 6 位验证码，正确后清除 secret，将 `totp_enabled` 设为 `false`
- 请求体：`{ "code": "123456" }`

## 文档接口

### `POST /api/documents`

- 上传 `pdf/docx/pptx/markdown`
- 创建文档记录（同步），投递异步解析任务
- `documents.status` 初始值为 `uploaded`
- 返回 `202 Accepted`（文档已创建，处理异步进行）
- 自动记录 `uploader_id` 和 `uploader_name`（取自当前登录用户）

字段（multipart/form-data）：

- `file`：必填
- `knowledge_base_id`：必填，须匹配配置中的 collection
- `title`：可选
- `tags`：可选，可重复字段
- `human_review`：可选，`"true"` 表示切分完成后进入 `review_pending` 等待审核；其他值或缺省走自动入库

### 文档所有权与删除权限

所有用户均可查看所有文档。

以下操作仅限文档上传者（返回 `403 forbidden`）：

- `POST /api/documents/:id/chunks/rechunk`
- `POST /api/documents/:id/chunks/approve`
- `POST /api/documents/:id/chunks/merge`
- `PUT  /api/documents/:id/chunks/:chunk_id`
- `POST /api/documents/:id/chunks/:chunk_id/reject`
- `POST /api/documents/:id/chunks/:chunk_id/restore`
- `POST /api/documents/:id/index`

注：`uploader_id` 为空的文档（存量数据）不受所有权限制。

`DELETE /api/documents/:id` 允许两类用户执行：

- 文档上传者本人
- 配置权限中包含 `delete_documents` 的用户

### `GET /api/documents`

- 文档列表查询
- 支持 `knowledge_base_id` 过滤
- 支持 `tag` 精确过滤，匹配 `documents.tags` 中的单个标签

### `GET /api/document-tags`

- 返回去重后的文档标签列表
- 支持可选 `knowledge_base_id` 过滤
- 响应项包含：
  - `tag`
  - `count`
- 前端文档列表页使用该接口构建标签筛选下拉，不从当前页表格数据临时聚合

### `GET /api/documents/:document_id`

- 查询单个文档详情和处理状态

### `DELETE /api/documents/:document_id`

- 删除文档
- 删除本地原文件
- 删除 chunk 记录
- 删除 Milvus 向量

## 用户接口

### `GET /api/users`

- 返回全部账户列表
- 需要权限 `view_user_list`
- 返回字段包含数据库中的 `status`，以及按用户名从配置补出的 `permissions`

### `POST /api/users/:user_id/disable`

- 将目标用户状态设为 `disabled`
- 需要权限 `disable_users`
- 被禁用用户后续登录返回 `403 forbidden`
- 已登录但被禁用的用户，在后续请求鉴权阶段返回 `403 forbidden`
- 不允许禁用自己

### `POST /api/users/:user_id/enable`

- 将目标用户状态设为 `active`
- 需要权限 `disable_users`
- 不允许操作自己的状态

## Chunk 审核接口

### `GET /api/documents/:document_id/chunks`

- 获取当前文档的 chunk 列表
- 返回每个 chunk 的正文文本，以及结构化资源引用字段 `resource_refs`

### `POST /api/documents/:document_id/chunks/rechunk`

- 重新自动切分整个文档
- 忽略当前 chunk 快照，强制生成新的 chunk 版本和 JSON 快照

### `POST /api/documents/:document_id/chunks/merge`

- 合并相邻 chunks

### `POST /api/documents/:document_id/chunks/:chunk_id/reject`

- 标记某个 chunk 忽略入库（`is_current=false, status=rejected`）

### `POST /api/documents/:document_id/chunks/:chunk_id/restore`

- 将已 `rejected` 的 chunk 恢复为 `draft`、`is_current=true`

### `PUT /api/documents/:document_id/chunks/:chunk_id`

- 编辑某个 chunk 的正文
- 请求体：`{ "text": "..." }`
- 编辑后 `source=manual`

### `POST /api/documents/:document_id/chunks/approve`

- 审核通过当前文档的全部 `draft` chunk（标记为 `approved`），文档状态变为 `approved` 并**自动触发 embedding 和入库**（无独立手动步骤）

### `POST /api/documents/:document_id/index`

- 对当前 `approved` chunk 版本触发 embedding 和 Milvus 入库
- 仅用于 `failed` 文档的失败重试。正常流程下 `approve` 已自动触发，不需要单独调用

如果未开启人工审核，系统在自动切分完成后直接进入入库流程。

## 知识库接口

### `GET /api/knowledge-bases/available`

- 返回已配置的 Milvus collection 列表及其参数

响应：

```json
{
  "data": {
    "items": [
      {
        "knowledge_base_id": "public",
        "dim": 1024,
        "analyzer": "chinese",
        "chunk_size": 512,
        "chunk_overlap": 64,
        "min_chunks": 3
      }
    ]
  }
}
```

### `GET /api/knowledge-bases`

- 返回各知识库的文档数量（从 DB 扫描，不查询 Milvus）

## 语义检索接口

### `POST /api/query`

- 输入查询文本，在指定知识库中检索相似 chunk
- 不支持跨知识库检索，`knowledge_base_id` 为必填项
- `search_mode` 为空时使用 dense（纯向量语义搜索）；`bm25` 时跳过 Embedder，仅做全文检索；`hybrid` 时两路并行后 RRF 重排

请求字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| `knowledge_base_id` | string | 必填 |
| `query` | string | 查询文本，必填 |
| `top_k` | int | 返回条数，默认 5，最大 50 |
| `search_mode` | string | `""`（dense）/ `"bm25"` / `"hybrid"` |
| `document_ids` | []string | 可选，限定搜索范围内的文档 ID 列表 |
| `ef` | int | HNSW 搜索精度参数，0 = Milvus 默认 |
| `drop_ratio` | float | BM25 稀疏向量剪枝比例，0 = 不剪枝 |
| `rrf_k` | int | Hybrid RRF 重排 k 值，0 = 默认 60 |

响应：

```json
{
  "data": {
    "query": "...",
    "knowledge_base_id": "public",
    "answer": "...",
    "items": [
      {
        "chunk_id": "...",
        "document_id": "...",
        "knowledge_base_id": "public",
        "filename": "report.pdf",
        "source_type": "pdf",
        "section_title": "...",
        "page_start": 3,
        "page_end": 4,
        "chunk_index": 2,
        "text": "...",
        "score": 0.92
      }
    ]
  }
}
```

- `answer`：LLM 基于 chunk 上下文生成的回答，未配置 LLM 时为 `""`
- `score`：dense 模式为余弦相似度（0~1），bm25/hybrid 模式为原始分值
- dense/hybrid 模式未配置 `embedder.base_url` + `embedder.api_key` 时返回 500

## 状态查询建议

文档状态（`documents.status` 实际取值）：

- `uploaded`：文档已上传，等待异步处理
- `processing`：解析、切分、embedding 或入库阶段进行中（具体看 `stage`）
- `review_pending`：人工审核模式下切分完成，等待审核
- `approved`：审核通过，已触发 embedding 流程
- `indexed`：已完成 embedding 和向量写入，文档不可再修改
- `failed`：某阶段失败，错误见 `error_message`

阶段（`documents.stage` 实际取值）：

- `upload`
- `parse`
- `chunk`
- `embed`
- `index`
- `done`

## Chunk 返回字段建议

`GET /api/documents/:document_id/chunks` 的单条 chunk 建议至少包含：

- `chunk_id`
- `chunk_index`
- `section_title`
- `page_start`
- `page_end`
- `text`
- `normalized_text`
- `status`
- `resource_refs`

其中：

- `text` 中保留对图片、表格、链接的自然语言引用，便于语义检索和人工审核
- `resource_refs` 中保存结构化引用，便于前端预览、跳转、定位原始资源
- embedding 输入建议只使用 `text`，不直接使用 `resource_refs`

`resource_refs` 字段示例：

```json
[
  {
    "ref_id": "img_001",
    "ref_type": "image",
    "label": "图 3",
    "caption": "系统架构图",
    "page": 5,
    "anchor_text": "见图 3",
    "storage_path": "backend/data/resources/kb_001/doc_123/images/img_001.png"
  },
  {
    "ref_id": "lnk_002",
    "ref_type": "link",
    "label": "OpenAI API 文档",
    "anchor_text": "参考文档",
    "url": "https://platform.openai.com/docs/api-reference",
    "is_external": true
  }
]
```
