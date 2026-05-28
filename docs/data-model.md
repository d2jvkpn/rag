# RAG 文档处理数据模型

## 设计原则

- 文档状态放关系库，不放 Milvus
- chunk 原文和审核版本放关系库
- 向量放 Milvus
- 删除和重建按 `document_id` 执行
- 去重边界使用 `knowledge_base_id + sha256`

## `documents`

字段：

- `document_id`
- `created_at`
- `updated_at`
- `knowledge_base_id`
- `filename`
- `title`
- `tags`
- `source_type`
- `storage_path`
- `chunk_snapshot_path`
- `sha256`
- `status`
- `stage`
- `error_message`
- `retry_count`
- `page_count`
- `chunk_count`
- `chunk_version`
- `chunk_config_hash`
- `started_at`
- `finished_at`
- `human_review`：是否需要人工审核；`true` 时切分完成后进入 `review_pending`，`false` 时自动审核通过并继续 embedding / 入库
- `uploader_id`、`uploader_name`：上传者快照，用于所有权中间件鉴权

建议约束：

- 主键：`document_id`，使用 `uuidv7`
- 索引：`knowledge_base_id`
- 唯一约束：`knowledge_base_id + sha256`
- 数据库定义建议使用 `UUID PRIMARY KEY DEFAULT uuidv7()`

字段说明补充：

- `storage_path`：原始上传文件在 `backend/data` 下的本地路径
- `chunk_snapshot_path`：当前生效的 chunk JSON 快照路径
- `chunk_version`：当前 chunk 版本号
- `chunk_config_hash`：切分参数和切分策略配置的哈希，用于判断快照是否可复用
- `tags`：建议使用 PostgreSQL `TEXT[]`，不使用 `jsonb`

## `document_chunks`

字段：

- `chunk_id`
- `created_at`
- `updated_at`
- `document_id`
- `chunk_index`
- `section_title`
- `page_start`
- `page_end`
- `text`
- `normalized_text`
- `status`
- `chunk_version`
- `source`
- `is_current`
- `review_comment`
- `filename`
- `embedding_model`：embedding 完成后写入；快照中也带，便于断电重建
- `embedding`：仅模型层和快照中携带（`[]float32`），数据库不存（向量在 Milvus）
- `resource_refs`

字段说明：

- `text`：最终用于入库的 chunk 文本
- `normalized_text`：清洗后的中间文本，供人工审核参考
- `status`：`draft / approved / rejected`
- `source`：`auto / manual / mixed`
- `is_current`：当前展示和操作的版本标记
- `resource_refs`：chunk 关联的图片、表格、链接等结构化引用信息，建议使用 `jsonb`
- `knowledge_base_id` 不建议在 `document_chunks` 中重复保存，查询时可通过 `documents` 关联获取
- 第一版不单独保存 `text_hash`

`resource_refs` 建议结构：

```json
[
  {
    "ref_id": "img_001",
    "ref_type": "image",
    "label": "图 3",
    "caption": "系统架构图",
    "page": 5,
    "anchor_text": "见图 3",
    "storage_path": "2026/05/27/2026-05-27_doc_123/img_001.png"
  },
  {
    "ref_id": "tbl_002",
    "ref_type": "table",
    "label": "表 2",
    "caption": "模型评测结果",
    "page": 8,
    "anchor_text": "如表 2 所示",
    "storage_path": "2026/05/27/2026-05-27_doc_123/tbl_002.json"
  },
  {
    "ref_id": "lnk_003",
    "ref_type": "link",
    "label": "OpenAI API 文档",
    "anchor_text": "参考文档",
    "url": "https://platform.openai.com/docs/api-reference",
    "is_external": true
  }
]
```

设计约束：

- `text` 中保留可读引用语句，例如“见图 3”或“如表 2 所示”
- `text` 中的链接优先保留锚文本，必要时保留简短可读 URL，不要把过长链接参数直接灌进正文
- `resource_refs` 中保存结构化引用，不要只把资源引用内嵌到纯文本
- `ref_type` 第一版可先支持 `image`、`table` 和 `link`
- `storage_path` 可指向抽取出的图片文件、表格 JSON 或其他派生资源；当前实现保存为相对 `{app.data_dir}/static` 的路径，前端用 `static_base` 拼接访问
- `url` 用于保存链接类引用的完整地址
- 如果第一版不落独立资源表，`ref_id` 只需在单文档范围内唯一

第一版约束：

- 不引入独立的 `document_resources` 表
- 图片、表格、链接等引用先内嵌在 `document_chunks.resource_refs`
- embedding 输入只使用 `document_chunks.text`

## `users`

字段：

- `user_id`
- `created_at`
- `updated_at`
- `username`
- `password_hash`：bcrypt（`golang.org/x/crypto/bcrypt`，`DefaultCost`）
- `status`：`active` / `disabled`，运行时状态，配置 `accounts[].permissions` 不进入此表
- `last_login_at`
- `totp_secret`：未启用时为空
- `totp_enabled`：`true` 时登录需附加 6 位动态码

建议约束：

- 主键：`user_id`，使用 `uuidv7`
- 唯一约束：`username`

主键约定：

- `documents.document_id` 使用 `uuidv7`
- `document_chunks.chunk_id` 使用 `uuidv7`
- `users.user_id` 使用 `uuidv7`
- 如果后续增加新业务主表，默认也使用 `uuidv7`
- migration 可直接写成 `UUID PRIMARY KEY DEFAULT uuidv7()`

## knowledge_bases

知识库元数据是真源，用于校验 `knowledge_base_id`、展示 UI 下拉项，并保存 collection 创建参数。字段：

- `knowledge_base_id`：主键，等于 Milvus collection 名
- `dim`：向量维度，来自创建时的 `embedder.dim`
- `analyzer`：BM25 分词器（`chinese` / `english` / `standard`）
- `chunk_size` / `chunk_overlap` / `min_chunks`：该知识库的切分参数
- `created_by` / `created_at` / `updated_at`

## Milvus Schema

每个 collection 一个知识库。字段：

- `id`：主键，直接使用 `chunk_id`
- `knowledge_base_id`
- `document_id`
- `chunk_id`
- `filename`
- `source_type`
- `section_title`
- `page_start`
- `page_end`
- `chunk_index`
- `text`：原文
- `embedding`：稠密向量，dim 由 `knowledge_bases.dim` 决定
- `sparse`：BM25 稀疏向量，由 Milvus 内置 BM25 function 从 `text` 自动生成（基于 collection 配置的 `analyzer`，默认 `chinese`）

说明：

- 删除按 `knowledge_base_id + document_id` 条件执行
- Milvus 存向量和检索元数据，chunk 全文也存在 Milvus（用于 BM25 / 检索结果展示）；关系库保留完整原文，是真源
- 创建知识库时 `ensureCollection` 检测 schema：发现 `sparse` 缺失或 `analyzer` 不匹配会**drop + recreate collection**（数据丢失，需要重新入库）
