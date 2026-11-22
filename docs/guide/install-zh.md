# 安装说明

## 前置系统要求

- 数据库：MySQL 8.0+、MariaDB 11.8+ 或 PostgreSQL 16+（CI 会在这三种数据库上运行测试）。
- Redis 6.0+（推荐 7.x）。
- 下面的服务安装需要带 systemd 的 Linux，以及 `curl`、`tar`、`sha256sum`。发布包同样覆盖 Windows 与 macOS，启动方式相同
  （`ppanel-server run --config ...`）。

配置文件说明见 [config-zh.md](config-zh.md)；容器镜像见 [README_ZH](../../README_ZH.md#-docker-部署)。

## 一键安装（Linux 脚本）

[`script/install.sh`](../../script/install.sh) 会下载与本机架构（amd64 或 arm64）对应的最新发布包，用发布中的
`SHA256SUMS` 校验，安装到 `/opt/ppanel-server`，创建系统用户 `ppanel` 和 systemd 服务，并启动服务：

```shell
curl -fsSL https://raw.githubusercontent.com/perfect-panel/backend/main/script/install.sh -o install.sh
less install.sh    # 以 root 运行之前先阅读脚本内容
sudo bash install.sh
```

再次运行即升级二进制并保留 `etc/`。`PPANEL_VERSION=v1.2.3 sudo -E bash install.sh` 安装指定版本。需要无人值守完成首次启动时，运行前导出
`PPANEL_DB` 与 `PPANEL_REDIS`（见[首次启动](#4-首次启动)）：脚本会把它们写入 `/opt/ppanel-server/etc/ppanel.env`，由服务单元加载。

## 手动安装

### 1. 下载并校验

发布地址：<https://github.com/perfect-panel/backend/releases>。每个发布包含每种平台一个压缩包、每个压缩包旁的 `.sha256`
文件，以及覆盖全部压缩包的 `SHA256SUMS`：

| 平台 | 文件 |
|---|---|
| Linux | `ppanel-server-linux-386.tar.gz`、`ppanel-server-linux-amd64.tar.gz`、`ppanel-server-linux-arm64.tar.gz` |
| Windows | `ppanel-server-windows-386.zip`、`ppanel-server-windows-amd64.zip`、`ppanel-server-windows-arm64.zip` |
| macOS | `ppanel-server-darwin-amd64.tar.gz`、`ppanel-server-darwin-arm64.tar.gz` |

二进制是静态链接的（`CGO_ENABLED=0`），Linux 压缩包可在同架构的任何发行版上运行，包括 Alpine。以 Linux amd64、版本 `v1.2.3` 为例：

```shell
VERSION=v1.2.3
BASE="https://github.com/perfect-panel/backend/releases/download/$VERSION"
curl -fsSLO "$BASE/ppanel-server-linux-amd64.tar.gz"
curl -fsSLO "$BASE/SHA256SUMS"
sha256sum --ignore-missing -c SHA256SUMS
```

`sha256sum` 必须输出 `ppanel-server-linux-amd64.tar.gz: OK`；校验失败的压缩包请不要安装。（引入校验和文件之前的旧版本只带
`.md5` 文件，请用 `md5sum -c` 校验。）

压缩包解压后是一个与文件同名的目录：

```
ppanel-server-linux-amd64/
├── ppanel-server       # 二进制
├── LICENSE
└── etc/
    └── ppanel.yaml     # 空配置文件，首次启动时补全
```

### 2. 安装文件

服务以专用系统用户运行，该用户只拥有自己的目录：

```shell
id ppanel >/dev/null 2>&1 || sudo useradd --system --home-dir /opt/ppanel-server --no-create-home --shell /usr/sbin/nologin ppanel
sudo mkdir -p /opt/ppanel-server
sudo tar -xzf ppanel-server-linux-amd64.tar.gz -C /opt/ppanel-server --strip-components=1
sudo chown -R ppanel:ppanel /opt/ppanel-server
sudo chmod 0750 /opt/ppanel-server
sudo chmod 0600 /opt/ppanel-server/etc/ppanel.yaml
```

首次启动后的目录布局：`/opt/ppanel-server/ppanel-server`（二进制）、`etc/ppanel.yaml`（配置文件，保存 JWT 密钥与数据库凭据）、
`logs/`（`Logger.Path`）、`cache/`（GeoIP 数据库）。

### 3. 创建 systemd 服务

```shell
sudo tee /etc/systemd/system/ppanel.service > /dev/null <<'EOF'
[Unit]
Description=PPanel Server
Documentation=https://github.com/perfect-panel/backend/blob/main/docs/guide/install-zh.md
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=ppanel
Group=ppanel
WorkingDirectory=/opt/ppanel-server
# 可选：用于无人值守首次启动的 PPANEL_DB 与 PPANEL_REDIS（权限 0600）。
EnvironmentFile=-/opt/ppanel-server/etc/ppanel.env
ExecStart=/opt/ppanel-server/ppanel-server run --config /opt/ppanel-server/etc/ppanel.yaml
Restart=on-failure
RestartSec=5s
# 停止时依次排空 HTTP、调度器、任务 worker 与链路追踪导出，最长约 18 秒。
TimeoutStopSec=25
# 服务只在自己的目录下写文件。
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

- `run` 是启动服务的子命令；不带子命令的二进制只会打印用法并退出，配合 `Restart=always` 会无限重启。
- `WorkingDirectory` 很重要：日志目录（`Logger.Path`，即 `logs`）和 GeoIP 数据库（`GeoIP.Path`，即 `./cache/...`）都相对于它。
- `ExecStart` 显式指定了配置文件；`--config` 的默认值是相对工作目录的 `etc/ppanel.yaml`，在这里是同一个文件。

### 4. 首次启动

配置文件为空时服务不会启动 API，而是启动**只监听 `127.0.0.1` 的安装向导**（端口为配置的 `Port`，默认 8080），绝不监听公网接口，
因此浏览器无法访问 `http://服务器地址:8080/init`。请用以下两种方式之一完成安装。

**A. 通过 SSH 隧道使用向导。** 在自己的电脑上执行：

```shell
ssh -L 8080:127.0.0.1:8080 user@your-server
```

然后在浏览器打开 <http://127.0.0.1:8080/init>，填写数据库、Redis 与首位管理员（密码至少 8 个字符）。向导会测试连接、执行数据库迁移、创建管理员、写入
`etc/ppanel.yaml`（只写入管理员邮箱，绝不写入密码），随后交给 API 服务，后者监听 `Host:Port`（默认 `0.0.0.0:8080`）。若在写入文件之后某一步失败，
重启服务即可从文件继续完成安装。

**B. 通过环境变量无人值守安装。** 在首次启动时用 `PPANEL_DB` 与 `PPANEL_REDIS` 提供连接信息（格式见
[config-zh.md 第 4 节](config-zh.md#4-环境变量)）。服务会用它们和生成的 `JwtAuth.AccessSecret` 补全空的配置文件——不论 `--config`
指向哪个路径——然后正常启动：执行迁移并创建首位管理员，其生成的密码会在日志中打印一次。配合上面的服务单元：

```shell
sudo install -m 0600 -o ppanel -g ppanel /dev/null /opt/ppanel-server/etc/ppanel.env
sudo tee /opt/ppanel-server/etc/ppanel.env > /dev/null <<'EOF'
PPANEL_DB=ppanel:secret@tcp(127.0.0.1:3306)/ppanel
PPANEL_REDIS=redis://:secret@127.0.0.1:6379/0
EOF
sudo systemctl restart ppanel
sudo journalctl -u ppanel -n 50      # 首位管理员的密码只在这里打印一次
```

PostgreSQL：`PPANEL_DB=postgres://ppanel:secret@127.0.0.1:5432/ppanel?sslmode=require`。文件有了密钥之后这两个变量就会被忽略，
因此 env 文件可以保留；不想让凭据保存两份的话可以删除它。

### 5. 验证

```shell
systemctl status ppanel
curl -fsS http://127.0.0.1:8080/healthz    # 存活探针：进程及其监听器已启动
curl -fsS http://127.0.0.1:8080/readyz     # 就绪探针：启动完成，数据库与 Redis 均有响应（否则 503）
/opt/ppanel-server/ppanel-server healthcheck --config /opt/ppanel-server/etc/ppanel.yaml --timeout 3s
```

`healthcheck` 向配置的监听地址（`Host` 不是 `0.0.0.0` 时使用 `Host`，开启 `TLS` 时使用 HTTPS）上的 `/healthz` 发起请求，`--timeout`
内没有响应即以非零状态退出，可用于看门狗。`/readyz` 返回 `503` 时会以 JSON 给出原因（`database unreachable`、`redis unreachable`、
`runtime bootstrap not finished`、`runtime bootstrap failed`）。请用首位管理员登录管理面板并修改其密码。

## 运维

- **服务**：`systemctl start|stop|restart|status ppanel`；`systemctl enable ppanel` 设置开机自启。
- **日志**：`journalctl -u ppanel -f` 查看控制台输出（启动信息、生成的管理员密码）。`Logger.Mode` 为默认的 `file` 时，请求日志写入
  `/opt/ppanel-server/logs/`（`access.log`、`error.log`、`slow.log`）。
- **升级**：先备份数据库，按第 1 步下载并校验新压缩包，然后：

  ```shell
  sudo systemctl stop ppanel
  sudo tar -xzf ppanel-server-linux-amd64.tar.gz -C /tmp
  sudo install -m 0755 -o ppanel -g ppanel /tmp/ppanel-server-linux-amd64/ppanel-server /opt/ppanel-server/ppanel-server
  sudo systemctl start ppanel
  ```

  迁移在启动时执行。迁移一旦执行就不支持降级，唯一的回滚手段是恢复数据库备份。重新运行 `script/install.sh` 会执行同样的升级。
- **停止**：`systemctl stop ppanel`（容器则为 `docker stop --time 20`）会等待优雅停机：先排空 HTTP，再依次停止调度器、任务 worker 和链路追踪导出，请预留约 20 秒再强制结束。
- **从 MySQL 迁移到 PostgreSQL**：`ppanel-server migrate mysql2postgres` 把 MySQL 数据库复制到一个空的 PostgreSQL 数据库。请用
  `--location <IANA 时区>`（默认 `Asia/Shanghai`）指定 MySQL `DATETIME` 值所在的时区，以保证每个时间点不变；详见
  [tools/mysql2postgres/README.md](../../tools/mysql2postgres/README.md)。
- **配置**：`etc/ppanel.yaml` 的每个配置项都在 [config-zh.md](config-zh.md) 中说明。文件保存着 JWT 密钥和数据库凭据，请保持权限为 `0600`。

## Docker

容器镜像见 [README_ZH](../../README_ZH.md#-docker-部署)。简而言之：镜像以 uid 65532 运行，`HEALTHCHECK` 运行 `ppanel healthcheck`；
由于安装向导在容器内只监听 `127.0.0.1`，容器部署必须设置 `PPANEL_DB` 与 `PPANEL_REDIS`，或挂载预先填好的 `etc/ppanel.yaml`。使用仓库中的
`docker-compose.yml` 时，首次 `docker compose up` 之前 `./etc/ppanel.yaml` 必须以文件形式存在（否则 Docker 会创建同名目录），且 uid 65532 可写。
停止容器请用 `docker stop --time 20`（compose 文件已设置 `stop_grace_period: 20s`），让优雅停机得以完成。

## NGINX 反向代理配置

以下示例把服务代理到 `api.ppanel.dev`。TLS 请在这里终结（例如使用 Certbot），而不是在服务内。

```nginx
# 放在 http 段：WebSocket 与 SSE 接口需要。
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
        # 流式响应（订单事件、节点 WebSocket）比普通请求保持更久。
        proxy_read_timeout 300s;
        proxy_buffering off;

        add_header X-Cache $upstream_cache_status;

        # 只对静态文件做短暂缓存。
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

同机的 nginx 默认即被信任：`TrustedProxies` 默认覆盖回环接口与私有网段，`X-Forwarded-For` 无需配置就会被采用。只有代理使用公网地址（或希望显式声明）时才需要填写代理，另外把前端来源填入 `AllowedOrigins`。在 `etc/ppanel.yaml` 中：

```yaml
TrustedProxies: ["127.0.0.1"]                       # 此处可省略：nginx 所在主机已在默认范围内
AllowedOrigins: ["https://user.ppanel.dev", "https://admin.ppanel.dev"]
```

### Cloudflare

使用 Cloudflare 时 nginx 看到的是 Cloudflare 的地址，访客地址在 `CF-Connecting-IP` 头中。让 nginx 还原访客地址，这样它传给后端的
`X-Real-IP` / `X-Forwarded-For` 才是访客本人。这需要 `ngx_http_realip_module`（用 `nginx -V` 查看；发行版自带的 nginx 都包含）。在 `http` 段加入：

```nginx
# Cloudflare Start：只信任 Cloudflare 的地址段，https://www.cloudflare.com/ips/
set_real_ip_from 173.245.48.0/20;
set_real_ip_from 103.21.244.0/22;
# ... 该页面列出的其余 IPv4 与 IPv6 地址段 ...
real_ip_header CF-Connecting-IP;
# Cloudflare End
```

不要使用 `set_real_ip_from 0.0.0.0/0`：那会让任何客户端伪造自己的地址。`etc/ppanel.yaml` 中的 `TrustedProxies` 仍然只填 nginx 所在主机（或保持默认），不要填 Cloudflare 的地址段。
