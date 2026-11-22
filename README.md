# PPanel Server

<div align="center">

[![License](https://img.shields.io/github/license/perfect-panel/backend)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.27.4%2B-blue)](go.mod)
[![Go Report Card](https://goreportcard.com/badge/github.com/perfect-panel/backend)](https://goreportcard.com/report/github.com/perfect-panel/backend)
[![Docker](https://img.shields.io/badge/Docker-Available-blue)](Dockerfile)
[![CI](https://img.shields.io/github/actions/workflow/status/perfect-panel/backend/ci.yml?branch=main&label=CI)](.github/workflows/ci.yml)

**PPanel is a pure, professional, and perfect open-source proxy panel tool, designed for learning and practical use.**

[English](README.md) | [中文](README_ZH.md) | [Report Bug](https://github.com/perfect-panel/backend/issues/new) | [Request Feature](https://github.com/perfect-panel/backend/issues/new)

</div>

> **Article 1.**  
> All human beings are born free and equal in dignity and rights.  
> They are endowed with reason and conscience and should act towards one another in a spirit of brotherhood.
>
> **Article 12.**  
> No one shall be subjected to arbitrary interference with his privacy, family, home or correspondence, nor to attacks upon his honour and reputation.  
> Everyone has the right to the protection of the law against such interference or attacks.
>
> **Article 19.**  
> Everyone has the right to freedom of opinion and expression; this right includes freedom to hold opinions without interference and to seek, receive and impart information and ideas through any media and regardless of frontiers.
>
> *Source: [United Nations – Universal Declaration of Human Rights (UN.org)](https://www.un.org/sites/un2.un.org/files/2021/03/udhr.pdf)*

## 📋 Overview

PPanel Server is the backend component of the PPanel project, providing robust APIs and core functionality for managing
proxy services. Built with Go, it emphasizes performance, security, and scalability.

### Key Features

- PPanel supports **12 node protocols**, including Shadowsocks, VLESS and Hysteria 2. See the [node API compatibility table](#node-api).
- **Login history** records the sign-in method, client IP, user agent, time and result for account auditing.
- **Minimalist Design**: Simple yet powerful, with complete business logic.
- **User Management**: Full authentication and authorization system.
- **Subscription System**: Manage user subscriptions and service provisioning.
- **Payments** support Stripe, Alipay F2F, EPay V1/V2, Cryptomus, GMPay and PayPal Checkout, plus the built-in wallet balance. Setup guides cover [EPay](docs/guide/epay.md), [GMPay](docs/guide/gmpay.md) and [PayPal](docs/guide/paypal.md).
- **Order Management**: Track and process user orders.
- **Ticket System**: Built-in customer support and issue tracking.
- **Node Management**: Monitor and control server nodes.
- **API Framework**: Comprehensive RESTful APIs for frontend integration.

## 🚀 Quick Start

### Prerequisites

- Building from source requires **Go 1.27.4 or later**.
- Choose **PostgreSQL 16+** (the [configuration template](etc/ppanel.yaml) uses PostgreSQL), **MySQL 8.0+** or **MariaDB 11.8+**.
- For caching and task queues, use **Redis 6.0+** or a **single Dragonfly v2.1 instance** started with `--cluster_mode=emulated --lock_on_hashtags`. See the [Redis configuration guide](docs/guide/config.md#37-redis-redis) for ACL users and per-project key prefixes.
- **Docker**: Optional, for containerized deployment
- **Git**: For cloning the repository

Deploying a release binary instead of building? See the [installation guide](docs/guide/install.md)
(`script/install.sh`, systemd unit, checksum verification) and the [configuration guide](docs/guide/config.md).

### Installation from Source

1. **Clone the repository**:
   ```bash
   git clone https://github.com/perfect-panel/backend.git
   cd backend
   ```

2. **Install dependencies**:
   ```bash
   go mod download
   ```

3. **Build the project** (the binaries land in `bin/`; pick the target of the machine that will run it):
   ```bash
   make linux-amd64
   ```

4. **Run the server**:
   ```bash
   ./bin/ppanel-server-linux-amd64 run --config etc/ppanel.yaml
   ```
   When the database address and connection environment variables are unset, the first start serves the setup wizard on `127.0.0.1` at the configured
   `Port` (`http://127.0.0.1:8080/init` by default), reachable from the same machine only. On a remote host open an SSH tunnel
   (`ssh -L 8080:127.0.0.1:8080 user@host`) or set `PPANEL_DB` and `PPANEL_REDIS` for a non-interactive
   installation; both are described in the [installation guide](docs/guide/install.md#4-first-start).

### 🐳 Docker Deployment

The image runs as an unprivileged user (uid 65532), listens on port 8080, reads `/app/etc/ppanel.yaml` and reports its
health through `ppanel healthcheck` (`HEALTHCHECK`). **The first-run setup wizard listens on `127.0.0.1` inside the
container and cannot be reached through a published port**, so a container deployment provides the connections in
one of two ways:

- set `PPANEL_DB` and `PPANEL_REDIS`: on the first start the server completes the empty configuration file with them
  and a generated JWT secret, applies the migrations and creates the first administrator, whose password is printed
  once in `docker logs`; or
- mount a pre-filled `etc/ppanel.yaml` ([configuration guide](docs/guide/config.md)).

| Variable | Format | Example |
|---|---|---|
| `PPANEL_DB` | MySQL DSN `user:password@tcp(host:port)/dbname`, or a URL: `mysql://…`, `postgres://user:password@host:5432/dbname?sslmode=require` | `postgres://ppanel:secret@db.internal:5432/ppanel?sslmode=require` |
| `PPANEL_REDIS` | `redis://[username:password@]host[:port][/db][?prefix=project]` | `redis://panel:secret@redis.internal:6379/0?prefix=panel-a` |

1. **Build the Docker image**:
   ```bash
   docker buildx build --platform linux/amd64 --load -t ppanel-server:latest .
   ```

2. **Run the container**. The mounted configuration file must exist and be writable by uid 65532 on the first
   start, when the server completes it (replace the database and Redis addresses with yours):
   ```bash
   mkdir -p etc && touch etc/ppanel.yaml && sudo chown 65532:65532 etc/ppanel.yaml
   docker run -d --name ppanel-server -p 8080:8080 \
     -e PPANEL_DB='postgres://ppanel:secret@db.internal:5432/ppanel?sslmode=require' \
     -e PPANEL_REDIS='redis://:secret@redis.internal:6379/0' \
     -v "$(pwd)/etc/ppanel.yaml:/app/etc/ppanel.yaml" \
     ppanel-server:latest
   docker logs -f ppanel-server   # prints the first administrator's password once
   ```

3. **Use Docker Compose**. The repository's [`docker-compose.yml`](docker-compose.yml) builds the image, publishes
   port 8080, mounts `./etc/ppanel.yaml` and checks the container's health:
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
   `./etc/ppanel.yaml` must exist as a **file** before the first `docker compose up` (Docker creates a directory of
   that name otherwise) and be writable by uid 65532; uncomment `environment` or pre-fill the file, then:
   ```bash
   mkdir -p etc && touch etc/ppanel.yaml && sudo chown 65532:65532 etc/ppanel.yaml
   docker compose up -d
   ```
   `stop_grace_period` gives the graceful shutdown (HTTP first, then the scheduler, the task worker and the trace
   exporter, up to about 18 s) the time it needs; with plain `docker run`, stop with `docker stop --time 20`.

4. **Pull a published image** from Docker Hub or GHCR. New releases publish the same manifest to both registries under these tags.

   | Trigger | Image tags in both registries |
   |---|---|
   | Push to `dev` | `nightly`, `nightly-<short-sha>` |
   | Push a beta tag such as `v2.0.0-beta.1` | `beta`, `2.0.0-beta.1` |
   | Push a stable tag such as `v2.0.0` on a commit in `main` | `latest`, `2.0.0`, `2.0`, `2.0.0-<short-sha>` |

   CI runs on pushes to `main`. The `lts` tag stays pinned to 1.21.7. Stable and beta images support Linux amd64 and arm64. Nightly images support Linux amd64, with Linux arm64 binaries available in the rolling GitHub prerelease.

   ```bash
   docker pull ppanel/ppanel-server:latest
   docker pull ghcr.io/perfect-panel/ppanel-server:latest
   ```

The server answers `GET /healthz` (liveness) and `GET /readyz` (readiness: database and Redis reachable) for
monitoring and load balancers; see the [configuration guide](docs/guide/config.md#5-health-checks).

## 📖 API Documentation

API documentation is generated from Swaggo annotations on the handlers and checked against the routes actually registered by Hertz. The root `ppanel.json` is the complete Swagger 2.0 document:

[ppanel.json](ppanel.json)

### Node API

Nodes fetch the full configuration of their enabled protocols with `GET /v2/server/{server_id}`. The legacy configuration endpoint returns a smaller set of fields. The node software and the user’s proxy client must support the selected protocol.

| Configuration endpoint | Node protocols |
| --- | --- |
| V2 and legacy `GET /v1/server/config` | Shadowsocks (including SS2022), VMess, VLESS, Trojan, AnyTLS, TUIC v5, Hysteria 2 |
| V2 only | ShadowsocksR (SSR), Mieru, Naive, Snell v5/v6, Nowhere v3 (NW3) |

Keep `/v1/server/*` reachable for node operations. Under this prefix, nodes use GET for `/user` (user list) and `/alivelist` (active IP counts per subscription), and POST for `/device` (device admission), `/push` (traffic), `/online` (online users) and `/status` (node health).

### Updating the API documentation

After changing a route, request DTO, or response DTO, run:

```bash
./script/generate-swagger.sh
go test ./internal/transport/http/routes -run '^TestSwagger' -count=1
```

GitHub Actions on `main` generates the full document plus the `admin.json`, `user.json`, `common.json`, `node.json`, and `edge.json` scopes, then syncs them to `docs/public/swagger` in [`perfect-panel/frontend`](https://github.com/perfect-panel/frontend). The `GH_TOKEN` secret needs Contents write access to that repository.

## 🔗 Related Projects

| Project          | Description                | Link                                                  |
|------------------|----------------------------|-------------------------------------------------------|
| PPanel Web       | Frontend for PPanel        | [GitHub](https://github.com/perfect-panel/frontend) |
| ppanel-node | Go node server | [GitHub](https://github.com/perfect-panel/ppanel-node) |
| Terraris | Rust node server | [GitHub](https://github.com/perfect-panel/Terraris) |
| PPanel User Web  | User interface for PPanel  | [Preview](https://user.ppanel.dev)                    |
| PPanel Admin Web | Admin interface for PPanel | [Preview](https://admin.ppanel.dev)                   |

## 🌐 Official Website

Visit [ppanel.dev](https://ppanel.dev/) for more details.

## 🏛 Architecture

![Architecture Diagram](docs/image/architecture-en.png)

## 📁 Directory Structure

```
.
├── cmd/              # Application entry point
├── docs/             # Documentation
├── etc/              # Configuration files (e.g., ppanel.yaml)
├── internal/         # Internal modules
│   ├── app/          # Assembly, bootstrap, migrations and scheduling
│   ├── arch/         # Architecture boundary checks
│   ├── auth/         # Shared authentication capabilities
│   ├── config/       # Configuration parsing
│   ├── infra/        # Mail, SMS, shared task messages and infrastructure
│   ├── module/       # Business facades, contracts, entities and adapters
│   ├── repository/   # Repository contracts and transaction assembly
│   └── transport/    # HTTP, WebSocket and task consumers
├── pkg/              # Utility code
├── script/           # Installation and CI helper scripts
├── scripts/          # Performance and maintenance scripts
├── go.mod            # Go module definition
├── Makefile          # Build automation
└── Dockerfile        # Docker configuration
```

## 💻 Development

### Build for Multiple Platforms

Use the `Makefile` to build for various platforms (e.g., Linux, Windows, macOS):

```bash
make all  # Builds linux-amd64, darwin-amd64, windows-amd64
make linux-arm64  # Build for specific platform
```

Every binary is static (`CGO_ENABLED=0 -trimpath`), built with the metadata from `script/ldflags.sh`. The release
publishes `ppanel-server-<os>-<arch>.tar.gz` (`.zip` for Windows) with a `SHA256SUMS` file for:

- Linux: `386`, `amd64`, `arm64` (plus the `linux/amd64` and `linux/arm64` container images)
- Windows: `386`, `amd64`, `arm64`
- macOS: `amd64`, `arm64`

`make` additionally builds `linux-amd64-v3`, `linux-armv5`, `linux-armv6`, `linux-armv7`, `windows-amd64-v3`,
`windows-armv7` and `darwin-amd64-v3`.

## 🔒 Security

Report vulnerabilities privately through
[GitHub Security Advisories](https://github.com/perfect-panel/backend/security/advisories/new), not in public
issues. [SECURITY.md](SECURITY.md) lists the supported versions and what to expect.

## 🤝 Contributing

Contributions are welcome! Please follow the [Contribution Guidelines](docs/contributing/CONTRIBUTING.md) for bug fixes, features, or
documentation improvements.

## ✨ Special Thanks

A huge thank you to the following outstanding open-source projects that have provided invaluable support for this
project's development! 🚀

<div style="overflow-x: auto;">
<table style="width: 100%; border-collapse: collapse; margin: 20px 0;">
  <thead>
    <tr style="background-color: #f5f5f5;">
      <th style="padding: 10px; text-align: center;">Project</th>
      <th style="padding: 10px; text-align: left;">Description</th>
      <th style="padding: 10px; text-align: center;">Project</th>
      <th style="padding: 10px; text-align: left;">Description</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td align="center" style="padding: 15px; vertical-align: middle;">
        <a href="https://www.cloudwego.io/docs/hertz/" style="text-decoration: none;">
          <strong>Hertz</strong><br/>
          <img src="https://img.shields.io/github/stars/cloudwego/hertz?style=social" alt="Hertz Stars" />
        </a>
      </td>
      <td style="padding: 15px; vertical-align: middle;">
        High-performance Go HTTP framework<br/>
      </td>
      <td align="center" style="padding: 15px; vertical-align: middle;">
        <a href="https://gorm.io/" style="text-decoration: none;">
          <img src="https://gorm.io/gorm.svg" width="50" alt="Gorm" style="border-radius: 8px;" /><br/>
          <strong>Gorm</strong><br/>
          <img src="https://img.shields.io/github/stars/go-gorm/gorm?style=social" alt="Gorm Stars" />
        </a>
      </td>
      <td style="padding: 15px; vertical-align: middle;">
        Powerful Go ORM framework<br/>
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
        Asynchronous task queue for Go<br/>
      </td>
      <td align="center" style="padding: 15px; vertical-align: middle;">
        <a href="https://goswagger.io/" style="text-decoration: none;">
          <img src="https://goswagger.io/go-swagger/logo.png" width="30" alt="Go-Swagger" style="border-radius: 8px;" /><br/>
          <strong>Go-Swagger</strong><br/>
          <img src="https://img.shields.io/github/stars/go-swagger/go-swagger?style=social" alt="Go-Swagger Stars" />
        </a>
      </td>
      <td style="padding: 15px; vertical-align: middle;">
        Comprehensive Go Swagger toolkit<br/>
      </td>
    </tr>
  </tbody>
</table>
</div>

---

🎉 **Salute to Open Source**: Thank you to the open-source community for making development simpler and more efficient!
Please give these projects a ⭐ to support the open-source movement!

## 📄 License

This project is licensed under the [GPL-3.0 License](LICENSE).
