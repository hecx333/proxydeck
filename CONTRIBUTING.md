# Contributing

## Development Setup

1. Start Redis locally or with Docker Compose.
2. Run the backend:

```bash
go run ./backend/cmd/gateway -config ./backend/configs/config.yaml
```

3. Run the frontend:

```bash
cd frontend
npm install
npm run dev
```

## Before Opening a Pull Request

- Run `go test ./backend/...`
- Run `cd frontend && npm run build`
- Keep changes scoped and documented
- Do not commit secrets, databases, `node_modules`, or build artifacts

## Pull Request Notes

- Describe the user-facing or operational impact
- Mention config, migration, or deployment changes explicitly
- Include screenshots for UI changes when relevant
