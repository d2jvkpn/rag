# Backend Scaffold

当前目录提供第一阶段后端最小骨架，目标是先跑通这条链路：

- 登录
- 文档上传
- 本地文件落盘
- 后台解析与切分
- chunk 快照写入
- 文档详情和 chunk 查询
- 文档删除

## PostgreSQL 配置（可选）

编辑 `configs/local.yaml`，取消注释并填写 DSN：

```yaml
database:
  dsn: "postgres://user:password@localhost:5432/ragdb?sslmode=disable"
```

配置后服务启动时自动使用 PostgreSQL；不配置则继续使用本地 JSON 文件。

初次使用 PostgreSQL 需要先执行迁移：

```bash
# 安装 migrate CLI
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# 执行迁移
migrate -path internal/migrations/sql -database "postgres://user:password@localhost:5432/ragdb?sslmode=disable" up
```

## 当前实现取舍

- 使用 `gin` 实现 HTTP 路由和中间件
- 使用本地 JSON 状态文件代替 PostgreSQL，后续可替换为真正的 repository 实现
- 使用后台 goroutine 队列代替 Asynq，先验证异步处理链路
- `markdown`、`docx`、`pptx` 已实现基础文本提取，其中 `docx/pptx` 原生表格会转成 Markdown 表格文本
- `pdf` 使用 `pdfplumber` 提取文本并尝试抽取页内表格；扫描版、复杂跨页表或复杂编码 PDF 仍可能失败

## 运行方式

```bash
cd backend
go run ./cmd/server --config configs/local.yaml
```

可用命令行参数：

- `--release bool`
- `--addr string`
- `--config configs/local.yaml`

监听地址优先级：

- 先使用 `--addr`
- 未传时使用配置文件里的 `http.addr`

默认账号：

- `admin`
- `admin123`

## 配置文件

配置文件固定使用 [configs/local.yaml](/home/appuser/workspace/rag.git/backend/configs/local.yaml:1)。

当前实现约定：

- 不使用 `.env`
- 不做 YAML 结构体反序列化
- 统一通过 `viper.Viper.GetString/GetXX` 读取配置

当前配置项：

- `http.addr`
- `http.jwt_secret`
- `http.jwt_token_ttl`
- `http.session_cookie`
- `http.allow_origins`
- `app.data_dir`
- `app.state_path`
- `accounts[].username`
- `accounts[].password`
- `accounts[].permissions`
