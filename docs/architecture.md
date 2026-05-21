# RAG 文档处理架构与技术方案

相关文档：

- [业务方案](./business.md)
- [API 设计](./api.md)
- [数据模型](./data-model.md)
- [前端业务设计](./frontend.md)

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

## 目录约定

仓库不使用共享的根目录 `configs/`、`data/`、`logs/`、`target/`。

目录按前后端分别维护：

- `backend/configs`
- `backend/data`
- `backend/logs`
- `backend/target`
- `frontend/public`
- `frontend/target`

## 前端技术基线

第一版前端继续采用当前选型，不额外引入复杂框架：

- `Vue 3`
- `Vite`
- `JavaScript`
- `Naive UI`
- `Pinia`
- `vue-router`

第一版不建议引入：

- `TypeScript`
- `Nuxt`
- `SSR`
- WebSocket 实时推送
- 富文本编辑器

前端目标是优先跑通“上传、处理、查看、重切分、删除”的后台闭环，而不是提前建设复杂交互能力。

## 前端分层建议

前端代码建议按职责拆成 5 层：

1. `pages`
2. `components`
3. `stores`
4. `services`
5. `utils`

分层职责：

- `pages`：页面级容器，负责路由、布局、页面数据加载
- `components`：通用展示组件和局部交互组件
- `stores`：通过 `Pinia` 管理跨页面共享状态
- `services`：统一封装 API 请求
- `utils`：沉淀状态映射、格式化、类型判断等基础工具

约束建议：

- 不要在页面组件中直接散落接口调用
- 不要过早把所有页面数据都放进全局 store
- 页面局部数据优先在页面内管理，跨页面共享的数据再进入 `Pinia`

## 前端路由建议

第一版建议使用以下核心路由：

- `/login`
- `/documents`
- `/documents/:documentId`
- `/documents/:documentId/chunks`

如果上传能力使用弹窗承载，则无需单独定义 `/upload` 页面路由。

## 前端状态管理建议

第一版推荐只维护少量全局 store：

- `authStore`
- `documentFilterStore`
- 可选：`documentCacheStore`

其中：

- `authStore`：保存当前登录用户和登录态
- `documentFilterStore`：保存列表页筛选条件
- `documentCacheStore`：可选，用于缓存近期访问的文档详情

页面级数据例如当前 chunk 列表、当前文档详情、表单临时状态，优先由页面组件本地管理。

## 前端接口封装建议

前端请求层建议按领域拆分，不在页面内直接发请求。

建议目录：

- `services/http.js`
- `services/auth.js`
- `services/documents.js`
- `services/chunks.js`

职责建议：

- `http.js`：统一请求客户端、错误处理、鉴权基础配置
- `auth.js`：登录、退出、获取当前用户
- `documents.js`：文档上传、列表、详情、删除、触发入库
- `chunks.js`：chunk 列表、重切分、合并、拒绝、审核

## 前端配置加载方式

前端第一版不使用 `.env` 配置文件，不依赖构建时注入配置。

配置方式建议：

- 在 `frontend/public/app.json` 中保存前端运行时配置
- 浏览器加载应用后，通过 HTTP 请求读取 `/app.json`
- 前端在应用启动阶段加载配置，再初始化后续接口请求和页面渲染

适合放入 `app.json` 的配置包括：

- 后端 API 基础地址
- 页面标题
- 默认轮询间隔
- 是否默认启用人工审核入口

设计要求：

- `app.json` 作为公开静态资源提供，不要放敏感信息
- 前端代码中不要硬编码环境差异配置
- 配置读取失败时，应显示明确错误，而不是静默降级
- `services/http.js` 等请求模块应依赖运行时加载后的配置

## 前端数据刷新策略

由于后端采用异步处理链路，前端必须考虑处理中状态的刷新机制。

第一版建议：

- 列表页支持手动刷新
- 详情页对处理中任务进行轮询
- 轮询间隔建议 `3 ~ 5` 秒
- 文档进入终态后停止轮询

终态建议包括：

- `indexed`
- `failed`

第一版不建议为了状态刷新引入 WebSocket，轮询已经足够支撑当前业务。

## 前端鉴权建议

认证方式已确定为 `JWT + HttpOnly Cookie`，前端按 Cookie 会话模式处理：

- 登录成功后请求 `GET /api/me`
- 前端不自行持久化 token
- 路由守卫基于当前用户态判断是否允许访问受保护页面
- 退出登录后清理前端用户态缓存

## 前端 UI 组件建议

`Naive UI` 足以支撑第一版后台界面，优先使用其标准组件完成实现。

建议优先使用：

- `NDataTable`
- `NForm`
- `NModal`
- `NDrawer`
- `NTag`
- `NAlert`
- `NSpin`
- `NTabs`

## 前端目录建议

建议目录结构：

```text
frontend/
  src/
    main.js
    App.vue
    router/
      index.js
    stores/
      auth.js
      document-filters.js
    services/
      http.js
      auth.js
      documents.js
      chunks.js
    config/
      app-config.js
    pages/
      LoginPage.vue
      DocumentsPage.vue
      DocumentDetailPage.vue
      DocumentChunksPage.vue
    components/
      layout/
      documents/
      chunks/
      common/
    utils/
      status.js
      format.js
      resource-refs.js
    styles/
      tokens.css
      main.css
```

## 前端实现原则

- 先做 CSR，不做 SSR
- 先做 REST API，不做实时推送
- 先做页面级数据获取，不做过度抽象
- 先保证状态流转清晰，不做重编辑体验
- 先完成文档后台闭环，再补充高级能力

## 进程划分

- `api`
- `worker`
- `frontend`
- `postgres`
- `milvus`
- `local storage`

前端构建产物目录约定：

- 前端打包文件输出到 `frontend/target/dist`
- 不输出到仓库根目录的 `target/`

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
- 业务静态文件不要放进 `logs/` 或 `target/`

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
backend/
  configs/
  data/
  logs/
  target/
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
  public/
  src/
  target/
    dist/
```

目录约定：

- `backend/configs/`: 后端配置文件目录
- `backend/data/`: 后端数据目录，包含文档原文件、chunk 快照、派生资源等
- `backend/logs/`: 后端日志目录
- `backend/target/`: 后端编译产物目录
- `frontend/public/`: 前端公开静态资源目录，包含 `app.json`
- `frontend/target/dist`: 前端打包产物目录

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
