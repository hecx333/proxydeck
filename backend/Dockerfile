FROM golang:1.25 AS builder

WORKDIR /app
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=1 GOOS=linux go build -o /proxydeck ./cmd/gateway

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y ca-certificates curl sqlite3 && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /proxydeck /usr/local/bin/proxydeck
COPY backend/configs/config.yaml /app/backend/configs/config.yaml
EXPOSE 8080 20000
CMD ["proxydeck", "-config", "/app/backend/configs/config.yaml"]
