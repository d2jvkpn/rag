# 第一阶段实施计划

相关文档：

- [总览](../README.md)
- [流程与业务规则](../workflow.md)
- [后端架构与技术方案](../backend.md)
- [API 设计](../api.md)
- [数据模型](../data-model.md)
- [前端业务设计](../frontend-business.md)
- [前端技术方案](../frontend.md)

## 目标

第一阶段只交付一个最小可运行闭环：

- 用户登录
- 上传 `pdf`、`docx`、`pptx`、`markdown`
- 创建文档记录并投递异步任务
- 完成解析、切分、快照落盘和状态写入
- 查询文档状态与 chunk 列表
- 删除文档及其关联数据

第一阶段先不把“完整审核流程”和“正式 embedding + Milvus 入库”作为阻塞上线条件。

## 范围收敛

第一阶段包含：

- 后端项目骨架和基础配置
- `gin` 路由骨架
- `configs/local.yaml` + `viper` 配置加载
- `--release`、`--addr`、`--config` 启动参数
- `users`、`documents`、`document_chunks` 基础 migration
- 登录态与基础鉴权
- 文档上传接口
- 文档列表、详情、删除接口
- 异步解析与切分 worker
- chunk JSON 快照写入 `backend/data/chunks/`
- chunk 列表查询接口
- `rechunk` 接口
- 统一错误响应、参数校验、结构化日志

第一阶段当前实现说明：

- HTTP 服务使用 `gin`
- 状态存储：`JSONStore`（本地 JSON）和 `PostgresStore`（`gorm` + `lib/pq`）双实现，通过 `database.dsn` 配置选择
- 异步处理使用进程内 goroutine 队列（`Asynq` 待接入）
- 鉴权使用 `JWT + HttpOnly Cookie`（`github.com/golang-jwt/jwt/v5`）

第一阶段不包含：

- 人工 chunk 合并、驳回、审核通过
- embedding 外部 API 接入
- Milvus 写入与检索
- OCR
- 多知识库高级权限模型

## 后端落地顺序

1. 初始化 `backend/` 目录和 Go 服务骨架
2. 建立配置、日志、数据库连接、HTTP 路由基础设施
3. 编写 `users`、`documents`、`document_chunks` migration 和模型
4. 实现 `POST /api/login`、`POST /api/logout`、`GET /api/me`
5. 实现 `POST /api/documents`、`GET /api/documents`、`GET /api/documents/:document_id`、`DELETE /api/documents/:document_id`
6. 接入 Asynq 任务投递和 worker handler
7. 先完成 `markdown`、`docx`、`pptx` 解析，再补 `pdf` 文本解析
8. 实现 chunker 和 chunk 快照读写
9. 实现 `GET /api/documents/:document_id/chunks`
10. 补充基础测试和本地运行说明

当前进度：

- 第 1、2、3、4、5、7、8、9、10 步已完成最小骨架
- `rechunk` 已提前落地
- 第 6 步当前用进程内 goroutine 队列替代 `Asynq`
- `PostgresStore` 已完整实现，通过 `database.dsn` 启用；Milvus 和 embedding 仍未开始

## 前端落地顺序

第一阶段前端只做最小页面：

- 登录页
- 文档列表页
- 文档上传入口
- 文档详情页
- chunk 预览页

联调顺序：

1. 先接登录态
2. 再接文档上传和状态轮询
3. 最后接 chunk 列表展示和删除动作

## 验收标准

满足以下条件即可认为第一阶段完成：

- 可以登录并获取当前用户信息
- 可以上传四类支持文件并创建文档记录
- 上传后能看到文档状态从 `uploaded/pending` 推进到 `review_pending` 或 `failed`
- 成功处理的文档可查询 chunk 列表
- chunk 快照已写入本地文件系统
- 删除文档时会同步删除原文件、chunk 记录和快照文件
- API 返回格式、状态名和错误码与设计文档一致

## 进入第二阶段前的前置条件

开始第二阶段前，第一阶段应已经验证以下事实：

- 文档上传和异步处理链路稳定
- `document_chunks` 字段足以支撑审核页展示
- 快照机制可以支持后续 `rechunk`
- 当前 parser 抽象足以接入 embedding 和 Milvus
