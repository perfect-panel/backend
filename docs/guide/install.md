# Installation

## Prerequisites

- A database: MySQL 8.0+, MariaDB 11.8+ or PostgreSQL 16+ (the test suite runs against all three in CI).
- Redis 6.0+ (7.x recommended).
- For the service installation below: Linux with systemd, `curl`, `tar` and `sha256sum`. The release archives
  also cover Windows and macOS, where the binary is started the same way (`ppanel-server run --config ...`).

The configuration file is described in [config.md](config.md); the container image in the
[README](../../README.md#-docker-deployment).

## Quick install (Linux, script)

[`script/install.sh`](../../script/install.sh) downloads the latest release for the machine's architecture (amd64 or
arm64), verifies it against the release's `SHA256SUMS`, installs it to `/opt/ppanel-server`, creates the `ppanel`
system user and a systemd unit, and starts the service:

```shell
curl -fsSL https://raw.githubusercontent.com/perfect-panel/backend/main/script/install.sh -o install.sh
less install.sh    # read what you are about to run as root
sudo bash install.sh
```

Running it again upgrades the binary and keeps `etc/`. `PPANEL_VERSION=v1.2.3 sudo -E bash install.sh` installs a
specific release. For a non-interactive first start export `PPANEL_DB` and `PPANEL_REDIS` before running it
(see [First start](#4-first-start)): the script writes them to `/opt/ppanel-server/etc/ppanel.env`, which the
unit loads.

## Manual installation

### 1. Download and verify

Releases are published at <https://github.com/perfect-panel/backend/releases>. Each release carries one archive
per platform, a `.sha256` file next to each archive and one `SHA256SUMS` over all of them:

| Platform | Assets |
|---|---|
| Linux | `ppanel-server-linux-386.tar.gz`, `ppanel-server-linux-amd64.tar.gz`, `ppanel-server-linux-arm64.tar.gz` |
| Windows | `ppanel-server-windows-386.zip`, `ppanel-server-windows-amd64.zip`, `ppanel-server-windows-arm64.zip` |
| macOS | `ppanel-server-darwin-amd64.tar.gz`, `ppanel-server-darwin-arm64.tar.gz` |

The binaries are static (`CGO_ENABLED=0`), so a Linux archive runs on any distribution of its architecture,
Alpine included. Example for Linux amd64 and release `v1.2.3`:

```shell
VERSION=v1.2.3
BASE="https://github.com/perfect-panel/backend/releases/download/$VERSION"
curl -fsSLO "$BASE/ppanel-server-linux-amd64.tar.gz"
curl -fsSLO "$BASE/SHA256SUMS"
sha256sum --ignore-missing -c SHA256SUMS
```

`sha256sum` must print `ppanel-server-linux-amd64.tar.gz: OK`; do not install an archive that fails the check.
(Releases before the checksum files were introduced carry `.md5` files instead; verify those with `md5sum -c`.)

The archive unpacks to a directory named after the asset:

```
ppanel-server-linux-amd64/
├── ppanel-server       # the binary
├── LICENSE
└── etc/
    └── ppanel.yaml     # empty configuration, completed on the first start
```

### 2. Install the files

The service runs as a dedicated system user that owns only its own directory:

```shell
id ppanel >/dev/null 2>&1 || sudo useradd --system --home-dir /opt/ppanel-server --no-create-home --shell /usr/sbin/nologin ppanel
sudo mkdir -p /opt/ppanel-server
sudo tar -xzf ppanel-server-linux-amd64.tar.gz -C /opt/ppanel-server --strip-components=1
sudo chown -R ppanel:ppanel /opt/ppanel-server
sudo chmod 0750 /opt/ppanel-server
sudo chmod 0600 /opt/ppanel-server/etc/ppanel.yaml
```

The layout after the first start: `/opt/ppanel-server/ppanel-server` (binary), `etc/ppanel.yaml`
(configuration, holds the JWT secret and database credentials), `logs/` (`Logger.Path`) and `cache/`
(GeoIP database).

### 3. Create the systemd unit

```shell
sudo tee /etc/systemd/system/ppanel.service > /dev/null <<'EOF'
[Unit]
Description=PPanel Server
Documentation=https://github.com/perfect-panel/backend/blob/main/docs/guide/install.md
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=ppanel
Group=ppanel
WorkingDirectory=/opt/ppanel-server
# Optional: PPANEL_DB and PPANEL_REDIS for a non-interactive first start (mode 0600).
EnvironmentFile=-/opt/ppanel-server/etc/ppanel.env
ExecStart=/opt/ppanel-server/ppanel-server run --config /opt/ppanel-server/etc/ppanel.yaml
Restart=on-failure
RestartSec=5s
# Shutdown drains HTTP, then the scheduler, the task worker and the trace
# exporter, which can take up to about 18 s.
TimeoutStopSec=25
# The server writes only under its own directory.
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/opt/ppanel-server

[Install]
WantedBy=multi-user.target
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now ppanel
```

- `run` is the subcommand that starts the server; the bare binary only prints its usage and exits, which under
  `Restart=always` would loop forever.
- `WorkingDirectory` matters: the log directory (`Logger.Path`, `logs`) and the GeoIP database (`GeoIP.Path`,
  `./cache/...`) are relative to it.
- `ExecStart` names the configuration file explicitly; `--config` defaults to `etc/ppanel.yaml` relative to the
  working directory, which is the same file here.

### 4. First start

With an empty configuration file the server does not start the API. It starts the **setup wizard on `127.0.0.1`
only**, at the configured `Port` (8080 by default) and never on a public interface, so `http://<server>:8080/init`
is not reachable from your browser. Complete the installation in one of two ways.

**A. The wizard through an SSH tunnel.** From your workstation:

```shell
ssh -L 8080:127.0.0.1:8080 user@your-server
```

Then open <http://127.0.0.1:8080/init> in your browser and enter the database, Redis and first administrator
(password of at least 8 characters). The wizard tests the connections, applies the database migrations, creates
the administrator, writes `etc/ppanel.yaml` (with the administrator's email, never the password) and hands over
to the API server, which listens on `Host:Port` (`0.0.0.0:8080` by default). Should a step fail after the file
was written, restart the server: it resumes the installation from the file.

**B. Non-interactive, with environment variables.** Give the first start the connections in `PPANEL_DB` and
`PPANEL_REDIS` ([config.md, section 4](config.md#4-environment-variables) has the formats). The server completes
the empty configuration file — whatever path `--config` names — with them and a generated `JwtAuth.AccessSecret`,
then starts normally: it applies the migrations and creates the first administrator, whose generated password is
printed once in the log. With the unit above:

```shell
sudo install -m 0600 -o ppanel -g ppanel /dev/null /opt/ppanel-server/etc/ppanel.env
sudo tee /opt/ppanel-server/etc/ppanel.env > /dev/null <<'EOF'
PPANEL_DB=ppanel:secret@tcp(127.0.0.1:3306)/ppanel
PPANEL_REDIS=redis://:secret@127.0.0.1:6379/0
EOF
sudo systemctl restart ppanel
sudo journalctl -u ppanel -n 50      # the first administrator's password is printed here once
```

PostgreSQL: `PPANEL_DB=postgres://ppanel:secret@127.0.0.1:5432/ppanel?sslmode=require`. The variables are ignored
once the file has a secret, so the file may stay; delete it if you prefer not to keep the credentials twice.

### 5. Verify

```shell
systemctl status ppanel
curl -fsS http://127.0.0.1:8080/healthz    # liveness: the process and its listener are up
curl -fsS http://127.0.0.1:8080/readyz     # readiness: start-up finished, database and Redis answer (503 otherwise)
/opt/ppanel-server/ppanel-server healthcheck --config /opt/ppanel-server/etc/ppanel.yaml --timeout 3s
```

`healthcheck` requests `/healthz` on the configured listener (`Host` when it is not `0.0.0.0`, HTTPS when `TLS`
is enabled) and exits non-zero when the server does not answer within `--timeout`; use it from a watchdog. A `503`
from `/readyz` names the reason as JSON (`database unreachable`, `redis unreachable`, `runtime bootstrap not
finished`, `runtime bootstrap failed`). Sign in to the admin panel with the first administrator and change its
password.

## Operations

- **Service**: `systemctl start|stop|restart|status ppanel`; `systemctl enable ppanel` for start on boot.
- **Logs**: `journalctl -u ppanel -f` for the console output (start-up messages, the generated administrator
  password). The request logs go to `/opt/ppanel-server/logs/` (`access.log`, `error.log`, `slow.log`) when
  `Logger.Mode` is `file`, the default.
- **Upgrade**: back up the database, download and verify the new archive as in step 1, then:

  ```shell
  sudo systemctl stop ppanel
  sudo tar -xzf ppanel-server-linux-amd64.tar.gz -C /tmp
  sudo install -m 0755 -o ppanel -g ppanel /tmp/ppanel-server-linux-amd64/ppanel-server /opt/ppanel-server/ppanel-server
  sudo systemctl start ppanel
  ```

  The migrations run at start-up. Downgrading is not supported once a migration has run: the only rollback is
  restoring the database backup. Re-running `script/install.sh` performs the same upgrade.
- **Stopping**: `systemctl stop ppanel` (and `docker stop --time 20` for the container) waits for the graceful
  shutdown, which drains HTTP first, then the scheduler, the task worker and the trace exporter; allow about 20 s
  before forcing it.
- **Moving from MySQL to PostgreSQL**: `ppanel-server migrate mysql2postgres` copies a MySQL database into an
  empty PostgreSQL one. Pass `--location <IANA zone>` (default `Asia/Shanghai`), the zone the MySQL `DATETIME`
  values are stored in, so every stored time keeps its instant; see
  [tools/mysql2postgres/README.md](../../tools/mysql2postgres/README.md).
- **Configuration**: every key of `etc/ppanel.yaml` is documented in [config.md](config.md). The file holds the
  JWT secret and the database credentials; keep it at mode `0600`.

## Docker

The [README](../../README.md#-docker-deployment) covers the container image. In short: the image runs as uid 65532,
its `HEALTHCHECK` runs `ppanel healthcheck`, and because the setup wizard listens on `127.0.0.1` inside the
container, a container deployment must either set `PPANEL_DB` and `PPANEL_REDIS` or mount a pre-filled
`etc/ppanel.yaml`. With the repository's `docker-compose.yml`, `./etc/ppanel.yaml` must exist as a file before the
first `docker compose up` (Docker otherwise creates a directory of that name) and be writable by uid 65532. Stop
the container with `docker stop --time 20` (the compose file sets `stop_grace_period: 20s`) so the graceful
shutdown can finish.

## NGINX reverse proxy

Below is an example configuration that proxies the server to `api.ppanel.dev`. Terminate TLS here (for example with
Certbot) rather than in the server.

```nginx
# In the http block: needed by the WebSocket and SSE endpoints.
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 80;
    server_name api.ppanel.dev;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        # Streaming responses (order events, node WebSocket) stay open longer
        # than a request.
        proxy_read_timeout 300s;
        proxy_buffering off;

        add_header X-Cache $upstream_cache_status;

        # Cache static files briefly, nothing else.
        set $static_file_cache 0;
        if ($uri ~* "\.(gif|png|jpg|css|js|woff|woff2)$") {
            set $static_file_cache 1;
            expires 1m;
        }
        if ($static_file_cache = 0) {
            add_header Cache-Control no-cache;
        }
    }
}
```

nginx on the same host is trusted by default: `TrustedProxies` covers the loopback interface and the private
networks, so `X-Forwarded-For` names the client without configuration. List the proxy only when it has a public
address (or to be explicit), and list the frontends' origins. In `etc/ppanel.yaml`:

```yaml
TrustedProxies: ["127.0.0.1"]                       # optional here: the nginx host is covered by the default
AllowedOrigins: ["https://user.ppanel.dev", "https://admin.ppanel.dev"]
```

### Cloudflare

Behind Cloudflare, nginx sees Cloudflare's addresses; the visitor's address arrives in the `CF-Connecting-IP`
header. Let nginx restore it, so that the `X-Real-IP` / `X-Forwarded-For` it passes on name the visitor. This
needs `ngx_http_realip_module` (check `nginx -V`; the distribution packages include it). In the `http` section:

```nginx
# Cloudflare Start: trust only Cloudflare's ranges, https://www.cloudflare.com/ips/
set_real_ip_from 173.245.48.0/20;
set_real_ip_from 103.21.244.0/22;
# ... the other IPv4 and IPv6 ranges of that page ...
real_ip_header CF-Connecting-IP;
# Cloudflare End
```

Do not use `set_real_ip_from 0.0.0.0/0`: it lets any client forge its address. `TrustedProxies` in
`etc/ppanel.yaml` still names only the nginx host (or keeps its default), never Cloudflare's ranges.
