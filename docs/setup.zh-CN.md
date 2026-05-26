# 部署说明

这份文档说明如何在本地或通过 Docker Compose 启动 ProxyDeck。

## 前置依赖

- Go 1.25 或更高版本
- Node.js 20 或更高版本
- Redis 7 或更高版本
- Docker 与 Docker Compose

## 本地开发

### 后端

1. 在 `127.0.0.1:6379` 启动 Redis。
2. 把 [`backend/configs/config.example.yaml`](../backend/configs/config.example.yaml) 复制为 `backend/configs/config.local.yaml`。
3. 设置你自己的 `security.encryption_key` 和 `admin.password`。
4. 启动网关：

```bash
go run ./backend/cmd/gateway -config ./backend/configs/config.local.yaml
```

### 前端

```bash
cd frontend
npm install
npm run dev
```

Vite 开发服务器会把 `/api`、`/metrics`、`/healthz` 代理到 `http://127.0.0.1:8080`。

## Docker Compose

1. 先把 [`.env.example`](../.env.example) 复制成 `.env`
2. 设置 `PROXYDECK_SECURITY_ENCRYPTION_KEY` 和 `PROXYDECK_ADMIN_PASSWORD`
3. 再执行：

```bash
docker compose up --build
```

说明：

- SQLite 数据默认落在 `./data`
- Redis 默认不会暴露到宿主机
- 前端容器会把 API 请求转发给后端容器

## 首次登录

- 地址：`http://127.0.0.1:5173/login`
- 用户名：`admin.username` 的实际值
- 密码：`admin.password` 的实际值

如果仍保留示例占位密钥或密码，后端会直接拒绝启动。

## 建议覆盖的配置

除了本地开发，至少应该覆盖以下环境变量：

- `PROXYDECK_SECURITY_ENCRYPTION_KEY`
- `PROXYDECK_ADMIN_USERNAME`
- `PROXYDECK_ADMIN_PASSWORD`
- `PROXYDECK_SQLITE_PATH`
- `PROXYDECK_REDIS_ADDR`

## 构建检查

```bash
go test ./backend/...
cd frontend && npm run build
```

## 运维接口

- 健康检查：`GET /healthz`
- 指标：`GET /metrics`
- 管理 API 前缀：`/api`
