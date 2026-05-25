# RAG 前端技术方案

相关文档：

- [总览](./README.md)
- [前端业务设计](./frontend-business.md)
- [后端架构与技术方案](./backend.md)
- [API 设计](./api.md)

## 前端技术基线

第一版前端继续采用当前选型，不额外引入复杂框架：

- `Vue 3`
- `Vite`
- `JavaScript`
- `Naive UI`
- `Pinia`
- `vue-router`
- `dayjs`

第一版不建议引入：

- `TypeScript`
- `Nuxt`
- `SSR`
- WebSocket 实时推送
- 富文本编辑器

前端目标是优先跑通“上传、处理、查看、重切分、删除”的后台闭环，而不是提前建设复杂交互能力。

## 目录约定

前端目录约定：

- `frontend/public`
- `frontend/target`

前端构建产物目录约定：

- 前端打包文件输出到 `frontend/target/dist`

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
- `/users`（仅当当前用户具备 `view_user_list` 权限时可访问）

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

补充约定：

- `authStore.user.permissions` 直接来自后端 `/api/me`
- 前端只消费权限，不在本地自行推导权限

页面级数据例如当前 chunk 列表、当前文档详情、表单临时状态，优先由页面组件本地管理。

## 前端接口封装建议

前端请求层建议按领域拆分，不在页面内直接发请求。

建议目录：

- `services/http.js`
- `services/auth.js`
- `services/documents.js`
- `services/chunks.js`
- `services/users.js`

职责建议：

- `http.js`：基于原生 `fetch` 的统一请求客户端、错误处理、鉴权基础配置
- `auth.js`：登录、退出、获取当前用户
- `documents.js`：文档上传、列表、详情、删除、触发入库
- `chunks.js`：chunk 列表、重切分、合并、拒绝、审核
- `users.js`：用户列表、启用、禁用

实现约定：

- 第一版统一使用原生 `fetch`
- 不引入 `axios`
- 请求封装统一放在 `services/http.js` 及领域 service 中

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

- 列表页支持手动刷新，不默认自动轮询
- 详情页对处理中任务进行轮询
- 轮询间隔建议 `3 ~ 5` 秒
- 文档进入终态后停止轮询
- 页面进入隐藏状态时暂停轮询，恢复可见后再继续

终态建议包括：

- `indexed`
- `failed`

第一版不建议为了状态刷新引入 WebSocket，轮询已经足够支撑当前业务。

## 前端鉴权建议

认证方式已确定为 `JWT + HttpOnly Cookie`，前端按 Cookie 会话模式处理：

- 登录成功后请求 `GET /api/me`
- 前端不自行持久化 token
- 所有 `/documents` 及其子路由都要求登录
- `/users` 路由除登录外，还要求 `view_user_list` 权限
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

统一约定：

- 全局提示统一使用 `Naive UI`
- 表格统一使用 `NDataTable`
- 图标体系统一使用 `Naive UI` 兼容方案，不额外混用多套组件风格

提示建议：

- 成功或失败短反馈使用 `message`
- 危险操作确认使用 `dialog`
- 不混用多套提示机制

## 前端上传约定

第一版上传能力建议：

- 支持单文件上传
- 可支持拖拽上传，但不是必须能力
- 先不实现复杂并发上传
- 上传完成后由页面展示异步处理状态，不在前端维护复杂上传任务队列

## 前端状态页与空态约定

第一版统一提供以下状态展示：

- 加载中状态
- 请求失败状态
- 空列表状态
- 无 chunk 状态
- 配置加载失败状态

建议通过公共组件统一这些状态表现，避免每个页面重复实现。

## 前端样式与时间处理约定

第一版前端样式使用普通 CSS：

- 不引入 Sass 或 Less
- 使用全局 token + 组件局部样式组织

日期与时间格式化统一使用：

- `dayjs`

## 前端目录建议

建议目录结构：

```text
frontend/
  public/
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
  target/
    dist/
```

## 前端实现原则

- 先做 CSR，不做 SSR
- 先做 REST API，不做实时推送
- 先做页面级数据获取，不做过度抽象
- 先保证状态流转清晰，不做重编辑体验
- 先完成文档后台闭环，再补充高级能力
