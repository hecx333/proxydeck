# ProxyDeck

ProxyDeck is a single-host proxy gateway for managing imported upstream proxy nodes and exposing them through a region-aware sticky HTTP proxy.

中文文档见 [`README.zh-CN.md`](README.zh-CN.md)。

## What It Does

- Imports upstream proxy subscriptions into a managed node pool
- Exposes an HTTP proxy with Basic Auth credentials and dynamic filters in the username
- Supports sticky sessions, health checks, region and ASN filtering, quota enforcement, and traffic stats
- Includes an admin API and a React-based admin console
- Ships with Dockerfiles and a Docker Compose setup for local or small-team deployment

## Stack

- Backend: Go, Gin, GORM, SQLite, Redis
- Frontend: React, TypeScript, Vite, Ant Design, TanStack Query, ECharts
- Deployment: Docker Compose

## Repository Layout

```text
backend/   Go services, migrations, and runtime config
frontend/  Admin console
docs/      Setup and maintenance notes
data/      Local runtime data directory (ignored by Git)
```

## Quick Start

### 1. Start Redis

```bash
redis-server --save '' --appendonly no --port 6379
```

### 2. Create a local config

```bash
cp ./backend/configs/config.example.yaml ./backend/configs/config.local.yaml
```

Set at least these values before you run anything outside a throwaway local environment:

- `security.encryption_key`
- `admin.password`

### 3. Start the backend

```bash
go run ./backend/cmd/gateway -config ./backend/configs/config.local.yaml
```

### 4. Start the frontend

```bash
cd frontend
npm install
npm run dev
```

Local defaults:

- Admin UI: `http://127.0.0.1:5173`
- Admin API: `http://127.0.0.1:8080`
- Proxy endpoint: `http://127.0.0.1:20000`

The sample config does not ship with a usable production secret or password.
If you keep the placeholder values, the backend will refuse to start.

## Docker Compose

1. Copy [`.env.example`](.env.example) to `.env`
2. Set `PROXYDECK_SECURITY_ENCRYPTION_KEY` and `PROXYDECK_ADMIN_PASSWORD`
3. Run:

```bash
docker compose up --build
```

Compose starts:

- `frontend` on `5173`
- `backend` on `8080` and `20000`
- `redis` on the internal Docker network only

## Proxy Username Format

The proxy username supports request-time filters:

```text
{uid}__region=SG__city=Singapore__asn=AS20473__sid=abc123
```

Example:

```bash
curl -x "http://user001__region=SG__sid=demo:pass123@127.0.0.1:20000" \
  https://ipinfo.io/json
```

Supported fields:

- `region`
- `city`
- `asn`
- `isp`
- `sid`

## Configuration

Start from [`backend/configs/config.example.yaml`](backend/configs/config.example.yaml) and keep your real values in an untracked local file such as `backend/configs/config.local.yaml`.

The backend also supports environment overrides such as:

- `PROXYDECK_SQLITE_PATH`
- `PROXYDECK_REDIS_ADDR`
- `PROXYDECK_SECURITY_ENCRYPTION_KEY`
- `PROXYDECK_ADMIN_USERNAME`
- `PROXYDECK_ADMIN_PASSWORD`

The backend refuses to start when sample placeholder secrets are still present.

## Validation

```bash
go test ./backend/...
cd frontend && npm run build
```

## Documentation

- Setup and deployment notes: [`docs/setup.md`](docs/setup.md)
- SQL migrations: [`backend/migrations/README.md`](backend/migrations/README.md)
- Chinese setup notes: [`docs/setup.zh-CN.md`](docs/setup.zh-CN.md)
- Chinese migration notes: [`backend/migrations/README.zh-CN.md`](backend/migrations/README.zh-CN.md)

## License

This project is licensed under the MIT License. See [`LICENSE`](LICENSE).
