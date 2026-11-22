# PPanel 服务端

<div align="center">

[![License](https://img.shields.io/github/license/perfect-panel/backend)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.27.4%2B-blue)](go.mod)
[![Go Report Card](https://goreportcard.com/badge/github.com/perfect-panel/backend)](https://goreportcard.com/report/github.com/perfect-panel/backend)
[![Docker](https://img.shields.io/badge/Docker-Available-blue)](Dockerfile)
[![CI](https://img.shields.io/github/actions/workflow/status/perfect-panel/backend/ci.yml?branch=main&label=CI)](.github/workflows/ci.yml)

**PPanel 是一个纯净、专业、完美的开源代理面板工具，旨在成为您学习和实际使用的理想选择。**

[中文](README_ZH.md) | [English](README.md) | [报告问题](https://github.com/perfect-panel/backend/issues/new) | [功能请求](https://github.com/perfect-panel/backend/issues/new)

</div>

> **第一条**  
> 人人生而自由，在尊严与权利上一律平等。  
> 他们赋有理性与良知，应当以兄弟般的精神彼此相待。  
>
> **第十二条**  
> 任何人的隐私、家庭、住宅和通信不得任意干涉，其名誉与荣誉不得加以攻击。  
> 人人有权受到法律的保护，以免遭受这种干涉或攻击。  
>
> **第十九条**  
> 人人有思想与表达的自由；此项自由包括持有主张而不受干预，以及通过任何媒介、无论国界，自由寻求、接受和传播信息与思想。  
>
> *来源： [United Nations – Universal Declaration of Human Rights (UN.org)](https://www.un.org/sites/un2.un.org/files/2021/03/udhr.pdf)*

## 📋 概述

PPanel 服务端是 PPanel 项目的后端组件，为代理服务提供强大的 API 和核心功能。它基于 Go 语言开发，注重性能、安全性和可扩展性。

### 核心特性

- **节点协议**覆盖 Shadowsocks、VLESS、Hysteria 2 等 12 种协议，详见[节点 API 兼容表](#节点-api)。
- **登录历史**记录登录方式、客户端 IP、User-Agent、时间和结果，供账户审计。
- **极简设计**：简单易用，保留完整的业务逻辑。
- **用户管理**：完善的认证和授权系统。
- **订阅管理**：处理用户订阅和服务开通。
- **支付方式**包括 Stripe、支付宝当面付、EPay V1/V2、Cryptomus、GMPay、PayPal Checkout 和站内余额。配置指南见 [EPay](docs/guide/epay-zh.md)、[GMPay](docs/guide/gmpay-zh.md) 和 [PayPal](docs/guide/paypal-zh.md)。
- **订单管理**：跟踪和处理用户订单。
- **工单系统**：内置客户支持和问题跟踪。
- **节点管理**：监控和控制服务器节点。
- **API 框架**：提供全面的 RESTful API，供前端集成。

## 🚀 快速开始

### 前提条件

- 从源码构建需要 **Go 1.27.4 或更高版本**。
- 数据库支持 **PostgreSQL 16+**（[配置模板](etc/ppanel.yaml)使用 PostgreSQL）、**MySQL 8.0+** 或 **MariaDB 11.8+**。
- 缓存和任务队列可使用 **Redis 6.0+**，或 **Dragonfly v2.1 单实例**。使用 Dragonfly 时加上启动参数 `--cluster_mode=emulated --lock_on_hashtags`。ACL 用户和项目前缀见 [Redis 配置指南](docs/guide/config-zh.md#37-redis-配置-redis)。
- **Docker**：可选，用于容器化部署
- **Git**：用于克隆仓库

不想自行构建、直接部署发布的二进制？请看[安装指南](docs/guide/install-zh.md)（`script/install.sh`、systemd 服务、校验和验证）和[配置指南](docs/guide/config-zh.md)。

### 通过源代码运行

1. **克隆仓库**：
   ```bash
   git clone https://github.com/perfect-panel/backend.git
   cd backend
   ```

2. **安装依赖**：
   ```bash
   go mod download
   ```

3. **构建项目**（二进制输出到 `bin/`；请选择将要运行它的机器对应的目标）：
   ```bash
   make linux-amd64
   ```

4. **启动服务器**：
   ```bash
   ./bin/ppanel-server-linux-amd64 run --config etc/ppanel.yaml
   ```
   未配置数据库地址且未设置连接环境变量时，首次启动会在 `127.0.0.1` 的配置端口 `Port` 上提供安装向导（默认 `http://127.0.0.1:8080/init`），只能从本机访问。远程主机请建立 SSH 隧道
   （`ssh -L 8080:127.0.0.1:8080 user@host`），或设置 `PPANEL_DB` 与 `PPANEL_REDIS` 进行无人值守安装；两种方式都在
   [安装指南](docs/guide/install-zh.md#4-首次启动)中说明。

### 🐳 Docker 部署

镜像以非特权用户（uid 65532）运行，监听 8080 端口，读取 `/app/etc/ppanel.yaml`，并通过 `ppanel healthcheck`（`HEALTHCHECK`）报告健康状态。
**首次运行的安装向导在容器内只监听 `127.0.0.1`，无法通过映射的端口访问**，因此容器部署必须用以下两种方式之一提供连接信息：

- 设置 `PPANEL_DB` 与 `PPANEL_REDIS`：首次启动时服务用它们和生成的 JWT 密钥补全空的配置文件，执行迁移并创建首位管理员，其密码会在
  `docker logs` 中打印一次；或
- 挂载预先填好的 `etc/ppanel.yaml`（见[配置指南](docs/guide/config-zh.md)）。

| 变量 | 格式 | 示例 |
|---|---|---|
| `PPANEL_DB` | MySQL DSN `user:password@tcp(host:port)/dbname`，或 URL：`mysql://…`、`postgres://user:password@host:5432/dbname?sslmode=require` | `postgres://ppanel:secret@db.internal:5432/ppanel?sslmode=require` |
| `PPANEL_REDIS` | `redis://[username:password@]host[:port][/db][?prefix=project]` | `redis://panel:secret@redis.internal:6379/0?prefix=panel-a` |

1. **构建 Docker 镜像**：
   ```bash
   docker buildx build --platform linux/amd64 --load -t ppanel-server:latest .
   ```

2. **运行容器**。挂载的配置文件必须存在，且在首次启动（服务补全配置）时对 uid 65532 可写（请把数据库与 Redis 地址换成自己的）：
   ```bash
   mkdir -p etc && touch etc/ppanel.yaml && sudo chown 65532:65532 etc/ppanel.yaml
   docker run -d --name ppanel-server -p 8080:8080 \
     -e PPANEL_DB='postgres://ppanel:secret@db.internal:5432/ppanel?sslmode=require' \
     -e PPANEL_REDIS='redis://:secret@redis.internal:6379/0' \
     -v "$(pwd)/etc/ppanel.yaml:/app/etc/ppanel.yaml" \
     ppanel-server:latest
   docker logs -f ppanel-server   # 首位管理员的密码只打印一次
   ```

3. **使用 Docker Compose**。仓库中的 [`docker-compose.yml`](docker-compose.yml) 构建镜像、发布 8080 端口、挂载 `./etc/ppanel.yaml`
   并检查容器健康状态：
   ```yaml
   services:
     ppanel:
       container_name: ppanel-server
       build:
         context: .
         dockerfile: Dockerfile
       ports:
         - "8080:8080"
       volumes:
         - ./etc/ppanel.yaml:/app/etc/ppanel.yaml
       # environment:
       #   PPANEL_DB: "ppanel:password@tcp(db:3306)/ppanel"
       #   PPANEL_REDIS: "redis://:password@redis:6379/0"
       healthcheck:
         test: ["CMD", "/app/ppanel", "healthcheck"]
         interval: 30s
         timeout: 5s
         retries: 3
         start_period: 30s
       stop_grace_period: 20s
       restart: always
   ```
   首次 `docker compose up` 之前，`./etc/ppanel.yaml` 必须以**文件**形式存在（否则 Docker 会创建同名目录），且对 uid 65532 可写；
   取消 `environment` 的注释或预先填好文件，然后：
   ```bash
   mkdir -p etc && touch etc/ppanel.yaml && sudo chown 65532:65532 etc/ppanel.yaml
   docker compose up -d
   ```
   `stop_grace_period` 为优雅停机（先排空 HTTP，再依次停止调度器、任务 worker 和链路追踪导出，最长约 18 秒）留出时间；直接用 `docker run`
   启动的容器请用 `docker stop --time 20` 停止。

4. **拉取已发布的镜像**。以下规则适用于新发布的镜像。每次发布后，Docker Hub 和 GHCR 上同名标签的摘要一致。

   | 触发条件 | 两个镜像仓库的标签 |
   |---|---|
   | 推送到 `dev` | `nightly`、`nightly-<短 SHA>` |
   | 推送 Beta 标签，例如 `v2.0.0-beta.1` | `beta`、`2.0.0-beta.1` |
   | 给 `main` 历史中的提交推送正式标签，例如 `v2.0.0` | `latest`、`2.0.0`、`2.0`、`2.0.0-<短 SHA>` |

   普通推送到 `main` 运行 CI。`lts` 固定为 1.21.7。正式版和 Beta 镜像支持 Linux amd64 和 arm64。Nightly 镜像支持 Linux amd64，Linux arm64 二进制可从滚动 GitHub 预发布下载。

   ```bash
   docker pull ppanel/ppanel-server:latest
   docker pull ghcr.io/perfect-panel/ppanel-server:latest
   ```

服务提供 `GET /healthz`（存活探针）与 `GET /readyz`（就绪探针：数据库与 Redis 可达）供监控和负载均衡使用，见
[配置指南](docs/guide/config-zh.md#5-健康检查)。

## 📖 API 文档

API 文档使用 Handler 上的 Swaggo 注解生成，并通过 Hertz 实际注册路由进行完整性校验。根目录的 `ppanel.json` 是完整 Swagger 2.0 文档：

[ppanel.json](./ppanel.json)

### 节点 API

节点通过 `GET /v2/server/{server_id}` 获取已启用协议的完整配置。旧版配置接口返回的字段较少。节点程序和用户使用的代理客户端也需要支持所选协议。

| 配置接口 | 节点协议 |
| --- | --- |
| V2 和旧版 `GET /v1/server/config` | Shadowsocks（含 SS2022）、VMess、VLESS、Trojan、AnyTLS、TUIC v5、Hysteria 2 |
| 仅 V2 | ShadowsocksR（SSR）、Mieru、Naive、Snell v5/v6、Nowhere v3（NW3） |

节点运行仍依赖 `/v1/server/*`。在此前缀下，节点通过 GET 读取 `/user`（用户列表）和 `/alivelist`（每个订阅的在线 IP 数），通过 POST 调用 `/device` 申请设备名额，并向 `/push`、`/online`、`/status` 分别上报流量、在线用户和节点状态。

### 更新 API 文档

修改路由、请求 DTO 或响应 DTO 后，运行：

```bash
./script/generate-swagger.sh
go test ./internal/transport/http/routes -run '^TestSwagger' -count=1
```

`main` 分支的 GitHub Actions 会生成 `ppanel.json` 以及 `admin.json`、`user.json`、`common.json`、`node.json`、`edge.json` 分类文档，并同步到
[`perfect-panel/frontend`](https://github.com/perfect-panel/frontend) 的 `docs/public/swagger` 目录。`GH_TOKEN` secret 需要对该仓库具有 Contents 写权限。

## 🔗 相关项目

| 项目               | 描述           | 链接                                                    |
|------------------|--------------|-------------------------------------------------------|
| PPanel Web       | PPanel 前端应用  | [GitHub](https://github.com/perfect-panel/frontend) |
| ppanel-node | Go 节点服务端 | [GitHub](https://github.com/perfect-panel/ppanel-node) |
| Terraris | Rust 节点服务端 | [GitHub](https://github.com/perfect-panel/Terraris) |
| PPanel User Web  | PPanel 用户界面  | [预览](https://user.ppanel.dev)                         |
| PPanel Admin Web | PPanel 管理员界面 | [预览](https://admin.ppanel.dev)                        |

## 🌐 官方网站

访问 [ppanel.dev](https://ppanel.dev) 获取更多信息。

## 🏛 系统架构

![Architecture Diagram](./docs/image/architecture-zh.png)

## 📁 目录结构

```
.
├── cmd/              # 应用程序入口
├── docs/             # 文档（使用指南、贡献指南、设计文档）
├── etc/              # 配置文件（如 ppanel.yaml）
├── internal/         # 内部模块
│   ├── app/          # 应用组装、初始化、迁移与定时调度
│   ├── arch/         # 架构边界检查
│   ├── auth/         # 跨模块认证能力
│   ├── config/       # 配置文件解析
│   ├── infra/        # 邮件、短信、共享任务消息等基础设施
│   ├── module/       # 业务模块（门面、contract、transport/http 与内部实现）
│   ├── repository/   # 仓储契约与事务组装
│   └── transport/    # HTTP、WebSocket 与任务消费
├── pkg/              # 公共工具代码
├── script/           # 安装脚本与 CI 辅助脚本
├── scripts/          # 性能测试与维护脚本
├── go.mod            # Go 模块定义
├── Makefile          # 构建自动化
└── Dockerfile        # Docker 配置
```

## 💻 开发

### 多平台构建

使用 `Makefile` 构建多种平台（如 Linux、Windows、macOS）：

```bash
make all  # 构建 linux-amd64、darwin-amd64、windows-amd64
make linux-arm64  # 构建特定平台
```

所有二进制都是静态链接的（`CGO_ENABLED=0 -trimpath`），并注入 `script/ldflags.sh` 提供的版本信息。发布会为以下平台提供
`ppanel-server-<os>-<arch>.tar.gz`（Windows 为 `.zip`）以及 `SHA256SUMS` 文件：

- Linux：`386`、`amd64`、`arm64`（另有 `linux/amd64`、`linux/arm64` 容器镜像）
- Windows：`386`、`amd64`、`arm64`
- macOS：`amd64`、`arm64`

`make` 还可以构建 `linux-amd64-v3`、`linux-armv5`、`linux-armv6`、`linux-armv7`、`windows-amd64-v3`、`windows-armv7` 和 `darwin-amd64-v3`。

## 🔒 安全

请通过 [GitHub Security Advisories](https://github.com/perfect-panel/backend/security/advisories/new) 私下报告安全漏洞，不要在公开 issue 中提交。
[SECURITY.md](SECURITY.md) 列出了受支持的版本以及处理流程。

## 🤝 贡献

欢迎各种贡献，包括功能开发、错误修复和文档改进。请查看[贡献指南](docs/contributing/CONTRIBUTING_ZH.md)了解详情。

## ✨ 特别感谢

感谢以下优秀的开源项目，它们为本项目的开发提供了强大的支持！ 🚀

<div style="overflow-x: auto;">
<table style="width: 100%; border-collapse: collapse; margin: 20px 0;">
  <thead>
    <tr style="background-color: #f5f5f5;">
      <th style="padding: 10px; text-align: center;">项目</th>
      <th style="padding: 10px; text-align: left;">描述</th>
      <th style="padding: 10px; text-align: center;">项目</th>
      <th style="padding: 10px; text-align: left;">描述</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td align="center" style="padding: 15px; vertical-align: middle;">
        <a href="https://www.cloudwego.io/zh/docs/hertz/" style="text-decoration: none;">
          <strong>Hertz</strong><br/>
          <img src="https://img.shields.io/github/stars/cloudwego/hertz?style=social" alt="Hertz Stars" />
        </a>
      </td>
      <td style="padding: 15px; vertical-align: middle;">
        高性能的 Go HTTP 框架<br/>
      </td>
      <td align="center" style="padding: 15px; vertical-align: middle;">
        <a href="https://gorm.io/" style="text-decoration: none;">
          <img src="https://gorm.io/gorm.svg" width="50" alt="Gorm" style="border-radius: 8px;" /><br/>
          <strong>Gorm</strong><br/>
          <img src="https://img.shields.io/github/stars/go-gorm/gorm?style=social" alt="Gorm Stars" />
        </a>
      </td>
      <td style="padding: 15px; vertical-align: middle;">
        功能强大的 Go ORM 框架<br/>
      </td>
    </tr>
    <tr>
      <td align="center" style="padding: 15px; vertical-align: middle;">
        <a href="https://github.com/hibiken/asynq" style="text-decoration: none;">
          <img src="https://user-images.githubusercontent.com/11155743/114697792-ffbfa580-9d26-11eb-8e5b-33bef69476dc.png" width="50" alt="Asynq" style="border-radius: 8px;" /><br/>
          <strong>Asynq</strong><br/>
          <img src="https://img.shields.io/github/stars/hibiken/asynq?style=social" alt="Asynq Stars" />
        </a>
      </td>
      <td style="padding: 15px; vertical-align: middle;">
        Go 语言的异步任务队列<br/>
      </td>
      <td align="center" style="padding: 15px; vertical-align: middle;">
        <a href="https://goswagger.io/" style="text-decoration: none;">
          <img src="https://goswagger.io/go-swagger/logo.png" width="30" alt="Go-Swagger" style="border-radius: 8px;" /><br/>
          <strong>Go-Swagger</strong><br/>
          <img src="https://img.shields.io/github/stars/go-swagger/go-swagger?style=social" alt="Go-Swagger Stars" />
        </a>
      </td>
      <td style="padding: 15px; vertical-align: middle;">
        完整的 Go Swagger 工具集<br/>
      </td>
    </tr>
  </tbody>
</table>
</div>

---

🎉 **致敬开源**：感谢开源社区，让开发变得更简单、更高效！欢迎为这些项目点亮 ⭐，支持开源事业！

## 📄 许可证

本项目采用 [GPL-3.0 许可证](LICENSE) 授权。
