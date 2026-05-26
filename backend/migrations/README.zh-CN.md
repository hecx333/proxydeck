# Migration 说明

这个目录保存与当前运行时行为对齐的 SQLite schema SQL 资产。

## 目的

- 给部署和验收提供可直接审阅的 SQL 基线
- 保留当前由运行时初始化补上的 schema 修复语句
- 为后续切换到显式 migration 工具预留基础

## 当前文件

- `001_initial_schema.sql`
  当前应用模型对应的基础表结构和索引
- `002_runtime_fixes.sql`
  需要和线上 schema 保持一致的运行时修复，包括：
  - `proxy_nodes(protocol, host, port)` 唯一索引
  - 流量和请求计数字段的空值回填

## 事实来源

`backend/internal/db/db.go` 仍然是当前运行时创建和修复 schema 的实际入口。

这里的 SQL 文件属于参考资产，需要和下面两部分保持同步：

- GORM `AutoMigrate`
- 启动阶段的 `db.Exec(...)` 修复逻辑

如果模型字段或运行时修复语句变了，这个目录也要在同一个改动里一起更新。
