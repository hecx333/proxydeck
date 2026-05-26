# Setup Guide

This document covers the minimal steps required to run ProxyDeck locally or with Docker Compose.

## Prerequisites

- Go 1.25 or newer
- Node.js 20 or newer
- Redis 7 or newer
- Docker and Docker Compose for containerized deployment

## Local Development

### Backend

1. Start Redis on `127.0.0.1:6379`.
2. Copy [`backend/configs/config.example.yaml`](../backend/configs/config.example.yaml) to `backend/configs/config.local.yaml`.
3. Set your own `security.encryption_key` and `admin.password`.
4. Start the gateway:

```bash
go run ./backend/cmd/gateway -config ./backend/configs/config.local.yaml
```

### Frontend

```bash
cd frontend
npm install
npm run dev
```

The Vite dev server proxies `/api`, `/metrics`, and `/healthz` to `http://127.0.0.1:8080`.

## Docker Compose

1. Copy [`.env.example`](../.env.example) to `.env`
2. Set `PROXYDECK_SECURITY_ENCRYPTION_KEY` and `PROXYDECK_ADMIN_PASSWORD`
3. Run:

```bash
docker compose up --build
```

Notes:

- SQLite data is stored under `./data`
- Redis is not exposed to the host by default
- The frontend container proxies API traffic to the backend container

## First Login

- URL: `http://127.0.0.1:5173/login`
- Username: the value from `admin.username`
- Password: the value from `admin.password`

The backend refuses to start if you keep the sample placeholder secret or password.

## Recommended Overrides

For anything beyond local development, override at least:

- `PROXYDECK_SECURITY_ENCRYPTION_KEY`
- `PROXYDECK_ADMIN_USERNAME`
- `PROXYDECK_ADMIN_PASSWORD`
- `PROXYDECK_SQLITE_PATH`
- `PROXYDECK_REDIS_ADDR`

## Build Checks

```bash
go test ./backend/...
cd frontend && npm run build
```

## Operational Endpoints

- Health: `GET /healthz`
- Metrics: `GET /metrics`
- Admin API base: `/api`
