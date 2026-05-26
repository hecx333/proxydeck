# ProxyDeck

ProxyDeck 是一个单机部署优先的代理网关，用来管理导入的上游代理节点，并通过支持地域筛选和 sticky session 的 HTTP 代理对外提供出口能力。

英文文档见 [`README.md`](README.md)。

## 功能概览

- 将上游代理订阅导入为可管理的节点池
- 通过 Basic Auth HTTP 代理对外提供访问能力，并支持在用户名中携带动态筛选条件
- 支持 sticky session、健康检查、地域和 ASN 过滤、配额限制、流量统计
- 包含管理 API 和 React 管理后台
- 提供 Dockerfile 和 Docker Compose，适合本地或小团队部署

## 技术栈

- 后端：Go、Gin、GORM、SQLite、Redis
- 前端：React、TypeScript、Vite、Ant Design、TanStack Query、ECharts
- 部署：Docker Compose

## 仓库结构

```text
backend/   Go 服务、migration 和运行配置
frontend/  管理后台
docs/      部署和维护文档
data/      本地运行数据目录（已加入 Git 忽略）
```

## 快速开始

### 1. 启动 Redis

```bash
redis-server --save '' --appendonly no --port 6379
```

### 2. 先创建本地配置

```bash
cp ./backend/configs/config.example.yaml ./backend/configs/config.local.yaml
```

在任何非临时本地环境里，至少先修改这两个值：

- `security.encryption_key`
- `admin.password`

### 3. 启动后端

```bash
go run ./backend/cmd/gateway -config ./backend/configs/config.local.yaml
```

### 4. 启动前端

```bash
cd frontend
npm install
npm run dev
```

本地默认地址：

- 管理后台：`http://127.0.0.1:5173`
- 管理 API：`http://127.0.0.1:8080`
- 代理入口：`http://127.0.0.1:20000`

示例配置不会提供可直接用于生产的密钥或密码。
如果保留占位值，后端会拒绝启动。

## Docker Compose

1. 先把 [`.env.example`](.env.example) 复制成 `.env`
2. 设置 `PROXYDECK_SECURITY_ENCRYPTION_KEY` 和 `PROXYDECK_ADMIN_PASSWORD`
3. 再执行：

```bash
docker compose up --build
```

Compose 默认启动：

- `frontend` 暴露 `5173`
- `backend` 暴露 `8080` 和 `20000`
- `redis` 只在 Docker 内部网络可见

## 代理用户名格式

代理用户名支持在请求时携带筛选参数：

```text
{uid}__region=SG__city=Singapore__asn=AS20473__sid=abc123
```

示例：

```bash
curl -x "http://user001__region=SG__sid=demo:pass123@127.0.0.1:20000" \
  https://ipinfo.io/json
```

支持的字段：

- `region`
- `city`
- `asn`
- `isp`
- `sid`

## 配置

请从 [`backend/configs/config.example.yaml`](backend/configs/config.example.yaml) 复制出本地配置，并把真实值放在未跟踪的 `backend/configs/config.local.yaml` 之类文件里。

后端支持以下环境变量覆盖：

- `PROXYDECK_SQLITE_PATH`
- `PROXYDECK_REDIS_ADDR`
- `PROXYDECK_SECURITY_ENCRYPTION_KEY`
- `PROXYDECK_ADMIN_USERNAME`
- `PROXYDECK_ADMIN_PASSWORD`

如果仍然保留示例占位密钥或密码，后端会直接拒绝启动。

## 验证命令

```bash
go test ./backend/...
cd frontend && npm run build
```

## 文档

- 部署说明：[`docs/setup.md`](docs/setup.md)
- 中文部署说明：[`docs/setup.zh-CN.md`](docs/setup.zh-CN.md)
- Migration 说明：[`backend/migrations/README.md`](backend/migrations/README.md)
- 中文 Migration 说明：[`backend/migrations/README.zh-CN.md`](backend/migrations/README.zh-CN.md)

## 许可证

项目使用 MIT License，见 [`LICENSE`](LICENSE)。
