# PPanel 配置指南

本文件为 PPanel 应用程序的配置文件提供全面指南。配置文件采用 YAML 格式，定义了服务器、日志、链路追踪、数据库、Redis 和管理员访问的相关设置。

## 1. 配置文件概述

- **默认路径**：`./etc/ppanel.yaml`
- **自定义路径**：通过启动参数 `--config` 指定配置文件路径。
- **格式**：YAML 格式，支持注释，文件名需以 `.yaml` 结尾。
- **权限**：文件中保存着 JWT 密钥和数据库凭据，请只允许运行服务的用户读取（`chmod 0600`）；安装向导和环境变量安装（第 4 节）写入文件时也使用该权限。

## 2. 配置文件结构

以下是配置文件示例，包含默认值和说明：

```yaml
# PPanel 配置文件
Host: "0.0.0.0"                     # 服务监听地址
Port: 8080                          # 服务监听端口
Debug: false                        # 是否开启调试模式（禁用后台日志）
TrustedProxies: []                  # 信任其 X-Forwarded-For 的反向代理；空 = 回环与私有网段，["none"] = 不信任任何代理
AllowedOrigins: []                  # CORS 允许的浏览器来源；空 = 原样反射请求的 Origin
HTTP: # 监听器限制
  ReadTimeoutSeconds: 180           # 读取请求的时限；0 = 不限
  WriteTimeoutSeconds: 0            # 写出响应的时限；0 = 不限（流式接口需要）
  IdleTimeoutSeconds: 180           # keep-alive 连接空闲多久后关闭
  MaxRequestBodyMB: 4               # 接受的最大请求体（MB）
AppLocation: "Asia/Shanghai"        # 应用时区（见 3.1）
TLS: # 由服务自身提供 HTTPS（通常交给反向代理）
  Enable: false
  CertFile: ""
  KeyFile: ""
JwtAuth: # JWT 认证配置
  AccessSecret: ""                  # 访问令牌密钥（必填，见 3.2）
  AccessExpire: 604800              # 访问令牌过期时间（秒）
Logger: # 日志配置
  ServiceName: "PPanel"             # 日志服务标识名称
  Mode: "file"                      # 日志输出模式（console、file、volume）
  Encoding: "json"                  # 日志格式（json、plain）
  TimeFormat: "2006-01-02 15:04:05.000"  # 自定义时间格式
  Path: "logs"                      # 日志文件目录
  Level: "info"                     # 日志级别（debug、info、error、severe）
  Compress: false                   # 是否压缩日志文件
  KeepDays: 30                      # 日志保留天数
  StackCooldownMillis: 100          # 堆栈日志冷却时间（毫秒）
  MaxBackups: 30                    # 最大日志备份数
  MaxSize: 100                      # 最大日志文件大小（MB）
  Rotation: "daily"                 # 日志轮转策略（daily、size）
Trace: # OpenTelemetry 链路追踪（见 3.4）
  Name: ""                          # 记录在 trace 中的服务名
  Endpoint: ""                      # 收集器地址；空 = 不导出
  Sampler: 0.1                      # 采样比例
  Batcher: "jaeger"                 # jaeger、zipkin、otlpgrpc、otlphttp 或 file
  OtlpGrpcSecure: false             # otlpgrpc 导出器是否使用 TLS；false = 明文 gRPC
  OtlpHeaders: {}                   # OTLP 导出附加的请求头
  OtlpHttpPath: ""                  # Endpoint 为 host:port 时 otlphttp 的路径
  OtlpHttpSecure: false             # Endpoint 为 host:port 时 otlphttp 是否使用 HTTPS
  Disabled: false                   # true = 关闭链路追踪
Database: # MySQL、MariaDB 或 PostgreSQL 数据库配置
  Driver: "mysql"                   # mysql 或 postgres
  Addr: ""                          # 数据库地址（必填）
  Username: ""                      # 数据库用户名（必填）
  Password: ""                      # 数据库密码（必填）
  Dbname: ""                        # 数据库名（必填）
  Config: "charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai&interpolateParams=true"  # 对应数据库的连接参数
  MaxIdleConns: 10                  # 最大空闲连接数
  MaxOpenConns: 10                  # 最大打开连接数
  ConnMaxLifetime: 1800             # 连接最大生命周期（秒）
  ConnMaxIdleTime: 300              # 空闲连接最大保留时间（秒）
  SlowThreshold: 1000               # 慢查询阈值（毫秒）
GeoIP: # MaxMind GeoLite2 城市库，为审计日志中的地址标注位置（见 3.6）
  Path: "./cache/GeoLite2-City.mmdb"
  Download: true                    # Path 不存在时自动下载
  DownloadURL: ""                   # 空 = 内置镜像
  SHA256: ""                        # 文件应有的十六进制摘要；空 = 不校验
  Required: false                   # true = 没有可用的库则拒绝启动
Redis: # Redis 配置
  Host: "localhost:6379"            # Redis 地址
  Username: ""                      # ACL 用户名，留空使用 default
  Pass: ""                          # Redis 密码
  KeyPrefix: ""                     # 例如 panel-a，生成 panel-a: 开头的键
  DB: 0                             # 缓存与会话使用的 Redis 数据库
  QueueDB: 5                        # 任务队列使用的 Redis 数据库
Administrator: # 首位管理员，仅在首次启动时创建
  Email: "admin@ppanel.dev"         # 管理员登录邮箱
  Password: ""                      # 管理员登录密码，留空则自动生成
```

## 3. 配置项说明

### 3.1 服务器设置

- **`Host`**：服务监听的地址。
  - 默认：`0.0.0.0`（监听所有网络接口）。
  - 它只是绑定地址。支付回调等对外链接由站点设置中的站点地址（见 3.10）或支付方式自己的域名生成，OAuth 重定向也固定到站点地址。
- **`Port`**：服务监听的端口。
  - 默认：`8080`。
  - 容器镜像以非特权用户运行，无法绑定 1024 以下的端口：需要低端口时请用端口映射（`-p 443:8080`），不要修改 `Port`。
- **`Debug`**：是否开启调试模式，开启后禁用后台日志功能。
  - 默认：`false`。
- **`TrustedProxies`**：可信的反向代理，其 `X-Forwarded-For` / `X-Real-IP` 头被视为真实客户端地址；填 IP 或 CIDR，例如
  `["127.0.0.1", "10.0.0.0/8"]`。
  - 默认：回环接口与私有网段（`127.0.0.0/8`、`::1/128`、`10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`、`fc00::/7`），因此同机的 nginx、Docker 网桥或内网负载均衡无需配置即被信任。显式配置会替换默认值；条目 `private` 代表默认网段（如 `["private", "203.0.113.10"]` 在默认之上增加一个公网代理），`["none"]` 表示不信任任何请求头，用于客户端直接访问的服务。
  - 客户端地址用于登录与审计日志以及所有按 IP 的限制（验证码、设备登录、可用性检查、订阅拉取、注册）。代理地址不在默认网段内时（公网地址的负载均衡或 Cloudflare 隧道），请把该地址填在这里，否则所有客户端都会显示为代理的地址，限制也会作用于全部客户端；对应的 nginx 配置见 [install-zh.md 的 NGINX 反向代理配置](install-zh.md#nginx-反向代理配置)。只填代理，不要填客户端：`0.0.0.0/0` 之类的配置会让任何人自行决定服务记录的地址。
- **`AllowedOrigins`**：CORS 允许的浏览器来源，格式 `scheme://host[:port]`，例如
  `["https://user.example.com", "https://admin.example.com"]`。
  - 默认：空，保持宽松的旧行为：原样反射请求的 `Origin`，任何网站的脚本都能在浏览器中调用 API。
  - 设置后，列表中的来源按精确匹配（不区分大小写）获得 CORS 头并允许携带凭据；未列出的来源不会收到任何 CORS 头，浏览器因此拦截调用。设备 WebSocket（`/v1/app/ws/...`）对浏览器客户端应用同一列表。
  - 生产环境请填写用户端和管理端前端的来源。不带 `Origin` 头的请求（客户端、节点、订阅）不受影响。
- **`HTTP`**：API 监听器的限制。默认值就是这些选项可配置之前服务一直使用的值。
  - **`ReadTimeoutSeconds`**：读取一个请求（头和体）的时限。默认 `180`；`0` = 不限。
  - **`WriteTimeoutSeconds`**：写出响应的时限。默认 `0` = 不限，流式接口（SSE 订单事件、节点 WebSocket）需要如此，设置上限会切断它们。
  - **`IdleTimeoutSeconds`**：keep-alive 连接空闲多久后关闭。默认 `180`。
  - **`MaxRequestBodyMB`**：接受的最大请求体（MB），更大的请求在读取前即被拒绝。默认 `4`，足够所有 API 请求，包括上传的 logo 和模板。
- **`AppLocation`**：应用使用的 IANA 时区，"今天"、到期提醒、流量重置周期和每日统计都按它计算。
  - 默认：`Asia/Shanghai`。
  - 任何 IANA 时区名都可用：二进制内嵌了时区数据库（`time/tzdata`），宿主机和容器镜像都不需要 zoneinfo 文件。
  - 服务启动时还会把它设为进程时区（不论 `TZ` 如何设置），因此写入的所有时间（包括自动填写的 `created_at` /
    `updated_at`）都在同一个时钟上。
  - 必须与数据库时区（`Database.Config` 中 MySQL 的 `loc`、PostgreSQL 的 `TimeZone`）一致：时间按数据库时区存储，
    统计也按数据库时区分天。安装页以及通过 `PPANEL_DB` / `PPANEL_REDIS` 环境变量完成的安装都会把 `AppLocation`
    的时区写进新数据库的连接参数；对已有数据库，两者不一致时服务启动会记录错误日志。只能在空库上，或把已存储的时间换算之后再修改数据库时区，否则所有已存时间都会被重新解读。
- **`TLS`**：由服务自身提供 HTTPS。
  - **`Enable`**：默认 `false`。通常由反向代理终结 TLS，再把代理填入 `TrustedProxies`。
  - **`CertFile`**、**`KeyFile`**：PEM 格式证书链与私钥的路径，需要运行服务的用户可读。

### 3.2 JWT 认证 (`JwtAuth`)

- **`AccessSecret`**：访问令牌的密钥。会话、订单事件凭据和游客结账签名都由它派生，为空则任何人都能伪造。
  - 必填：为空时服务启动即退出；短于 16 个字符时会记录警告。请使用足够长的随机值。
  - 只有全新安装会自动生成：配置文件没有密钥时，安装向导（或用 `PPANEL_DB` / `PPANEL_REDIS` 环境变量补全配置的流程，见第 4 节）会生成一个并写入文件；预先填写的配置文件必须自带密钥。
- **`AccessExpire`**：令牌过期时间（秒）。
  - 默认：`604800`（7天）。

### 3.3 日志配置 (`Logger`)

- **`ServiceName`**：日志的服务标识名称，在 `volume` 模式下用作日志目录名。
  - 默认：`PPanel`。
- **`Mode`**：日志输出方式。
  - 选项：`console`（标准输出/错误输出）、`file`（写入指定目录）、`volume`（Docker 卷）。
  - 默认：`file`。
- **`Encoding`**：日志格式。
  - 选项：`json`（结构化 JSON）、`plain`（纯文本，带颜色）。
  - 默认：`json`。
- **`TimeFormat`**：日志时间格式。
  - 默认：`2006-01-02 15:04:05.000`。
- **`Path`**：日志文件（`access.log`、`error.log`、`slow.log`）的存储目录，相对于工作目录。
  - 默认：`logs`。
- **`Level`**：日志过滤级别。
  - 选项：`debug`（全部）、`info`（除 debug 外的全部）、`error`（仅错误、慢查询和堆栈）、`severe`（不输出任何日志：
    服务没有高于 `error` 的日志，这一级别相当于关闭日志）。
  - 默认：`info`。
- **`Compress`**：是否压缩日志文件（仅在 `file` 模式下生效）。
  - 默认：`false`。
- **`KeepDays`**：日志文件保留天数。
  - 默认：`30`。
- **`StackCooldownMillis`**：堆栈日志冷却时间（毫秒），防止日志过多。
  - 默认：`100`。
- **`MaxBackups`**：最大日志备份数量（仅在 `size` 轮转时生效）。
  - 默认：`30`。
- **`MaxSize`**：日志文件最大大小（MB，仅在 `size` 轮转时生效）。
  - 默认：`100`。
- **`Rotation`**：日志轮转策略。
  - 选项：`daily`（按天轮转）、`size`（按大小轮转）。
  - 默认：`daily`。

### 3.4 链路追踪 (`Trace`)

对 HTTP 请求、数据库调用和队列任务做 OpenTelemetry 追踪。未设置 `Endpoint` 时不会导出任何数据。

- **`Name`**：记录在 trace 中的服务名（`service.name`）。
  - 默认：空；导出时请填一个名字，例如 `ppanel`。
- **`Endpoint`**：batcher 发送 span 的目标；为空则不导出。
  - `jaeger` batcher 使用 Jaeger 的 OTLP/HTTP 接收端：可以是 `http://jaeger:4318` 这样的 URL（未指定路径时提交到
    `/v1/traces`，由 scheme 决定是否 TLS），也可以是纯 `host:port`。Jaeger 从 1.35 起原生接收 OTLP；旧 Jaeger Thrift
    导出器的地址（`udp://host:6831`、`http://host:14268/api/traces`）会被改写为同一主机 4318 端口的 OTLP，并在日志中给出警告。
  - `otlpgrpc` 填收集器的 gRPC 地址（`host:4317`）；`otlphttp` 填 URL 或 `host:port`（配合 `OtlpHttpPath`、
    `OtlpHttpSecure`）；`zipkin` 填 Zipkin API 地址；`file` 填文件路径。
- **`Sampler`**：保留的 trace 比例，`0` 到 `1`。
  - 默认：`0.1`。
- **`Batcher`**：导出器：`jaeger`、`zipkin`、`otlpgrpc`、`otlphttp` 或 `file`。
  - 默认：`jaeger`。
  - `zipkin` 使用的导出器已被上游弃用，新部署请选择 OTLP 端点（`otlpgrpc`、`otlphttp` 或 `jaeger`）。
- **`OtlpGrpcSecure`**：`otlpgrpc` 导出器的传输安全。
  - 默认：`false`：与收集器之间的 gRPC 连接不使用 TLS，与服务一直以来的行为相同。收集器提供 TLS 时设为 `true`，使用系统根证书校验。`otlphttp` 通过 URL scheme 或 `OtlpHttpSecure` 决定是否 TLS。
- **`OtlpHeaders`**：每次 OTLP 导出附带的请求头，例如托管收集器需要的 `uptrace-dsn` 或 `Authorization`。
- **`OtlpHttpPath`**：`Endpoint` 为纯 `host:port` 时 OTLP/HTTP 接收端的路径，例如 `/v1/traces`。
- **`OtlpHttpSecure`**：`Endpoint` 为纯 `host:port` 时 `otlphttp` 是否使用 HTTPS。
  - 默认：`false`。
- **`Disabled`**：无视其他设置，完全关闭链路追踪。
  - 默认：`false`。

### 3.5 数据库 (`Database`)

- **`Driver`**：数据库类型，可选 `mysql` 或 `postgres`。MySQL 8 与 MariaDB 11.8 均使用 `mysql`。
  - 默认：`mysql`。
- **`Addr`**：数据库服务器地址。
  - 必填。
- **`Username`**：数据库用户名。
  - 必填。
- **`Password`**：数据库密码。
  - 必填。
- **`Dbname`**：数据库名。
  - 必填。
- **`Config`**：对应数据库的连接参数。
  - MySQL 默认：`charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai&interpolateParams=true`。
  - PostgreSQL 默认：`sslmode=prefer&TimeZone=<AppLocation>&application_name=perfect-panel`，例如
    `sslmode=prefer&TimeZone=Asia/Shanghai&application_name=perfect-panel`。
  - PostgreSQL：`prefer` 在服务器提供 TLS 时加密连接，但不校验服务器证书；服务器不提供 TLS 时会静默回退到明文。经网络访问的数据库请显式设置
    `sslmode=verify-full`（配合服务器 CA 证书），至少也要 `sslmode=require`；`sslmode=disable` 只适合同一主机或内网中的数据库。
  - PostgreSQL：`Config` 中没有 `TimeZone` 时会自动补上 `TimeZone=<AppLocation>`。自定义了 `Config` 且 `AppLocation` 不是
    `Asia/Shanghai` 的部署，仍请显式写上 `TimeZone=<AppLocation>`；无法确定数据库时区时服务会在启动日志中记录错误。
  - 这些参数中的时区即数据库时区，见 `AppLocation`。
- **`MaxIdleConns`**：最大空闲连接数。
  - 默认：`10`。
- **`MaxOpenConns`**：最大打开连接数。
  - 默认：`10`。
- **`ConnMaxLifetime`**：连接池中单个连接的最大复用时间（秒）。
  - 默认：`1800`。
- **`ConnMaxIdleTime`**：空闲连接在连接池中的最大保留时间（秒）。
  - 默认：`300`。
- **`SlowThreshold`**：慢查询阈值（毫秒）。
  - 默认：`1000`。

### 3.6 GeoIP (`GeoIP`)

MaxMind GeoLite2 城市库为登录与审计日志中的地址标注位置。没有它服务照常运行，只记录地址本身。

- **`Path`**：`GeoLite2-City.mmdb` 文件的路径，相对于工作目录。ASN 库（`GeoLite2-ASN.mmdb`）应放在同一目录中。
  - 默认：`./cache/GeoLite2-City.mmdb`。容器镜像为它准备了可写的 `/app/cache`。
- **`Download`**：`Path` 处没有文件或文件未通过 `SHA256` 校验时，启动时自动下载城市库。`DownloadURL` 为空时还会从内置镜像下载 ASN 库；
  自定义了 `DownloadURL` 时只下载城市库，ASN 库需要手动放到城市库旁边。
  - 默认：`true`。
- **`DownloadURL`**：城市库的下载地址。
  - 默认：空，使用内置镜像（GitHub 上发布的一份 GeoLite2-City 副本）。想控制服务加载的内容，可以指向自己托管的副本，或带许可密钥的 MaxMind 下载链接。
- **`SHA256`**：城市库应有的十六进制 SHA-256 摘要。已存在的文件摘要不符时，`Download` 开启则重新下载；下载得到的文件摘要不符时绝不会替换现有文件。
  - 默认：空，不校验。`DownloadURL` 指向自己控制的文件时建议设置。
- **`Required`**：城市库缺失或无效时拒绝启动。
  - 默认：`false`：服务在没有地理位置的情况下启动，并在日志中说明原因。

### 3.7 Redis 配置 (`Redis`)

- **`Host`**：Redis 服务器地址。
  - 默认：`localhost:6379`。
- **`Username`** 用于业务和队列连接的 ACL 认证，留空时使用默认用户。
- **`Pass`**：Redis 密码。
  - 默认：`""`（无密码）。
- **`DB`**：缓存、会话与限流使用的 Redis 数据库索引。
  - 默认：`0`。
- **`QueueDB`**：任务队列（asynq）使用的 Redis 数据库索引，生产者、消费者与调度器共用。
  - 默认：`5`，即队列一直使用的数据库，升级后已排队的任务得以保留。多个部署共用相同数据库时应使用互不重叠的项目前缀。

- **`KeyPrefix`** 为缓存、会话、限流、任务队列、worker 元数据及 Pub/Sub 频道统一添加项目前缀。填 `panel-a` 或 `panel-a:` 都会生成 `panel-a:` 开头的名称，例如 `panel-a:asynq:queues`。相应的 ACL 键规则为 `~panel-a:*`，频道规则为 `&panel-a:*`。
  留空时沿用原有名称。启用或更换前缀会切换到新的键空间，现有会话和待处理任务仍在旧键下。使用队列检查工具时也要指定同一前缀，PPanel 所用的 [Asynq 分支](https://github.com/pars-aria-labs/asynq/tree/v1.2.0) 提供带 `--prefix` 的 CLI。切换前应先处理完待执行任务或迁移数据，并同步更新生产者和消费者。

单实例 Dragonfly v2.1 可沿用上述 Redis 配置。启动 Dragonfly 时加上 `--cluster_mode=emulated --lock_on_hashtags` 即可，节点缓存清理无需 Lua 脚本白名单。滚动升级时，等所有旧版后端实例退出后，再移除旧清理脚本的白名单。

### 3.8 管理员登录 (`Administrator`)

用来创建首位管理员：每次启动时，只要数据库中还没有管理员，就按这里的值创建一个。安装向导会把安装者的邮箱写入这里，绝不会写入密码。

- **`Email`**：管理员登录邮箱。
  - 默认：`admin@ppanel.dev`。
- **`Password`**：管理员登录密码。
  - 默认：空。留空时会生成随机密码并在启动日志（`docker logs`、`journalctl -u ppanel`）中打印一次，请用它登录后立即修改；
    填写了则按填写的值使用。
  - 管理员创建之后请把它删掉：管理员已存在而文件里仍留有 `Password` 时，每次启动都会在日志中提示，因为文件中保存着一份无人需要的凭据。

### 3.9 邮件发送（SMTP）

SMTP 中继不在本文件中配置：管理员在面板的系统设置（邮件）里填写，配置保存在数据库中。字段如下：

| 字段 | 含义 |
|---|---|
| `host`、`port` | 中继地址。465 端口为隐式 TLS（SMTPS）；25、587、2525 以明文开始，再通过 STARTTLS 升级加密。 |
| `user`、`pass` | 中继凭据；留空则不认证。 |
| `from`、`reply_to` | 发件地址与可选的回复地址。显示名称取站点名称。 |
| `ssl` | 要求加密。465 端口（或开启 `implicit_tls`）时连接一开始就是 TLS；其他端口必须通过 STARTTLS 升级，中继不提供 STARTTLS 时会在发送凭据之前拒绝。Mailgun、SendGrid、Postmark、Brevo 的 STARTTLS 端口（587、2525）就用这个设置。关闭时，中继提供 STARTTLS 就用，否则保持明文。 |
| `implicit_tls` | 在 465 之外的端口上以 TLS 握手开始连接。仅用于在非标准端口提供 SMTPS 的中继；不要用于 STARTTLS 端口，否则握手会撞上明文问候，邮件发不出去。 |
| `insecure_skip_verify` | 接受任何中继证书。仅用于自签名证书的中继；默认会校验证书。 |

### 3.10 站点地址（系统设置）

`Site.Host` 同样不在本文件中：管理员在面板的系统设置（站点）里填写，是面板对外的公开地址，例如 `https://panel.example.com`。支付回调由它生成，Apple 与 Telegram 登录方式的 OAuth 重定向也固定到它。启用这两种登录方式之一时必须设置它：为空时，只有停留在 API 自身主机（同一主机、其子域名或父域名）上的重定向才会被接受，否则登录被拒绝，并且服务启动时会记录错误日志，指出受影响的登录方式。

## 4. 环境变量

两个环境变量可以在首次启动时补全一份空的配置文件，供容器和无人值守安装使用——这些场景下只监听 `127.0.0.1` 的安装向导无法访问：

| 环境变量 | 配置项 | 格式 | 示例 |
|----------------|----------|------|------|
| `PPANEL_DB` | `Database` | MySQL DSN `user:password@tcp(host:port)/dbname[?params]`，或 URL：`mysql://user:password@host:3306/dbname`、`postgres://user:password@host:5432/dbname[?sslmode=require]` | `ppanel:secret@tcp(127.0.0.1:3306)/ppanel` |
| `PPANEL_REDIS` | `Redis` | `redis://[username:password@]host[:port][/db][?prefix=project]`（省略时端口为 `6379`、数据库为 `0`） | `redis://panel:secret@127.0.0.1:6379/0?prefix=panel-a` |

- 两者都必须设置，且只在配置文件（默认的 `etc/ppanel.yaml` 或 `--config` 指定的文件）还没有 `JwtAuth.AccessSecret`
  时读取；文件一旦有了密钥就会忽略它们，因此可以一直留在环境中。
- 服务会把文件写回：包含生成的密钥、数据库连接（DSN 不带参数时使用 `Config` 的默认值，带有 `AppLocation` 的时区）和
  Redis 连接，并保留文件中已有的其他启动配置（`Host`、`Port`、`TLS`、`Logger`、`Trace`、`EdgeSubscribe` 等）。随后正常启动、执行迁移并创建首位管理员（见 3.8）。
- 此时运行服务的用户必须对该文件有写权限；容器镜像以 uid 65532 运行。

## 5. 健康检查

| 端点 | 含义 | 响应 |
|---|---|---|
| `GET /healthz` | 存活探针：进程及其监听器已启动。 | `200` |
| `GET /readyz` | 就绪探针：启动流程已完成，且数据库与 Redis 均有响应（探测结果缓存 5 秒）。 | `200`，否则 `503` 并返回 `{"status":"unavailable","reason":"…"}`，reason 为 `database unreachable`、`redis unreachable`、`runtime bootstrap not finished` 或 `runtime bootstrap failed` 之一 |

两者都不需要认证，除原因外不泄露任何信息，可以开放给监控与负载均衡；负载均衡的健康检查请指向 `/readyz`。安装向导运行期间两者都不提供。

`ppanel-server healthcheck [--config etc/ppanel.yaml] [--timeout 3s]` 从配置文件读取监听设置并请求其上的 `/healthz`：
`127.0.0.1:<Port>`，`Host` 不是 `0.0.0.0` 时为 `<Host>:<Port>`，开启 `TLS.Enable` 时使用 HTTPS。超时内没有得到健康的响应即以非零状态退出。
容器镜像的 `HEALTHCHECK` 就运行它，systemd 或 cron 的看门狗也可以使用。

## 6. 最佳实践

- **安全性**：首次登录后请修改首位管理员的密码。只要还有管理员在使用旧默认密码 `password`，服务启动时都会记录错误日志。
- **反向代理**：同机或私有网段内的代理默认即被信任；公网地址的代理需填入 `TrustedProxies`，客户端直接访问的服务则设置 `TrustedProxies: ["none"]`；把前端来源填入 `AllowedOrigins`。
- **站点地址**：启用 Apple 或 Telegram 登录之前，先在系统设置中填写站点地址（见 3.10）。
- **日志**：生产环境中建议使用 `file` 或 `volume` 模式持久化日志，将 `Level` 设置为 `error` 以减少日志量；`severe`
  会关闭全部日志输出。
- **数据库**：确保 `Database` 和 `Redis` 凭据安全，避免在版本控制中暴露，并把 `etc/ppanel.yaml` 保持为 `0600`。经网络访问的 PostgreSQL 请使用 `sslmode=require` 或 `verify-full`。
- **JWT**：为 `JwtAuth` 的 `AccessSecret` 设置强密钥以增强安全性。

如需进一步帮助，请参考 PPanel 官方文档或联系支持团队。
