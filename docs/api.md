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
- `documents.status` 初始值建议为 `uploaded`

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

## 状态查询建议

文档状态：

- `uploaded`
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
