# 2.0 LTS 验收记录

候选改动基于后端 `e8b1850e`、ppanel-node `3b7f5d65`、Terraris `8e5c6104`。以下结果针对这三个工作树中的改动。本轮按维护者要求跳过 PayPal Sandbox 买家实付和生产旧库副本演练。

| 议题 | 验收范围及证据 | 结果 |
| --- | --- | --- |
| [#228](https://github.com/perfect-panel/backend/issues/228) | 同一批次并发投递八次、订阅用量已提交但流量日志写入失败、重放、同 ID 不同内容、第二批累计。核对用户订阅、逐条日志及日报读取的汇总。真实 Terraris 在后端已接受第一批后丢失 HTTP 响应，重传原始 Protobuf 和 ID，新流量使用新 ID。 | PostgreSQL 17.12、MySQL 8.0.46 和 MariaDB 11.8.9 的回执与账务回归通过。Terraris 实际连通测试及 ppanel-node JSON/Protobuf 上报测试通过。 |
| [#225](https://github.com/perfect-panel/backend/issues/225) | 管理员编辑与 Telegram 禁用/启用均轮换会话；禁用期间与启用后检查真实请求鉴权。Redis 不可用时禁用可以生效，启用须等到能够撤销旧会话。重新登录签发的新会话可用。 | 通过。 |
| [#227](https://github.com/perfect-panel/backend/issues/227) | 轮换订阅 URL Token 和节点 UUID，检查订阅内容、节点用户列表、实际 VLESS 新连接与已建立的转发连接；覆盖禁用、退款、到期。另一个有效账户同时仍可转发数据。 | 通过。真实 Terraris 使用一秒拉取间隔，测试给配置拉取与应用留出十秒观察窗口。 |
| [#226](https://github.com/perfect-panel/backend/issues/226) | [epusdt `3506640`](https://github.com/GMWalletApp/epusdt/tree/3506640084f9a6e911e37f7e9b6bf61f14723b33) 使用真实 SQLite 订单、收银台、订单处理函数和签名回调 worker；测试事件模拟链上确认。后端验签、查单及 PostgreSQL 入账走实际实现。覆盖重复回调、队列故障后重投、丢失回调主动查单、任务重复执行及最终订阅/钱包余额。退款用例核对钱包余额和赠送余额各自的退款金额。 | 受控 GMPay 通过；PayPal Sandbox 实付跳过。钱包退款在 PPanel 的钱包账本落账，本轮未执行渠道原路退款。 |
| [#210](https://github.com/perfect-panel/backend/issues/210) | 隔离的新建数据库从仓库迁移到 `03025` 后执行跨模块回归。 | 从真实 `v1.21.7` 旧库副本升级、恢复备份及业务核数按要求跳过。 |

## 节点升级与存储

先部署带迁移 `03025_node_traffic_report` 的后端和流量 worker，再升级节点。节点继续使用 `POST /v1/server/push`，JSON 和 Protobuf 共用 HTTP 请求头 `Idempotency-Key`。每次发送一份新的不可变增量时生成 ID，失败重试携带原 ID 并保持条目顺序与数值。ID 为 1 到 64 个 ASCII 字母、数字、短横线或下划线，按服务器和协议区分。相同内容的不同批次必须用不同 ID。同一 ID 对应不同增量会得到 HTTP 409。

后端在第一次接受已标识批次时保存归属、按当时倍率折算的增量以及输入摘要，SQL 提交后返回成功。流量 worker 分别在用量事务与日志事务中更新同一回执的进度，日志提交后释放载荷。已完成的紧凑回执保留用于识别迟到的重传；应将回执纳入数据库备份和容量估算。可观察 `node_traffic_report` 中 `completed = false` 的数量和最早接收时间判断积压。

部署期间，不带请求头的旧节点仍走 Redis 分钟聚合，响应丢失后可能重复计量。向旧后端发送请求头也不提供去重保证。节点待发送增量保存在进程内存，进程崩溃时尚未提交给面板的字节不在本轮验收保证内。

## 复验

下列三个环境变量分别指向允许创建临时数据库的 PostgreSQL、MySQL、MariaDB 测试实例。每个测试用随机库名迁移和写入，在退出时删除该测试库。应用回归使用本地 Redis 协议测试服务；受控 GMPay 另使用上游自带的 SQLite 测试环境。

```sh
# 先设置 PPANEL_LTS_TEST_POSTGRES_DSN、PPANEL_LTS_TEST_MYSQL_DSN、PPANEL_LTS_TEST_MARIADB_DSN
go test -race -count=1 ./internal/module/identity/internal/{adminuser,authn,profile}
go test -race -count=1 -run '^TestLTS' ./internal/app -v
```

只设置 PostgreSQL DSN 即可运行账户和订阅联验。要检查真实数据面和丢失上报响应，先从修订后的 Terraris 工作树运行 `cargo build --locked --bin terraris`，将 `PPANEL_LTS_TERRARIS_BIN` 设为该二进制的绝对路径，再执行上面的应用测试。日志中的 `SKIP` 表示对应外部环境没有参与本次运行。

执行以下命令可单独运行 GMPay 双端联验。脚本只在临时目录构建测试程序，固定上游提交；本地回调接收器处理该支付方式的实际通知路径。

```sh
python3 scripts/acceptance/gmpay.py
```

节点仓库的重试回归可分别运行 `go test -race ./node ./api/panel`（ppanel-node）和 `cargo test --locked --test control_infrastructure --test panel_contract`（Terraris）。

已发布节点与第三方节点可先与新后端并行运行。完成运行中的节点升级前，不能把整个部署视为具备上报去重保证。
