# RAG 前端业务设计

相关文档：

- [总览](./overview.md)
- [业务方案](./business.md)
- [架构与技术方案](./architecture.md)
- [API 设计](./api.md)
- [数据模型](./data-model.md)

## 目标

前端第一版的核心目标不是提供复杂编辑能力，而是围绕文档入库流程，提供清晰、可追踪、可回退的操作界面。

需要解决的核心问题：

- 上传文档并触发处理流程
- 查看文档处理状态和错误原因
- 查看 chunk 切分结果
- 在启用人工审核时完成审核操作
- 执行重切分、入库、删除等生命周期管理动作

## 产品定位

前端定位为内部运营后台，而不是面向终端用户的内容消费界面。

设计重点：

- 流程清晰
- 状态可见
- 操作可追踪
- 错误可定位

不作为第一版重点的内容：

- 高级可视化分析
- 复杂富文本编辑
- 批量协作审阅
- 多角色权限编排

## 使用模式

系统存在两种业务模式，前端需要同时兼容：

### 自动入库模式

- 上传文档后直接进入处理链路
- 自动完成解析、切分、embedding、Milvus 入库
- 用户主要查看状态、错误和结果
- 用户仅在需要抽查或处理异常时进入 chunk 详情

### 人工审核模式

- 上传文档后先完成解析和切分
- 文档进入 `review_pending`
- 用户进入 chunk 审核页确认内容
- 审核通过后再触发入库

前端必须明确展示当前文档是否启用人工审核，以及当前所在处理路径。

## 页面结构

第一版建议包含 5 个核心页面：

1. 登录页
2. 文档列表页
3. 上传弹窗或上传页
4. 文档详情页
5. chunk 审核页

## 页面设计

### 登录页

用途：

- 用户名密码登录
- 获取当前登录态

页面元素：

- 用户名输入框
- 密码输入框
- 登录按钮
- 登录失败提示

业务要求：

- 登录成功后进入文档列表页
- 如果已有有效登录态，可直接进入主页面

### 文档列表页

用途：

- 作为系统主工作台
- 查看所有文档的处理进度和结果
- 进入详情、审核、重切分、删除等操作

建议布局：

- 顶部工具栏
- 中部文档表格
- 行内操作区

顶部工具栏建议包含：

- 上传按钮
- `knowledge_base_id` 筛选
- 文档状态筛选
- 文件类型筛选
- 关键词搜索

文档表格建议字段：

- 文档标题或文件名
- 文件类型
- `knowledge_base_id`
- `status`
- `stage`
- `chunk_count`
- `updated_at`
- 错误摘要
- 操作

支持的主要操作：

- 查看详情
- 查看 chunks
- 重新切分
- 触发入库
- 删除文档

业务要求：

- `status` 和 `stage` 需要显式展示，不只显示纯文本
- 失败文档需要突出展示 `error_message`
- 如果文档命中 chunk 快照且已完成切分，可从列表进入详情或 chunk 页面

### 上传弹窗或上传页

用途：

- 上传源文档并创建 `documents` 记录

表单字段：

- `file`
- `knowledge_base_id`
- `title` 可选
- `tags` 可选
- 是否启用人工审核

业务要求：

- 支持上传 `pdf/docx/pptx/markdown`
- 上传成功后显示初始状态 `uploaded`
- 页面应提示后续将进入异步处理流程
- 上传成功后可以跳转到详情页，或回到列表页展示处理中状态

### 文档详情页

用途：

- 展示单个文档的完整处理信息
- 作为文档级操作入口

建议展示内容：

- 文档基础信息
- 原始文件信息
- 当前状态和阶段
- 处理时间
- `chunk_count`
- `chunk_version`
- `chunk_snapshot_path`
- 是否启用人工审核
- 最近错误信息

建议操作：

- 查看 chunk 列表
- 重新切分
- 触发入库
- 删除文档

业务要求：

- 如果文档处于失败状态，需优先展示错误原因
- 如果文档存在有效 chunk 快照，应展示当前快照版本
- 如果文档尚未切分完成，应展示处理中状态而不是空白页

### chunk 审核页

用途：

- 查看当前文档的 chunk 切分结果
- 在启用人工审核时完成审核
- 在未启用人工审核时作为只读检查页

建议布局：

- 左侧 chunk 列表
- 右侧 chunk 详情

左侧列表展示：

- `chunk_index`
- `section_title`
- `page_start ~ page_end`
- `status`
- 文本摘要

右侧详情展示：

- `text`
- `normalized_text`
- `resource_refs`
- 页码范围
- 章节信息
- `chunk_version`

支持的操作：

- 合并相邻 chunk
- 删除 chunk
- 标记忽略入库
- 审核通过
- 重新切分整个文档

业务要求：

- 未启用人工审核时，可隐藏“审核通过”主按钮，保留只读查看和重切分操作
- 启用人工审核时，只有审核通过后的 chunk 版本才能进入入库流程
- 被忽略或拒绝的 chunk 需要有清晰状态标识

## `resource_refs` 展示规则

前端需要统一展示 `image`、`table`、`link` 三类结构化引用。

### 图片引用

展示字段：

- `label`
- `caption`
- `page`
- `storage_path`

建议交互：

- 显示缩略信息
- 支持预览图片或查看路径

### 表格引用

展示字段：

- `label`
- `caption`
- `page`
- `storage_path`

建议交互：

- 显示结构化资源路径
- 后续可扩展为表格预览

### 链接引用

展示字段：

- `label`
- `anchor_text`
- `url`
- `is_external`

建议交互：

- 显示锚文本
- 支持点击跳转原始链接

## 关键业务流程

### 流程一：上传并自动入库

1. 用户上传文档
2. 列表页显示 `uploaded`
3. 后端异步执行解析、切分、embedding、入库
4. 前端轮询或刷新后看到状态推进到 `indexed` 或 `failed`
5. 如果失败，用户进入详情查看错误

### 流程二：上传并人工审核

1. 用户上传文档并启用人工审核
2. 后端完成解析和切分
3. 文档状态进入 `review_pending`
4. 用户进入 chunk 审核页
5. 用户确认 chunk 内容并执行审核通过
6. 系统触发 embedding 和入库
7. 文档状态变为 `indexed` 或 `failed`

### 流程三：重切分

1. 用户在列表页、详情页或审核页触发 `rechunk`
2. 系统忽略当前快照，生成新的 `chunk_version`
3. 新版本 chunk JSON 快照落盘
4. 页面刷新后展示最新 chunk 列表

### 流程四：删除文档

1. 用户发起删除
2. 前端二次确认
3. 后端删除原始文件、chunk 快照、派生资源、数据库记录和向量
4. 删除成功后从列表中移除

## 状态展示规则

前端至少要区分两组状态：

### 文档状态

- `uploaded`
- `pending`
- `processing`
- `review_pending`
- `reviewing`
- `approved`
- `indexed`
- `failed`

### 处理阶段

- `upload`
- `parse`
- `chunk`
- `embed`
- `index`
- `done`
- `delete`

展示要求：

- 状态和阶段同时展示
- `failed` 必须关联错误信息
- `review_pending` 和 `reviewing` 需要明显区别于自动流程状态

## 交互约束

为了避免误操作，以下动作建议二次确认：

- 删除文档
- 重新切分
- 审核通过并触发入库

为了避免用户误判系统状态，以下信息建议始终可见：

- 当前 `status`
- 当前 `stage`
- 最近更新时间
- 错误信息

## 第一版取舍

第一版建议优先实现：

- 登录页
- 文档列表页
- 上传弹窗
- 文档详情页
- chunk 查看与基础审核页

第一版可以暂缓：

- 高级批量操作
- 表格结构化预览
- 复杂 chunk 局部编辑
- 多列看板
- 多人协作审核

## 与后端接口的对应关系

前端主要依赖这些接口：

- `POST /api/login`
- `POST /api/logout`
- `GET /api/me`
- `POST /api/documents`
- `GET /api/documents`
- `GET /api/documents/:document_id`
- `DELETE /api/documents/:document_id`
- `GET /api/documents/:document_id/chunks`
- `POST /api/documents/:document_id/chunks/rechunk`
- `POST /api/documents/:document_id/chunks/merge`
- `POST /api/documents/:document_id/chunks/:chunk_id/reject`
- `POST /api/documents/:document_id/chunks/approve`
- `POST /api/documents/:document_id/index`

## 一句话结论

前端第一版应围绕“文档上传、状态追踪、chunk 查看、可选人工审核、重切分与删除”构建一个信息密度高、状态清晰的运营后台，而不是优先做复杂编辑器或展示型界面。
