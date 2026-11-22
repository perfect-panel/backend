# Pull Request 提交须知

为了确保代码库的质量和项目的可维护性，在提交 Pull Request（PR）之前，请务必遵循以下准则：

## 1. PR 标题和描述

- **标题清晰**：简明扼要地描述 PR 的主要内容，例如：
    - Fix: 修复用户登录时的错误提示
    - Feature: 添加订单导出功能

- **描述详细**：在描述中包括以下内容：
    - 此 PR 的目的和背景。
    - 变更的详细说明。
    - 如果涉及 Bug 修复，需描述问题重现的步骤。
    - 如果涉及新功能，需描述其使用方式。
    - 关联的 Issue（如有），使用关键字关闭 Issue，例如：`Closes #123`。

## 2. 提交代码前的检查

- **代码风格**：确保代码符合项目的代码规范。
- **功能测试**：对新功能或 Bug 修复进行全面测试，确保没有功能缺失或回归问题。
- **单元测试**：为新增或修改的功能编写单元测试，并确保所有测试通过(工具类即`pkg/*`下面的必须带有单元测试)。
- **文档更新**：如果 PR 涉及新功能或接口更改，确保文档同步更新。
- **文件命名**：Go 文件与目录一律使用小写 snake_case（`update_server_handler.go`，而不是
  `updateServerHandler.go`），其他写法会让 `internal/arch` 测试失败。

### 本地检查

Makefile 执行与 CI 相同的检查。所需的 Go 工具（goimports、golangci-lint）在首次使用时按 Makefile
固定的版本安装到 `bin/tools`（已被 git 忽略）；govulncheck 通过 `go run` 以固定版本运行，只存在于 Go
模块缓存中。不会做任何全局安装。

| 命令 | 作用 |
|---|---|
| `make check` | 格式检查、`go vet`、golangci-lint、单元测试。推送前请运行。 |
| `make fmt` / `make fmt-check` | 改写 / 检查格式（`gofmt` 与 `goimports`）。 |
| `make vet` | `go vet ./...` |
| `make lint` | 对整个代码树运行 golangci-lint，与 CI 一致：有任何问题都会失败。 |
| `make lint-new LINT_BASE=origin/dev` | golangci-lint，只报告 `LINT_BASE`（默认 `HEAD`，即未提交的改动）之后改动的行；pre-commit 钩子用它做快速检查。 |
| `make test` / `make test-race` | 单元测试，可选开启竞态检测。 |
| `make vulncheck` | 用 govulncheck 检查可达的已知漏洞。 |
| `make proto` / `make proto-check` | 重新生成 `api/**/*.pb.go` / 提交的文件与生成结果不一致时失败。需要 `PATH` 上有 protoc 21.12（显示为 `libprotoc 3.21.12`）；protoc-gen-go 按 `go.mod` 要求的版本构建。 |
| `make linux-amd64`（及其他平台目标） | 构建发布二进制。`VERSION`、`CHANNEL`（`stable`、`beta`、`nightly`、`dev`）与 `BUILD_TIME`（UTC，`YYYY-MM-DDTHH:MM:SSZ`，默认当前时间）决定注入的构建信息；`make ldflags` 打印对应参数。 |

依赖数据库的测试在未设置 DSN 时跳过，CI 的设置方式如下：

```sh
PPANEL_TEST_MYSQL_DSN='root:mysql@tcp(127.0.0.1:3306)/ppanel?charset=utf8mb4&parseTime=true&loc=UTC&interpolateParams=true' \
PPANEL_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:5432/ppanel?sslmode=disable' \
go test ./...
```

新增、删除或调整 HTTP 路由时，刷新路由清单并审查其差异：

```sh
go test ./internal/transport/http/routes -run TestRegisterHandlers_routeInventory -update
```

### Git hooks

提交时由 [lefthook](https://github.com/evilmartians/lefthook) 执行检查。先安装一次 lefthook 1.10 或更新版本
（例如 `brew install lefthook` 或 `go install github.com/evilmartians/lefthook/v2@latest`），
再在本地仓库中启用：

```sh
lefthook install
```

- **pre-commit**：用 goimports 格式化暂存的 Go 文件并重新暂存，然后运行只针对改动行的
  golangci-lint、`go vet` 与单元测试。
- **commit-msg**：用 commitlint 按 [Conventional Commits](https://www.conventionalcommits.org)
  规则检查提交信息。仓库没有 `package.json`，该 hook 通过 `npx` 运行固定版本的 commitlint，
  因此 `PATH` 上需要 Node.js 22.12 或更新版本。

## 3. 分支策略

- **正确的分支**：
    - 新功能应基于 `feature/*` 分支进行开发。
    - Bug 修复应基于 `fix/*` 分支。
    - 确保 PR 的目标分支与项目的分支策略一致。

- **同步主干代码**：在提交 PR 之前，请确保分支已经与目标分支（`main` 或 `dev`）同步。

## 4. 审查流程

- **小型提交**：避免一次性提交过多的更改，将 PR 拆分为更小的逻辑单元。

---

感谢您的贡献！



