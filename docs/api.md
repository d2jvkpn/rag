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

## 文档接口

### `POST /api/documents`

- 上传 `pdf/docx/pptx/markdown`
- 创建文档记录（同步），投递异步解析任务
- `documents.status` 初始值为 `uploaded`
- 返回 `202 Accepted`（文档已创建，处理异步进行）
- 自动记录 `uploader_id` 和 `uploader_name`（取自当前登录用户）

字段建议：

- `file`
- `knowledge_base_id`
- `title` 可选
- `tags` 可选

### 文档所有权

所有用户均可查看所有文档，但以下操作仅限文档上传者（返回 `403 forbidden`）：

- `DELETE /api/documents/:id`
- `POST /api/documents/:id/chunks/rechunk`
- `POST /api/documents/:id/chunks/approve`
- `POST /api/documents/:id/chunks/merge`
- `POST /api/documents/:id/chunks/:chunk_id/edit`
- `POST /api/documents/:id/chunks/:chunk_id/reject`
- `POST /api/documents/:id/chunks/:chunk_id/restore`
- `POST /api/documents/:id/index`

注：`uploader_id` 为空的文档（存量数据）不受所有权限制。

### `GET /api/documents`

- 文档列表查询

### `GET /api/documents/:document_id`

- 查询单个文档详情和处理状态

### `DELETE /api/documents/:document_id`

- 删除文档
- 删除本地原文件
- 删除 chunk 记录
- 删除 Milvus 向量

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

- 标记某个 chunk 忽略入库

### `POST /api/documents/:document_id/chunks/approve`

- 审核通过当前 chunk 版本

### `POST /api/documents/:document_id/index`

- 对当前 `approved` chunk 版本触发 embedding 和 Milvus 入库

如果未开启人工审核，系统可在自动切分完成后直接触发 embedding 和入库，不必等待 `approve`。

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
        "chunk_overlap": 64
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

文档状态：

- `uploaded`：文档已上传，等待处理
- `processing`：解析或切分进行中
- `review_pending`：切分完成，等待人工审核（或直接入库）
- `reviewing`：人工审核进行中
- `approved`：审核通过，待触发入库
- `indexed`：已完成 embedding 和向量写入
- `failed`：某阶段处理失败，错误原因见 `error_message`

阶段状态：

- `upload`
- `parse`
- `chunk`
- `embed`
- `index`
- `done`
- `delete`

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
