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
- 前端打包 base 由 `BASE_PATH` 环境变量控制；`make build` 使用 `/ui/`，容器镜像将 `target/dist` 复制到后端运行目录 `target/ui`，后端检测到该目录时提供 `/ui` 路由；`/static` 仍由后端业务静态文件路由提供

### 构建时版本注入

`vite.config.js` 通过 `define` 将三个编译期常量注入到前端代码：

| 常量 | 来源（优先级高→低） |
|---|---|
| `__GIT_BRANCH__` | 环境变量 `GIT_BRANCH`，否则执行 `git rev-parse --abbrev-ref HEAD` |
| `__GIT_COMMIT__` | 环境变量 `GIT_COMMIT`，否则执行 `git rev-parse HEAD` |
| `__COMMIT_TIME__` | 环境变量 `COMMIT_TIME`，否则执行 `git log -1 --format=%cI` |

容器构建时由顶层 `Makefile` 通过 `--build-arg` 传入，确保镜像中的前端与后端显示一致的版本信息。本地 `npm run dev` 不设置环境变量时，vite 自动执行 git 命令获取当前值。

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

当前核心路由：

- `/login`
- `/documents`
- `/documents/:documentId`
- `/documents/:documentId/chunks`
- `/search`：语义检索页（调用 `POST /api/knowledge-bases/query`）
- `/users`（仅当当前用户具备 `manage_users` 权限时可访问）
- `/` 重定向到 `/documents`

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

当前目录：

- `services/http.js`
- `services/auth.js`
- `services/documents.js`
- `services/chunks.js`
- `services/users.js`
- `services/knowledge-bases.js`
- `services/search.js`

职责：

- `http.js`：基于原生 `fetch` 的统一请求客户端、错误处理、鉴权基础配置
- `auth.js`：登录、退出、获取当前用户、TOTP setup/enable/disable、修改密码
- `documents.js`：文档上传、列表、详情、删除、触发入库；同时负责文档标签下拉数据（`GET /api/document-tags`）
- `chunks.js`：chunk 列表、重切分、合并、编辑、拒绝、恢复、审核
- `users.js`：用户列表、启用、禁用
- `knowledge-bases.js`：知识库列表、创建、删除（`GET/POST/DELETE /api/knowledge-bases`）
- `search.js`：语义检索（`POST /api/knowledge-bases/query`）、可用 collection 配置（`GET /api/knowledge-bases/available`）、知识库列表（供检索页下拉使用）

实现约定：

- 第一版统一使用原生 `fetch`
- 不引入 `axios`
- 请求封装统一放在 `services/http.js` 及领域 service 中

## 前端配置加载方式

前端不使用 `.env` 配置文件。配置通过运行时 JSON 文件加载，文件名由构建时环境变量 `CONFIG` 决定，默认为 `app.json`。

配置方式：

- 生产：`frontend/public/app.json`，构建后随静态资源部署
- 本地开发：`frontend/public/app.local.json`，`make frontend_run`（`CONFIG=app.local.json npm run dev`）自动使用
- 浏览器加载应用后，通过 HTTP 请求读取对应文件（fetch 使用 `cache: 'no-store'`，确保每次获取最新配置）
- 前端在应用启动阶段加载配置，再初始化后续接口请求和页面渲染

配置文件当前字段：

- `api_base_url`：后端 API 基础地址
- `static_base_url`：后端静态资源基础地址
- `request_timeout_ms`：请求超时，默认 15000
- `poll_interval_ms`：详情页轮询处理中文档的间隔，默认 3000

> 上传流程统一进入人工审核；前端不再提供 `human_review` 开关，并在提交时固定发送 `human_review=true`。
> 上传弹窗中，选择知识库后应立即清除对应的必填校验提示，并展示该 collection 参数。知识库由”知识库管理”页创建；页面对登录用户可见，创建/删除按钮仅对具备 `manage_knowledge_bases` 权限的用户显示。

设计要求：

- 配置文件作为公开静态资源提供，不要放敏感信息
- 前端代码中不要硬编码环境差异配置
- 配置读取失败时，应显示明确错误，而不是静默降级
- `services/http.js` 等请求模块应依赖运行时加载后的配置
- 配置加载层会将 snake_case 字段规范化为前端代码使用的 camelCase 字段，并兼容旧 camelCase 配置

## 前端数据刷新策略

由于后端采用异步处理链路，前端必须考虑处理中状态的刷新机制。

第一版建议：

- 列表页支持手动刷新，不默认自动轮询
- 详情页对处理中任务进行轮询
- 轮询间隔建议 `3 ~ 5` 秒
- 文档进入终态后停止轮询
- 页面进入隐藏状态时暂停轮询，恢复可见后再继续

终态（停止轮询）：

- `indexed`
- `failed`
- `review_pending`（切分完成、等待人工审核，不再轮询）

第一版不建议为了状态刷新引入 WebSocket，轮询已经足够支撑当前业务。

## 前端鉴权建议

认证方式已确定为 `JWT + HttpOnly Cookie`，前端按 Cookie 会话模式处理：

- 登录成功后请求 `GET /api/me`
- 前端不自行持久化 token
- 所有 `/documents` 及其子路由都要求登录
- `/users` 路由除登录外，还要求 `manage_users` 权限
- 路由守卫基于当前用户态判断是否允许访问受保护页面
- 退出登录后清理前端用户态缓存

文档列表页补充约定：

- 列表调用 `GET /api/documents?page=&page_size=` 获取当前页，不在浏览器端对全量文档切片
- 状态筛选通过 `status` 查询参数交给后端执行，避免只过滤当前页数据
- 标签筛选使用可清空下拉框，不使用自由输入
- 下拉选项来自 `GET /api/document-tags`
- 当已选择 `knowledge_base_id` 时，标签下拉按当前知识库范围加载

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


## Chunk 审核页交互约定

`/documents/:documentId/chunks` 左侧 chunk 列表按 `page/page_size` 分页渐进加载，默认每页 50 条；首屏加载第一页，滚动到底加载下一页，点击“跳到最后”直接加载最后一页，随后滚动到顶部可加载上一页。列表头展示当前已加载的 chunk index 范围和总数。右侧详情区优先展示 chunk 正文和清洗后文本。chunk metadata 与 `resource_refs` 不直接铺在正文区，通过详情头部的 `Details ({n})` / `详情 ({n})` 按钮打开合并弹窗查看，按钮中的 `n` 为当前 chunk 的 `resource_refs` 数量；左侧 chunk 列表对有引用的 chunk 显示引用数量标签。`resource_refs` 区域在无引用时显示空态，有引用时展示类型、标签、页码、caption、锚文本、URL、资源路径和外部资源标识。

## 前端上传约定

第一版上传能力建议：

- 支持单文件上传
- 可支持拖拽上传，但不是必须能力
- 先不实现复杂并发上传
- 上传完成后由页面展示异步处理状态，不在前端维护复杂上传任务队列

## 状态与类型映射

文档状态的标签文案、UI 类型（success / warning / error 等）和终态判断统一定义在 `utils/status.js`：

- `STATUS_LABEL`：状态 → 显示文案
- `STATUS_TYPE`：状态 → Naive UI tag type
- `isTerminal(status)`：是否为终态

组件中直接消费这三个导出，不要在页面或列表列定义中重复写映射。

## 前端状态页与空态约定

第一版统一提供以下状态展示：

- 加载中状态
- 请求失败状态
- 空列表状态
- 无 chunk 状态
- 配置加载失败状态

建议通过公共组件统一这些状态表现，避免每个页面重复实现。

## 前端国际化（i18n）

前端使用自实现的轻量 i18n（无第三方库）：

- 文案目录 `src/i18n/{zh,en}.js`，每种语言一个扁平 key/value 对象
- `src/i18n/index.js` 暴露 `useI18n()`，组件中调用 `t(key, vars)` 读取文案
- 当前语言由 Pinia store `stores/locale.js` 持有，并写入 localStorage 持久化
- 页面/组件 UI 文本应使用 `t(...)` 读取字典，不要硬编码字符串

新增页面或组件时：

1. 先在两个语言文件中补 key
2. 在组件中通过 `useI18n` 使用
3. 出现需要变量插值的文案时使用 `{varName}` 占位符

## 前端样式与时间处理约定

第一版前端样式使用普通 CSS：

- 不引入 Sass 或 Less
- 使用全局 token + 组件局部样式组织

日期与时间格式化统一使用：

- `dayjs`

## 前端目录建议

当前目录结构：

```text
frontend/
  public/
    app.json
  src/
    main.js
    App.vue
    router/
      index.js
    stores/
      auth.js
      document-filters.js
      locale.js
    services/
      http.js
      auth.js
      documents.js
      chunks.js
      users.js
      knowledge-bases.js
      search.js
    config/
      app-config.js
    i18n/
      index.js
      zh.js
      en.js
    pages/
      LoginPage.vue
      DocumentsPage.vue
      DocumentDetailPage.vue
      DocumentChunksPage.vue
      SearchPage.vue
      UsersPage.vue
    components/
      AppLayout.vue
    utils/
      status.js
      format.js
  target/
    dist/
```

> 历史 `frontend.md` 中的 `components/{layout,documents,chunks,common}/` 分组未落地，当前所有组件归于 `components/AppLayout.vue` 一个文件；新组件按需新增即可。

## 前端实现原则

- 先做 CSR，不做 SSR
- 先做 REST API，不做实时推送
- 先做页面级数据获取，不做过度抽象
- 先保证状态流转清晰，不做重编辑体验
- 先完成文档后台闭环，再补充高级能力
