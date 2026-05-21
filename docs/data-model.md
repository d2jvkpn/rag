# RAG 文档处理数据模型

## 设计原则

- 文档状态放关系库，不放 Milvus
- chunk 原文和审核版本放关系库
- 向量放 Milvus
- 删除和重建按 `document_id` 执行
- 去重边界使用 `knowledge_base_id + sha256`

## `documents`

字段建议：

- `document_id`
- `knowledge_base_id`
- `filename`
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
- `created_at`
- `updated_at`

建议约束：

- 主键：`document_id`
- 索引：`knowledge_base_id`
- 唯一约束：`knowledge_base_id + sha256`

字段说明补充：

- `storage_path`：原始上传文件在 `backend/data` 下的本地路径
- `chunk_snapshot_path`：当前生效的 chunk JSON 快照路径
- `chunk_version`：当前 chunk 版本号
- `chunk_config_hash`：切分参数和切分策略配置的哈希，用于判断快照是否可复用

## `document_chunks`

字段建议：

- `chunk_id`
- `document_id`
- `knowledge_base_id`
- `chunk_index`
- `section_title`
- `page_start`
- `page_end`
- `text`
- `normalized_text`
- `text_hash`
- `status`
- `chunk_version`
- `source`
- `is_current`
- `review_comment`
- `filename`
- `embedding_model`
- `resource_refs`
- `created_at`
- `updated_at`

字段说明：

- `text`：最终用于入库的 chunk 文本
- `normalized_text`：清洗后的中间文本，供人工审核参考
- `status`：`draft / approved / rejected`
- `source`：`auto / manual / mixed`
- `is_current`：当前展示和操作的版本标记
- `resource_refs`：chunk 关联的图片、表格、链接等结构化引用信息，建议使用 `jsonb`

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
    "storage_path": "backend/data/resources/kb_001/doc_123/images/img_001.png"
  },
  {
    "ref_id": "tbl_002",
    "ref_type": "table",
    "label": "表 2",
    "caption": "模型评测结果",
    "page": 8,
    "anchor_text": "如表 2 所示",
    "storage_path": "backend/data/resources/kb_001/doc_123/tables/tbl_002.json"
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
- `storage_path` 可指向抽取出的图片文件、表格 JSON 或其他派生资源
- `url` 用于保存链接类引用的完整地址
- 如果第一版不落独立资源表，`ref_id` 只需在单文档范围内唯一

第一版约束：

- 不引入独立的 `document_resources` 表
- 图片、表格、链接等引用先内嵌在 `document_chunks.resource_refs`
- embedding 输入只使用 `document_chunks.text`

## `users`

字段建议：

- `user_id`
- `username`
- `password_hash`
- `status`
- `last_login_at`
- `created_at`
- `updated_at`

建议约束：

- 主键：`user_id`
- 唯一约束：`username`

## Milvus Schema

建议字段：

- `id`
- `knowledge_base_id`
- `document_id`
- `chunk_id`
- `filename`
- `source_type`
- `section_title`
- `page_start`
- `page_end`
- `chunk_index`
- `text`
- `text_hash`
- `embedding`

说明：

- `id` 可直接使用 `chunk_id`
- 删除条件建议使用 `knowledge_base_id + document_id`
- Milvus 存向量和核心检索元数据，chunk 全文仍保留在关系库
