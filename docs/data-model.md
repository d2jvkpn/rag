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
- `sha256`
- `status`
- `stage`
- `error_message`
- `retry_count`
- `page_count`
- `chunk_count`
- `started_at`
- `finished_at`
- `created_at`
- `updated_at`

建议约束：

- 主键：`document_id`
- 索引：`knowledge_base_id`
- 唯一约束：`knowledge_base_id + sha256`

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
- `created_at`
- `updated_at`

字段说明：

- `text`：最终用于入库的 chunk 文本
- `normalized_text`：清洗后的中间文本，供人工审核参考
- `status`：`draft / approved / rejected`
- `source`：`auto / manual / mixed`
- `is_current`：当前展示和操作的版本标记

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
