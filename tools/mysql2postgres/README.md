# mysql2postgres

用于将现有 PPanel MySQL 部署一次性迁移到 PostgreSQL 的数据迁移工具。

源 MySQL/MariaDB 库与目标 PostgreSQL 库应先完成同一版本的 PPanel schema 迁移。工具从两端读取实际表、列和主键，再用 PostgreSQL COPY 复制数据。

## 迁移流程

1. 停止 PPanel server、queue、scheduler，以及可能写入流量数据的节点。
2. 备份 MySQL 数据库。
3. 创建一个空的 PostgreSQL 数据库。
4. 使用与源库相同版本的 PPanel 和单独的 PostgreSQL 配置初始化目标库，然后停止该实例。
5. 指定源、目标 DSN 运行 `--dry-run` 并核对表名和行数。确认目标库中的初始化数据可以替换后，再使用 `--truncate --yes` 执行复制。
6. 核对迁移结果，将面板配置的 `Database.Driver` 设为 `postgres`，填写目标库的连接信息，然后启动 PPanel。迁移命令的连接来自 `--mysql` 和 `--postgres`；编辑 `etc/ppanel.yaml` 只影响面板启动。

## 使用方式

推荐使用主程序内置命令：

```bash
./ppanel-server migrate mysql2postgres \
  --mysql 'user:pass@tcp(127.0.0.1:3306)/ppanel?charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai' \
  --postgres 'postgres://ppanel:pass@127.0.0.1:5432/ppanel?sslmode=prefer' \
  --truncate \
  --yes
```

开发调试用 wrapper：

```bash
go run ./tools/mysql2postgres \
  --mysql 'user:pass@tcp(127.0.0.1:3306)/ppanel?charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai' \
  --postgres 'postgres://ppanel:pass@127.0.0.1:5432/ppanel?sslmode=prefer' \
  --truncate \
  --yes
```

建议先 dry-run 查看迁移计划：

```bash
go run ./tools/mysql2postgres \
  --mysql 'user:pass@tcp(127.0.0.1:3306)/ppanel?charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai' \
  --postgres 'postgres://ppanel:pass@127.0.0.1:5432/ppanel?sslmode=prefer' \
  --dry-run
```

## 参数

- `--mysql`：源 MySQL DSN。
- `--postgres`：目标 PostgreSQL DSN。
- `--location`：MySQL 中 DATETIME 值所在的 IANA 时区，即面板的 `AppLocation`（默认 `Asia/Shanghai`）。`--mysql` 未带 `loc` 参数时按此时区读取；以文本形式读到的时间也按此时区解析，与运行迁移工具的机器时区无关。带 `loc` 的 DSN 保留自己的时区。
- `--schema`：PostgreSQL schema，默认是 `public`。
- `--truncate`：复制前清空计划中的目标表。如果计划外的表通过外键引用这些表，PostgreSQL 会拒绝整条清空操作。
- `--yes`：使用 `--truncate` 时必须传入，用于确认破坏性操作。
- `--tables`：逗号分隔的表白名单。
- `--exclude`：逗号分隔的表黑名单。
- `--batch-size`：进度日志输出间隔。
- `--dry-run`：只打印迁移计划，不复制数据。

## 注意事项

- `schema_migrations` 永远不会被复制。
- 只复制源库和目标库同时存在的表与列。PostgreSQL 新增列会保留默认值。
- 表会按照 PostgreSQL 外键依赖顺序复制；如果 MySQL 表存在主键，读取时会按主键排序。
- 复制完成后只校准已复制列的 PostgreSQL 序列。仅存在于目标库的自增列在插入时沿用自身序列。
- 数据按表提交。复制中途报错时，已成功提交的表会保留，切换面板前应核对并完成剩余迁移。
- 建议使用全新的 PostgreSQL 数据库。如果不使用 `--truncate`，可能因为 migrations 已插入默认数据而触发唯一约束错误。
