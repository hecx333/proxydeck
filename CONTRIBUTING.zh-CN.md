# 参与贡献

## 开发环境

1. 在本地启动 Redis，或者直接使用 Docker Compose。
2. 启动后端：

```bash
go run ./backend/cmd/gateway -config ./backend/configs/config.yaml
```

3. 启动前端：

```bash
cd frontend
npm install
npm run dev
```

## 提交 PR 前

- 运行 `go test ./backend/...`
- 运行 `cd frontend && npm run build`
- 保持改动范围清晰，并补充必要说明
- 不要提交密钥、数据库文件、`node_modules` 或构建产物

## PR 说明

- 写清楚用户侧或运维侧影响
- 如果涉及配置、migration、部署方式变化，要明确标出
- UI 变更建议附截图
