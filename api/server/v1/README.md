# Server HTTP Protobuf 接口

此目录的 [server.proto](server.proto) 为节点 Server 接口提供可选的
Protobuf 二进制传输，覆盖以下端点：

- `POST /v1/server/online`
- `POST /v1/server/push`
- `POST /v1/server/status`
- `GET /v1/server/config`
- `GET /v1/server/user`
- `GET /v1/server/alivelist`
- `POST /v1/server/device`
- `GET /v2/server/{server_id}`

上报类请求使用 `Content-Type: application/protobuf` 发送 Protobuf body；
增加 `Accept: application/protobuf` 可请求 Protobuf 响应。未指定时仍默认使用
JSON。GET 接口只需使用 `Accept` 协商响应格式。既有的 `server_id`、`protocol`
与 `secret_key` 查询参数保持不变。

`GET /v1/server/alivelist` 返回当前 server/protocol 可服务订阅的全局在线来源 IP 数，
JSON 为 `{"alive":{"订阅ID":数量}}`，无在线设备时为 `{"alive":{}}`。不使用 ETag，
避免在线数据过期后仍沿用旧计数。用户 ID 与 `GET /v1/server/user` 的订阅 ID 一致。

`POST /v1/server/device` 接收 JSON `{"uid":订阅ID,"ip":"来源IP"}` 或
`ClaimServerDeviceRequest`，返回 `{"allowed":true/false,"ttl_seconds":300}` 或
`ClaimServerDeviceResponse`。身份由 query 参数确定，body 不能覆盖 server/protocol。
Redis 原子检查同订阅的独立 IP 数并申请/续期五分钟租约；同 IP 跨节点或协议去重，
IPv4-mapped IPv6 归一化为 IPv4。设备限制为 0 时不限名额，其他情况按当前用户列表
中的设备限制准入；已获名额的 IP 可续期。在线上报也续期租约，失联条目自动过期。
单次在线上报最多 10000 项，去重后全部续期；分批上报按 IP 合并，不覆盖前一批。

所有成功的 Protobuf 信封均使用 `code = 200` 和 `message = "success"`。失败响应
使用 `Result`；带业务数据的成功响应使用强类型 `data` 字段。
`GET /v2/server/{server_id}` 定义了与既有 PPanel 节点对接一致的 DNS、出站和
协议消息。只有 `plugin_options` 使用 `google.protobuf.Value`，以保留对象和
SIP003 字符串两种插件参数形式。

Nowhere 节点使用 `GET /v2/server/{server_id}` 获取扁平协议配置，并复用现有的
用户、状态和流量接口。服务端配置固定为 TLS，版本为 1；`network` 为 `mix`、
`tcp` 或 `udp`，ALPN 默认且只能包含一个 `now/1`。用户列表中的 UUID 直接作为
Nowhere shared key。旧版 `GET /v1/server/config` 不定义 Nowhere 配置结构。

修改 schema 后，在仓库根目录运行 `make proto` 重新生成 Go 绑定。它要求
protoc 版本与 CI 一致（`make -s print-PROTOC_VERSION`，即 libprotoc 3.21.12），
并按 go.mod 中锁定的版本构建 protoc-gen-go，保证生成文件头与提交内容一致。
CI 的 `make proto-check` 会重新生成并在与提交的 `server.pb.go` 不一致时失败。
