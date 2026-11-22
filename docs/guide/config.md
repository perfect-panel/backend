# PPanel Configuration Guide

This document provides a comprehensive guide to the configuration file for the PPanel application. The configuration
file is in YAML format and defines settings for the server, logging, tracing, database, Redis, and admin access.

## 1. Configuration File Overview

- **Default Path**: `./etc/ppanel.yaml`
- **Custom Path**: Specify a custom path using the `--config` startup parameter.
- **Format**: YAML, supports comments, and must be named with a `.yaml` extension.
- **Permissions**: the file holds the JWT secret and the database credentials. Keep it readable by the service user
  only (`chmod 0600`); the setup wizard and the environment-variable installation (section 4) write it with that mode.

## 2. Configuration File Structure

Below is an example of the configuration file with default values and explanations:

```yaml
# PPanel Configuration
Host: "0.0.0.0"                     # Server listening address
Port: 8080                          # Server listening port
Debug: false                        # Enable debug mode (disables background logging)
TrustedProxies: []                  # Reverse proxies whose X-Forwarded-For is trusted; empty = loopback + private networks, ["none"] = no proxy
AllowedOrigins: []                  # Browser origins admitted by CORS; empty = reflect any Origin
HTTP: # Listener limits
  ReadTimeoutSeconds: 180           # Time allowed to read a request; 0 = unlimited
  WriteTimeoutSeconds: 0            # Time allowed to write a response; 0 = unlimited (streaming endpoints)
  IdleTimeoutSeconds: 180           # Idle time before a keep-alive connection is closed
  MaxRequestBodyMB: 4               # Largest accepted request body, in MB
AppLocation: "Asia/Shanghai"        # Application time zone (see 3.1)
TLS: # Serve HTTPS from the server itself (usually left to a reverse proxy)
  Enable: false
  CertFile: ""
  KeyFile: ""
JwtAuth: # JWT authentication settings
  AccessSecret: ""                  # Access token secret (required, see 3.2)
  AccessExpire: 604800              # Access token expiration (seconds)
Logger: # Logging configuration
  ServiceName: "PPanel"             # Service name for log identification
  Mode: "file"                      # Log output mode (console, file, volume)
  Encoding: "json"                  # Log format (json, plain)
  TimeFormat: "2006-01-02 15:04:05.000"  # Custom time format
  Path: "logs"                      # Log file directory
  Level: "info"                     # Log level (debug, info, error, severe)
  Compress: false                   # Enable log compression
  KeepDays: 30                      # Log retention period (days)
  StackCooldownMillis: 100          # Stack trace cooldown (milliseconds)
  MaxBackups: 30                    # Maximum number of log backups
  MaxSize: 100                      # Maximum log file size (MB)
  Rotation: "daily"                 # Log rotation strategy (daily, size)
Trace: # OpenTelemetry tracing (see 3.4)
  Name: ""                          # Service name recorded in the traces
  Endpoint: ""                      # Collector endpoint; empty = no export
  Sampler: 0.1                      # Fraction of traces sampled
  Batcher: "jaeger"                 # jaeger, zipkin, otlpgrpc, otlphttp or file
  OtlpGrpcSecure: false             # TLS for the otlpgrpc exporter; false = plaintext gRPC
  OtlpHeaders: {}                   # Extra headers for the OTLP exporters
  OtlpHttpPath: ""                  # Path for otlphttp with a host:port Endpoint
  OtlpHttpSecure: false             # HTTPS for otlphttp with a host:port Endpoint
  Disabled: false                   # true = tracing off
Database: # MySQL, MariaDB, or PostgreSQL database configuration
  Driver: "mysql"                   # mysql or postgres
  Addr: ""                          # Database address (required)
  Username: ""                      # Database username (required)
  Password: ""                      # Database password (required)
  Dbname: ""                        # Database name (required)
  Config: "charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai&interpolateParams=true"  # Dialect connection parameters
  MaxIdleConns: 10                  # Maximum idle connections
  MaxOpenConns: 10                  # Maximum open connections
  ConnMaxLifetime: 1800             # Maximum connection lifetime (seconds)
  ConnMaxIdleTime: 300              # Maximum connection idle time (seconds)
  SlowThreshold: 1000               # Slow query threshold (milliseconds)
GeoIP: # MaxMind GeoLite2 city database, locates the addresses in the audit logs (see 3.6)
  Path: "./cache/GeoLite2-City.mmdb"
  Download: true                    # Download the database when Path is missing
  DownloadURL: ""                   # Empty = the built-in mirror
  SHA256: ""                        # Expected hex digest of the file; empty = not checked
  Required: false                   # true = refuse to start without a valid database
Redis: # Redis configuration
  Host: "localhost:6379"            # Redis address
  Username: ""                      # Optional ACL user (empty uses default)
  Pass: ""                          # Redis password
  KeyPrefix: ""                     # Example: panel-a gives panel-a:...
  DB: 0                             # Redis database of the cache and sessions
  QueueDB: 5                        # Redis database of the task queue
Administrator: # First administrator, created on the first start
  Email: "admin@ppanel.dev"         # Admin login email
  Password: ""                      # Admin login password; empty = generate one
```

## 3. Configuration Details

### 3.1 Server Settings

- **`Host`**: Address the server listens on.
  - Default: `0.0.0.0` (all network interfaces).
  - It is only a bind address. Public links, such as payment callback URLs, are built from the site host
    (site settings, see 3.10) or a payment method's own domain, and OAuth redirects are pinned to the site host.
- **`Port`**: Port the server listens on.
  - Default: `8080`.
  - The container image runs as an unprivileged user, which cannot bind a port below 1024: publish a low port with
    a port mapping (`-p 443:8080`) instead of changing `Port`.
- **`Debug`**: Enables debug mode, disabling background logging.
  - Default: `false`.
- **`TrustedProxies`**: Reverse proxies whose `X-Forwarded-For` and `X-Real-IP` headers name the real client, as IP
  addresses or CIDR ranges, for example `["127.0.0.1", "10.0.0.0/8"]`.
  - Default: the loopback interface and the private networks (`127.0.0.0/8`, `::1/128`, `10.0.0.0/8`,
    `172.16.0.0/12`, `192.168.0.0/16`, `fc00::/7`), so nginx on the same host, a Docker bridge or an internal load
    balancer is trusted without configuration. A configured list replaces the default; the entry `private` stands for
    the default networks (`["private", "203.0.113.10"]` adds a public proxy), and `["none"]` trusts no header at
    all, for a server its clients reach directly.
  - The client address feeds the login and audit logs and every per-IP limit (verification codes, device login,
    availability checks, subscription fetches, registrations). When the proxy's address is outside the default
    networks (a load balancer or a Cloudflare tunnel with a public address), list it here, otherwise every client
    appears as the proxy and the limits apply to all clients together; [install.md, NGINX reverse proxy](install.md#nginx-reverse-proxy)
    shows the matching nginx configuration. List the proxies only, never the clients: an entry such as `0.0.0.0/0`
    lets anyone choose the address the server records.
- **`AllowedOrigins`**: Browser origins admitted by CORS, as `scheme://host[:port]`, for example
  `["https://user.example.com", "https://admin.example.com"]`.
  - Default: empty, which keeps the permissive behaviour of reflecting the request's `Origin`: any website's scripts
    may call the API from a browser.
  - When set, a listed origin is matched exactly (case-insensitively) and receives the CORS headers, with
    credentials allowed; an unlisted origin receives no CORS headers at all, so the browser blocks the call. The
    device WebSocket (`/v1/app/ws/...`) applies the same list to browser clients.
  - Set it to the origins of the user and admin frontends in production. Requests without an `Origin` header
    (clients, nodes, subscriptions) are not affected.
- **`HTTP`**: Limits of the API listener. The defaults are the values the server ran with before they were
  configurable.
  - **`ReadTimeoutSeconds`**: Time allowed to read a request, headers and body. Default: `180`; `0` = unlimited.
  - **`WriteTimeoutSeconds`**: Time allowed to write a response. Default: `0` = unlimited, which the streaming
    endpoints (order events over SSE, the node WebSocket) need; a bound here would cut them off.
  - **`IdleTimeoutSeconds`**: Idle time before a keep-alive connection is closed. Default: `180`.
  - **`MaxRequestBodyMB`**: Largest request body accepted, in MB; larger requests are rejected before they are read.
    Default: `4`, enough for every API request including uploaded logos and templates.
- **`AppLocation`**: IANA time zone of the application: "today", expiry reminders, reset cycles and the daily
  statistics are computed in it.
  - Default: `Asia/Shanghai`.
  - Any IANA zone name works: the binary embeds the time zone database (`time/tzdata`), so neither the host
    nor the container image needs zoneinfo files.
  - The server also makes it the process time zone at startup, whatever `TZ` says, so every time it writes
    (automatic `created_at` / `updated_at` included) is on the same clock.
  - It must match the database time zone (`Database.Config`: MySQL `loc`, PostgreSQL `TimeZone`), in which
    timestamps are stored and statistics are grouped by day. The setup page, and the `PPANEL_DB` /
    `PPANEL_REDIS` environment-variable install, write the zone of `AppLocation` into a new database's
    parameters; for an existing database the server logs an error at startup when they differ. Change the database
    zone only on an empty database or after converting the stored times: changing it reinterprets every stored time.
- **`TLS`**: Serve HTTPS from the server itself.
  - **`Enable`**: Default `false`. The usual deployment terminates TLS at a reverse proxy, which is then listed in
    `TrustedProxies`.
  - **`CertFile`**, **`KeyFile`**: Paths of the PEM certificate chain and private key, readable by the service user.

### 3.2 JWT Authentication (`JwtAuth`)

- **`AccessSecret`**: Secret key for access tokens. Sessions, order event tickets and guest-checkout signatures
  are all keyed by it, so an empty secret would let anyone forge them.
  - Required: the server exits at startup when it is empty, and logs a warning when it is shorter than 16
    characters. Use a long random value.
  - Only a new installation gets one generated: when the configuration file has no secret, the setup wizard (or the
    `PPANEL_DB` / `PPANEL_REDIS` environment variables completing the file, section 4) writes a generated secret
    into it. A pre-filled file must contain one.
- **`AccessExpire`**: Token expiration time in seconds.
  - Default: `604800` (7 days).

### 3.3 Logging (`Logger`)

- **`ServiceName`**: Identifier for logs; in `volume` mode it names the log directory.
  - Default: `PPanel`.
- **`Mode`**: Log output destination.
  - Options: `console` (stdout/stderr), `file` (to a directory), `volume` (Docker volume).
  - Default: `file`.
- **`Encoding`**: Log format.
  - Options: `json` (structured JSON), `plain` (plain text with colors).
  - Default: `json`.
- **`TimeFormat`**: Custom time format for logs.
  - Default: `2006-01-02 15:04:05.000`.
- **`Path`**: Directory for log files (`access.log`, `error.log`, `slow.log`), relative to the working directory.
  - Default: `logs`.
- **`Level`**: Log filtering level.
  - Options: `debug` (everything), `info` (everything but debug), `error` (errors, slow queries and stack traces
    only), `severe` (nothing: the server writes no entry above `error`, so this level silences the log).
  - Default: `info`.
- **`Compress`**: Enable compression for log files (only in `file` mode).
  - Default: `false`.
- **`KeepDays`**: Retention period for log files (in days).
  - Default: `30`.
- **`StackCooldownMillis`**: Cooldown for stack trace logging to prevent log flooding.
  - Default: `100`.
- **`MaxBackups`**: Maximum number of log backups (for `size` rotation).
  - Default: `30`.
- **`MaxSize`**: Maximum log file size in MB (for `size` rotation).
  - Default: `100`.
- **`Rotation`**: Log rotation strategy.
  - Options: `daily` (rotate daily), `size` (rotate by size).
  - Default: `daily`.

### 3.4 Tracing (`Trace`)

OpenTelemetry tracing of the HTTP requests, the database calls and the queued tasks. Nothing is exported until
`Endpoint` is set.

- **`Name`**: Service name recorded on the traces (`service.name`).
  - Default: empty; give it a name such as `ppanel` when exporting.
- **`Endpoint`**: Where the batcher sends the spans; empty exports nothing.
  - For the `jaeger` batcher it is Jaeger's OTLP/HTTP receiver, either a URL such as `http://jaeger:4318`
    (posting to `/v1/traces` unless the URL names another path; the scheme selects TLS) or a bare `host:port`.
    Jaeger has received OTLP natively since 1.35; the endpoints of the former Jaeger Thrift exporter
    (`udp://host:6831`, `http://host:14268/api/traces`) are rewritten to OTLP on port 4318 of the same host, with a
    warning in the log.
  - For `otlpgrpc` it is the collector's gRPC address (`host:4317`), for `otlphttp` a URL or `host:port`
    (see `OtlpHttpPath` and `OtlpHttpSecure`), for `zipkin` the Zipkin API URL and for `file` a file path.
- **`Sampler`**: Fraction of the traces kept, from `0` to `1`.
  - Default: `0.1`.
- **`Batcher`**: The exporter: `jaeger`, `zipkin`, `otlpgrpc`, `otlphttp` or `file`.
  - Default: `jaeger`.
  - `zipkin` uses an exporter its upstream has deprecated; prefer an OTLP endpoint (`otlpgrpc`, `otlphttp` or
    `jaeger`) for new deployments.
- **`OtlpGrpcSecure`**: Transport security of the `otlpgrpc` exporter.
  - Default: `false`: the gRPC connection to the collector is made without TLS, as the server always did. Set it to
    `true` for a collector that serves TLS; the system's root certificates verify it. `otlphttp` selects TLS through
    the URL scheme or `OtlpHttpSecure` instead.
- **`OtlpHeaders`**: Headers sent with every OTLP export, for example an `uptrace-dsn` or an `Authorization` header
  for a hosted collector.
- **`OtlpHttpPath`**: Path of the OTLP/HTTP receiver when `Endpoint` is a bare `host:port`, for example
  `/v1/traces`.
- **`OtlpHttpSecure`**: Use HTTPS for `otlphttp` when `Endpoint` is a bare `host:port`.
  - Default: `false`.
- **`Disabled`**: Turns tracing off entirely, whatever the other settings say.
  - Default: `false`.

### 3.5 Database (`Database`)

- **`Driver`**: Database dialect: `mysql` or `postgres`. Use `mysql` for both MySQL 8 and MariaDB 11.8.
  - Default: `mysql`.
- **`Addr`**: Database server address.
  - Required.
- **`Username`**: Database username.
  - Required.
- **`Password`**: Database password.
  - Required.
- **`Dbname`**: Database name.
  - Required.
- **`Config`**: Dialect-specific connection parameters.
  - MySQL default: `charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai&interpolateParams=true`.
  - PostgreSQL default: `sslmode=prefer&TimeZone=<AppLocation>&application_name=perfect-panel`, for example
    `sslmode=prefer&TimeZone=Asia/Shanghai&application_name=perfect-panel`.
  - PostgreSQL: `prefer` encrypts the connection when the server offers TLS, but does not verify the server's
    certificate, and falls back to plaintext without a warning when it does not. For a database reached over a
    network set `sslmode=verify-full` (with the server's CA certificate), or at least `sslmode=require`,
    explicitly; `sslmode=disable` is only for a database on the same host or in a private network.
  - PostgreSQL: a `Config` without `TimeZone` gets `TimeZone=<AppLocation>` added automatically. With a custom
    `Config` and an `AppLocation` other than `Asia/Shanghai`, still write `TimeZone=<AppLocation>` into it
    explicitly; the server logs an error at start-up when it cannot determine the database zone.
  - The zone in these parameters is the database time zone; see `AppLocation`.
- **`MaxIdleConns`**: Maximum idle connections.
  - Default: `10`.
- **`MaxOpenConns`**: Maximum open connections.
  - Default: `10`.
- **`ConnMaxLifetime`**: Maximum time a pooled connection may be reused, in seconds.
  - Default: `1800`.
- **`ConnMaxIdleTime`**: Maximum time an idle pooled connection is retained, in seconds.
  - Default: `300`.
- **`SlowThreshold`**: Threshold for slow query logging (in milliseconds).
  - Default: `1000`.

### 3.6 GeoIP (`GeoIP`)

The MaxMind GeoLite2 city database gives the login and audit logs a location for each address. Without it the
server runs normally and records the addresses alone.

- **`Path`**: Path of the `GeoLite2-City.mmdb` file, relative to the working directory. The ASN database
  (`GeoLite2-ASN.mmdb`) is expected next to it, in the same directory.
  - Default: `./cache/GeoLite2-City.mmdb`. The container image ships a writable `/app/cache` for it.
- **`Download`**: Download the city database at startup when the file at `Path` is missing or fails the `SHA256`
  check. With `DownloadURL` empty the ASN database is downloaded from the built-in mirror as well; with a custom
  `DownloadURL` only the city database is fetched, and the ASN database has to be placed next to it by hand.
  - Default: `true`.
- **`DownloadURL`**: Where to download the city database from.
  - Default: empty, the built-in mirror (a copy of GeoLite2-City published on GitHub). Point it at your own copy,
    or at MaxMind's download link with your licence key, to control what the server loads.
- **`SHA256`**: Hex SHA-256 digest the city database must have. An existing file with another digest is
  downloaded again when `Download` is on, and a download with another digest never replaces the file.
  - Default: empty, no check. Set it when `DownloadURL` names a file you control.
- **`Required`**: Make a missing or invalid city database fatal at startup.
  - Default: `false`: the server starts without geolocation and logs why.

### 3.7 Redis (`Redis`)

- **`Host`**: Redis server address.
  - Default: `localhost:6379`.
- **`Username`** selects the ACL user for both application and queue connections. An empty value uses the default user.
- **`Pass`**: Redis password.
  - Default: `""` (no password).
- **`DB`**: Redis database index of the cache, the sessions and the rate limits.
  - Default: `0`.
- **`QueueDB`**: Redis database index of the task queue (asynq), shared by the producer, the consumer and the
  scheduler.
  - Default: `5`, the database the queue always used, so an upgrade keeps its queued tasks. Deployments sharing the same databases should use distinct project prefixes.

- **`KeyPrefix`** scopes cache, session, rate-limit and queue keys, worker metadata and Pub/Sub channels. With `panel-a` or `panel-a:`, keys start with `panel-a:`, including `panel-a:asynq:queues`. ACL patterns for this project are `~panel-a:*` for keys and `&panel-a:*` for channels.
  An empty prefix keeps existing names. Enabling or changing it selects a new keyspace. Drain outstanding jobs or migrate the old data before switching all producers and workers together. Existing sessions and pending jobs stay under the old names. Use the same prefix for queue inspection. The [Asynq fork used by PPanel](https://github.com/pars-aria-labs/asynq/tree/v1.0.1) provides a CLI with a `--prefix` option.

### 3.8 Admin Login (`Administrator`)

Seeds the first administrator: at every start, when the database has no administrator, one is created from these
values. The setup wizard writes the installer's email here and never the password.

- **`Email`**: Admin login email.
  - Default: `admin@ppanel.dev`.
- **`Password`**: Admin login password.
  - Default: empty. When it is empty, a random password is generated and printed once in the startup log
    (`docker logs`, `journalctl -u ppanel`). Sign in with it and change it. A configured value is used as given.
  - Remove it once the administrator exists: a `Password` still in the file at that point is reported in the log
    at every start, since the file then holds a credential nothing needs.

### 3.9 Email Delivery (SMTP)

The SMTP relay is not part of this file: administrators configure it in the panel (system settings, email),
and the settings are stored in the database. The fields are:

| Field | Meaning |
|---|---|
| `host`, `port` | Relay address. Port 465 is implicit TLS (SMTPS); 25, 587 and 2525 start in plaintext and upgrade with STARTTLS. |
| `user`, `pass` | Relay credentials; empty to send without authentication. |
| `from`, `reply_to` | Sender address and, optionally, the reply address. The display name is the site name. |
| `ssl` | Encryption required. On port 465 (or with `implicit_tls`) the connection starts with TLS; on any other port the session must upgrade with STARTTLS, and a relay that does not offer it is refused before the credentials are sent. This is the setting to use with the STARTTLS ports of Mailgun, SendGrid, Postmark or Brevo (587, 2525). Off, STARTTLS is used when the relay offers it and the session stays in plaintext otherwise. |
| `implicit_tls` | Start the connection with a TLS handshake on a port other than 465. Only for relays that serve SMTPS on a non-standard port; never for a STARTTLS port, where the handshake would meet a plaintext greeting and no mail would go out. |
| `insecure_skip_verify` | Accept any relay certificate. Only for relays with a self-signed certificate; certificates are verified by default. |

### 3.10 Site Host (system settings)

`Site.Host` is not part of this file either: administrators set it in the panel (system settings, site) as the
public URL of the panel, for example `https://panel.example.com`. Payment callbacks are built from it, and the
OAuth redirects of the Apple and Telegram sign-in methods are pinned to it. It must be set when either of those
methods is enabled: with it empty, a redirect is only accepted when it stays on the API's own host (the same host,
a subdomain or a parent domain), otherwise the sign-in is refused, and the server logs an error at start-up naming
the affected methods.

## 4. Environment Variables

Two environment variables complete an empty configuration file on the first start, for containers and unattended
installations, where the setup wizard (which listens on `127.0.0.1` only) is out of reach:

| Environment Variable | Configuration Section | Format | Example |
|----------------------|-----------------------|--------|---------|
| `PPANEL_DB` | `Database` | MySQL DSN `user:password@tcp(host:port)/dbname[?params]`, or a URL: `mysql://user:password@host:3306/dbname`, `postgres://user:password@host:5432/dbname[?sslmode=require]` | `ppanel:secret@tcp(127.0.0.1:3306)/ppanel` |
| `PPANEL_REDIS` | `Redis` | `redis://[username:password@]host[:port][/db][?prefix=project]` (port `6379` and database `0` when omitted) | `redis://panel:secret@127.0.0.1:6379/0?prefix=panel-a` |

- Both are needed, and they are read only while the configuration file (the default `etc/ppanel.yaml` or the one
  given with `--config`) has no `JwtAuth.AccessSecret`; once the file has a secret they are ignored, so they may
  stay in the environment.
- The server writes the file back with a generated secret, the database connection (a DSN without parameters gets
  the `Config` defaults, carrying the `AppLocation` zone) and the Redis connection, keeping every other boot setting
  the file already had (`Host`, `Port`, `TLS`, `Logger`, `Trace`, `EdgeSubscribe`, ...). It then starts normally,
  applies the migrations and creates the first administrator (section 3.8).
- The file must be writable by the user the server runs as at that moment; the container image runs as uid 65532.

## 5. Health Checks

| Endpoint | Meaning | Response |
|---|---|---|
| `GET /healthz` | Liveness: the process and its listener are up. | `200` |
| `GET /readyz` | Readiness: start-up has completed and the database and Redis answer (the probe result is cached for 5 seconds). | `200`, or `503` with `{"status":"unavailable","reason":"…"}`, the reason being `database unreachable`, `redis unreachable`, `runtime bootstrap not finished` or `runtime bootstrap failed` |

Both are unauthenticated and disclose nothing beyond the reason, so they can be exposed to monitoring and load
balancers; point load-balancer health checks at `/readyz`. Neither is served while the setup wizard is running.

`ppanel-server healthcheck [--config etc/ppanel.yaml] [--timeout 3s]` reads the listener settings from the
configuration file and requests `/healthz` on it: `127.0.0.1:<Port>`, or `<Host>:<Port>` when `Host` is not
`0.0.0.0`, over HTTPS when `TLS.Enable` is on. It exits with a non-zero status when it gets no healthy answer
within the timeout. The container image's `HEALTHCHECK` runs it, and a systemd or cron watchdog can too.

## 6. Best Practices

- **Security**: Change the first administrator's password after the first sign-in. The server logs an error at
  startup while any administrator still uses the old default password `password`.
- **Reverse proxy**: A proxy on the same host or in a private network is trusted by default; a proxy with a public
  address goes into `TrustedProxies`, and a server its clients reach directly sets `TrustedProxies: ["none"]`; set
  `AllowedOrigins` to the frontends' origins.
- **Site host**: Set the site host in the system settings before enabling Apple or Telegram sign-in (3.10).
- **Logging**: Use `file` or `volume` mode for production to persist logs. Set `Level` to `error` to reduce log
  volume; `severe` writes nothing at all.
- **Database**: Ensure `Database` and `Redis` credentials are secure and not exposed in version control, and keep
  `etc/ppanel.yaml` at mode `0600`. Use `sslmode=require` or `verify-full` for a PostgreSQL server reached over a
  network.
- **JWT**: Specify a strong `AccessSecret` for `JwtAuth` to enhance security.

For further assistance, refer to the official PPanel documentation or contact support.
