# RAG Backend

当前目录提供第一阶段后端实现，目标是跑通文档处理闭环：

- 登录和 Cookie 会话
- 文档上传与原文件落盘
- 后台解析、切分、重切分
- chunk 快照写入
- 文档详情、chunk 查询、搜索接口
- 文档删除及关联数据清理

## 运行方式

```bash
cd backend
go run ./cmd/server --config examples/local.yaml
```

可用命令行参数：

- `--release bool`
- `--addr string`
- `--config string`

监听地址优先级：

- 优先使用 `--addr`
- 未传时使用配置文件里的 `http.addr`
- 配置缺省时使用代码默认值 `:3061`

默认账号来自配置文件 `accounts[]`。示例配置提供：

- `admin`
- `admin123`

## 配置文件

示例配置文件：[examples/local.yaml](/home/appuser/workspace/rag.git/backend/examples/local.yaml:1)。

当前实现约定：

- 不使用 `.env`
- 通过 `viper.Viper.GetString/GetXX` 读取配置
- `database.dsn` 为空时使用本地 JSON store
- `redis.dsn` 为空时使用内存 token blacklist 和进程内 goroutine 任务队列
- `embedder.base_url` / `embedder.api_key` 为空时使用 Noop embedder
- `milvus.addr` 为空时使用 Noop vector store
- `llm.base_url` / `llm.api_key` 为空时不生成回答

常用配置项：

- `http.addr`
- `http.base_path`
- `http.jwt_secret`
- `http.jwt_token_ttl`
- `http.session_cookie`
- `http.allow_origins`
- `app.data_dir`
- `app.state_path`
- `accounts[].username`
- `accounts[].password`
- `accounts[].permissions`
- `database.dsn`
- `redis.dsn`
- `embedder.base_url`
- `embedder.api_key`
- `embedder.model`
- `embedder.batch_size`
- `milvus.addr`
- `milvus.db`
- `milvus.collections[]`
- `llm.base_url`
- `llm.api_key`
- `llm.model`

## PostgreSQL 配置（可选）

设置 `database.dsn` 后，服务启动时使用 PostgreSQL；不配置则使用本地 JSON 文件。

```yaml
database:
  dsn: "postgres://user:password@localhost:5432/ragdb?sslmode=disable"
```

初次使用 PostgreSQL 需要先执行迁移：

```bash
# 安装 migrate CLI
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# 执行迁移
migrate -path internal/migrations/sql -database "postgres://user:password@localhost:5432/ragdb?sslmode=disable" up
```

## Redis / Asynq 配置（可选）

设置 `redis.dsn` 后，服务使用 Redis token blacklist，并启用 Redis-backed Asynq 任务队列；不配置则使用内存 blacklist 和进程内 goroutine 队列。

```yaml
redis:
  dsn: "redis://127.0.0.1:6379/0"
```

## 当前实现取舍

- HTTP 服务使用 `gin`
- 状态存储支持本地 JSON 和 PostgreSQL，通过 `database.dsn` 选择
- 异步任务支持进程内 goroutine 队列和 Asynq，通过 `redis.dsn` 选择
- `markdown`、`docx`、`pptx` 已实现基础文本提取，其中 `docx/pptx` 原生表格会转成 Markdown 表格文本
- `pdf` 使用 `pdfplumber` 提取文本并尝试抽取页内表格；扫描版、复杂跨页表或复杂编码 PDF 仍可能失败
- embedding、Milvus、LLM 都按配置启用；未配置时使用 Noop 实现或不生成回答
