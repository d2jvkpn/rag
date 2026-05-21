# RAG 文档处理 API 设计

## 范围

第一版 API 只覆盖这些能力：

1. 登录
2. 文档上传
3. 文档状态查询
4. chunk 草稿查询
5. chunk 审核与确认入库
6. 文档删除

## 认证接口

### `POST /api/login`

- 用户名密码登录

### `POST /api/logout`

- 退出登录

### `GET /api/me`

- 获取当前登录用户信息

## 文档接口

### `POST /api/documents`

- 上传 `pdf/docx/pptx/markdown`
- 创建文档记录
- 投递异步解析任务

字段建议：

- `file`
- `knowledge_base_id`
- `title` 可选
- `tags` 可选

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

### `POST /api/documents/:document_id/chunks/rechunk`

- 重新自动切分整个文档

### `POST /api/documents/:document_id/chunks/merge`

- 合并相邻 chunks

### `POST /api/documents/:document_id/chunks/:chunk_id/reject`

- 标记某个 chunk 忽略入库

### `POST /api/documents/:document_id/chunks/approve`

- 审核通过当前 chunk 版本

### `POST /api/documents/:document_id/index`

- 对当前 `approved` chunk 版本触发 embedding 和 Milvus 入库

## 状态查询建议

文档状态：

- `pending`
- `processing`
- `review_pending`
- `reviewing`
- `approved`
- `indexed`
- `failed`

阶段状态：

- `upload`
- `parse`
- `chunk`
- `embed`
- `index`
- `done`
- `delete`
