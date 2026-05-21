# RAG 文档处理架构与技术方案

相关文档：

- [业务方案](./business.md)
- [API 设计](./api.md)
- [数据模型](./data-model.md)

## 推荐架构

建议拆成 3 个逻辑层：

1. `ingest api`
2. `parser/chunker worker`
3. `embedding/index worker`

## 技术选型基线

- Web 框架：`gin`
- 异步任务：`Asynq`
- 前端：`Vue 3 + Vite + JavaScript`
- 前端 UI：`Naive UI`
- 前端状态管理：`Pinia`
- 前端路由：`vue-router`
- 原始文档存储：本地文件存储
- 关系库：`PostgreSQL`
- 向量库：`Milvus`
- PDF 解析：Python 生态工具
- DOCX 解析：Go 内直接解析
- PPTX 解析：Go 内直接解析
- Markdown 解析：Go 内直接解析
- Embedding：外部 API
- 配置管理：`yaml + viper`
- 日志：`zap`
- 监控：任务记录 + logging

## 进程划分

- `api`
- `worker`
- `frontend`
- `postgres`
- `milvus`
- `local storage`

## 数据模型

详见 [数据模型](./data-model.md)。

## Milvus Schema 建议

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

## 去重与重建策略

- 同一知识库内按 `sha256` 去重
- 文档更新时走“全量删除 + 全量重建”
- 人工审核通过的 chunk 版本不要被后台静默覆盖

## 用户系统

第一版只做最小登录能力：

- 用户名密码登录
- 登录态校验
- 退出登录

建议认证方式：

- `JWT + HttpOnly Cookie`

接口详见 [API 设计](./api.md)。

## 前端页面建议

1. 登录页
2. 文档列表页
3. 文档上传页或上传弹窗
4. 文档详情页
5. chunk 审核页

## 推荐目录结构

```text
configs/
data/
logs/
target/
backend/
  internal/
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
frontend/
  src/
  public/
```

目录约定：

- `configs/`: 配置文件目录
- `data/`: 数据目录，包含本地存储文件等
- `logs/`: 日志目录
- `target/`: `backend` 编译产物目录，`frontend` 打包产物目录

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
