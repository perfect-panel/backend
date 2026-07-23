# V2 下单、支付与订单事件流设计

## 目标

将目前面向前端的“报价（可选）→ 创建订单 → Checkout → 第三方支付”收敛为一次 V2 下单请求。服务端仍保持订单创建与第三方支付发起两个独立的可靠步骤，避免把网络调用放进数据库事务。

V2 必须同时满足：

- 一次前端提交返回订单与支付载荷；
- 同一个客户端重试不会重复预占库存、优惠券或赠金；
- 支付回调和异步履约通过 SSE 推送状态，不需要固定间隔轮询；
- V1 API 与既有订单保持兼容。

不在本期把第三方支付、订单创建和异步履约强行合并为一个数据库事务，也不在支付完成前把订单视为已履约。

## 现有基础与边界

现有 `order` 已保存订单状态、支付渠道、支付金额/币种快照、交易号、优惠券预占与访客 Checkout capability。网关回调将订单从 `Pending` 标记为 `Paid`，激活队列再将其置为 `Finished`；Paid 状态和定时对账可修复数据库与队列之间的短暂故障。

V2 复用这些事实来源。新增 SSE 只通知已提交的状态，不能成为支付或履约的事实来源。

## API

### 创建订单并发起支付

```http
POST /v2/public/orders
Idempotency-Key: 550e8400-e29b-41d4-a716-446655440000
Content-Type: application/json
```

认证用户可以创建四种订单；未认证访问仅允许 `purchase`，且必须提供 `guest`。请求中不接受价格、手续费、优惠金额或支付金额，所有金额由服务端重新计算。

```json
{
  "type": "purchase",
  "payment_id": 3,
  "subscribe_id": 12,
  "quantity": 1,
  "coupon": "SUMMER",
  "return_url": "https://app.example.com/payment/result",
  "guest": {
    "auth_type": "email",
    "identifier": "user@example.com",
    "password": "client-provided-secret",
    "invite_code": "OPTIONAL"
  }
}
```

按 `type` 验证字段：

| `type` | 必填字段 | 不允许字段 |
| --- | --- | --- |
| `purchase` | `subscribe_id`、`quantity`、`payment_id` | `user_subscribe_id`、`amount` |
| `renewal` | `user_subscribe_id`、`quantity`、`payment_id` | `subscribe_id`、`amount`、`guest` |
| `reset_traffic` | `user_subscribe_id`、`payment_id` | `subscribe_id`、`quantity`、`amount`、`guest` |
| `recharge` | `amount`、`payment_id` | `subscribe_id`、`user_subscribe_id`、`quantity`、`coupon`、`guest` |

`return_url` 只用于渠道支付完成后的浏览器跳转，不能作为回调地址或授权依据。

成功响应：

```json
{
  "order": {
    "order_no": "202607230001",
    "status": "pending_payment",
    "fulfillment_status": "not_started",
    "amount": 1200,
    "currency": "CNY",
    "expires_at": 1784798100
  },
  "payment": {
    "type": "qr",
    "checkout_url": "https://..."
  },
  "events": {
    "url": "/v2/public/orders/202607230001/events?ticket=...",
    "ticket_expires_at": 1784798100
  },
  "checkout_token": "guest-only-capability"
}
```

`payment.type` 为 `url`、`qr`、`stripe` 或 `balance`。Stripe 使用 `client_secret` 字段；余额支付返回 `payment_status: "paid"`，但只有收到履约事件后才代表订阅或余额已经可用。

`checkout_token` 仅在访客新购时返回。认证用户不需要它。

### 订单事件流

```http
GET /v2/public/orders/{orderNo}/events?ticket={short-lived-ticket}
Accept: text/event-stream
Last-Event-ID: 42
```

浏览器应在取得下单响应后先建立 SSE，再显示二维码、跳转地址或 Stripe 支付页。连接建立时服务端发送当前快照；之后发送状态事件。`Last-Event-ID` 用于断线重连补发。

```text
id: 42
event: order.payment_paid
data: {"order_no":"202607230001","payment_status":"paid","fulfillment_status":"pending"}

id: 43
event: order.fulfilled
data: {"order_no":"202607230001","payment_status":"paid","fulfillment_status":"finished"}
```

固定事件集合：

| 事件 | 触发事务 | 前端含义 |
| --- | --- | --- |
| `order.created` | Pending 订单已提交 | 可以开始支付 |
| `order.payment_paid` | Pending → Paid 已提交 | 已收款，等待异步履约 |
| `order.fulfilled` | Paid → Finished 已提交 | 订阅/余额已可用 |
| `order.closed` | Pending → Closed 已提交 | 超时、取消或支付未完成 |

不为队列临时重试发送“失败”事件；订单仍为 Paid 时，前端显示“正在处理”，其最终状态由 `fulfilled` 或人工处理决定。

SSE 响应设置 `Cache-Control: no-cache`、`X-Accel-Buffering: no`，每 20 秒发送心跳。流令牌为短期签名 capability，绑定 `order_no`、所有者或访客 capability hash、过期时间和 `events:read` scope。它只能用于该订单事件流；不得使用长期 Bearer Token 作为 URL 参数。

### 断线、多连接与重放语义

SSE 是至少一次通知，不是恰好一次消息队列。客户端必须以订单状态和事件 id 为准，不得因为收到重复事件重复展示成功页、重复请求履约或重复发起支付。

| 场景 | 服务端行为 | 客户端行为 |
| --- | --- | --- |
| 支付前 SSE 尚未建立或意外断开 | 订单状态和事件已写入数据库，不依赖连接存在 | 建连后取得快照与历史事件 |
| 浏览器自动重连 | 原生 `EventSource` 自动携带最后收到的 `Last-Event-ID` | 忽略 `id <= last_seen_id` 的事件 |
| 页面刷新或新开标签页 | 允许 `?after={event_id}`；没有游标时发送当前快照 | 持久化每个订单的最后 event id，或接受快照重置 |
| 同一订单多条 SSE 连接 | 同一 ticket 最多允许 3 条并发连接，超过返回 `429` | 多标签页可独立消费；同页只保留一个连接 |
| HTTP/SSE 服务重启或 Redis 订阅断开 | handler 重新订阅 Redis，并从最后已发送 id 查询 `order_event` | 浏览器重连；状态不会因连接断开而丢失 |
| outbox 已提交但 Redis 发布失败 | 发布器重试与周期扫描补发；SSE 查询可直接读取未发布事件 | 不把短暂没有实时消息视为支付失败 |
| 重复广播或查询与广播重叠 | 允许重复发送同一 event id | 以 event id 去重，状态按版本单调前进 |
| 游标早于事件保留期 | 返回 `event: order.reset` 与当前完整快照，而非静默缺失 | 丢弃旧游标，从快照重新建立本地状态 |

连接建立必须遵循“**授权 → 订阅 Redis → 查询并发送 `id > after` 的事件 → 转发实时消息**”的顺序。先订阅可消除“历史查询完成到开始监听之间恰好发生支付”的漏事件窗口；查询与订阅重叠导致的重复由 event id 消除。

每个事件 id 都来自 `order_event.id`，且 payload 包含 `state_version`、`payment_status` 与 `fulfillment_status`。同一订单的状态转移被条件更新/行锁串行化，并在同一事务中递增 `state_version`；只有真正提交状态变更的事务可写事件；支付回调与关单竞争时，败方不产生事件。

stream ticket 的有效期覆盖订单剩余支付窗口并额外保留 10 分钟。服务端在 ticket 到期时发送 `event: stream.expiring` 后关闭连接；客户端通过下列接口以订单所有权或访客 checkout capability 换取新 ticket，而不重建订单：

```http
POST /v2/public/orders/{orderNo}/event-ticket
```

事件保留期为 30 天；清理任务只能删除已发布且超过保留期的事件。订单详情接口始终是快照来源，因此即使客户端长期离线也能恢复到正确的最终状态。

匿名新购在激活后会创建正式账户，但原始 checkout capability 仍可用于该订单的状态恢复、SSE ticket 刷新和会话兑换；它不因 `order.user_id` 写入而失效。收到 `order.fulfilled` 后，前端可用 capability 兑换正常登录态，避免把长期 Bearer Token 放入事件流：

```http
POST /v2/public/orders/{orderNo}/session
Content-Type: application/json

{"checkout_token":"guest-only-capability"}
```

该接口仅在访客账户已创建且订单为 Paid 或 Finished 时返回 `access_token`。前端不需要、也不应通过固定轮询请求它。

### 恢复支付与状态查询

为支付页刷新和渠道载荷生成失败保留以下接口，而不是让客户端重新创建订单：

```http
POST /v2/public/orders/{orderNo}/checkout
GET  /v2/public/orders/{orderNo}
POST /v2/public/orders/{orderNo}/event-ticket
POST /v2/public/orders/{orderNo}/session
```

两者沿用订单所有权/访客 capability 校验。`/checkout` 只允许 Pending 订单，并返回与首次请求相同的支付金额和币种；Stripe 复用已保存的 PaymentIntent。

`GET` 仅用于首次 SSE 建连前、断线重连后的状态校准和排障，前端不得周期性轮询它。

## 幂等与状态机

### 创建幂等

新增到 `order`：

```text
state_version         bigint       not null default 0
idempotency_key       varchar(128) null
idempotency_hash      char(64)     null
unique(idempotency_key)
```

V2 的 `Idempotency-Key` 限制为 16–128 个可打印 ASCII 字符；V1 与历史订单保留 `NULL`。MySQL 与 PostgreSQL 的唯一索引均允许多个 `NULL`，因此不会阻塞现有 V1 写入。服务端计算请求的稳定哈希（包含订单类型、业务参数、支付方式、身份范围；不包含 `return_url`），并在创建资源的同一事务中写入订单。

- 首次请求：执行现有的库存、优惠券、赠金预占与订单创建规则；
- 同 key、同 hash：返回同一订单并再次执行幂等 Checkout；
- 同 key、不同 hash：返回 `409 IDEMPOTENCY_KEY_REUSED`；
- 已支付、已完成或已关闭订单：不创建新订单，按现有订单状态返回。

这使“订单已创建但响应丢失”与“前端按钮重复提交”安全可恢复。

### 状态机

```text
Pending --verified payment--> Paid --activation committed--> Finished
   |                                |
   +--close/expire--> Closed         +--retry/reconcile--> Paid
```

所有转移继续使用条件更新，并在同一事务中递增 `state_version`。网关回调、余额支付、手工标记支付、关单与激活器都必须通过统一的状态转移服务写入事件；不得由 HTTP handler 或支付回调直接写 SSE。

## 可靠事件发布

新增 `order_event` 作为 transactional outbox：

```text
id              bigint primary key
order_id        bigint not null
order_no        varchar(255) not null
event_type      varchar(64) not null
payload         json/text not null
created_at      timestamp not null
published_at    timestamp null
index(order_id, id)
index(published_at, id)
```

状态和 `order_event` 必须在同一个数据库事务中提交。发布器随后把事件广播到 Redis channel `order-events:{order_no}`，并记录 `published_at`；发布失败时通过队列重试及周期性扫描补发。

SSE handler 的顺序为：授权 → 订阅 Redis → 从 `order_event` 查询 `id > Last-Event-ID` 并发送 → 转发实时消息。客户端按事件 id 去重，因此查询和实时广播重叠只会产生可安全忽略的重复。

事件表是断线恢复与审计来源；Redis Pub/Sub 只负责低延迟广播。

## 实现结构

```text
V2OrderHandler
  └─ CreateAndCheckoutUseCase
       ├─ OrderCreationService
       │    ├─ PurchaseCreator
       │    ├─ RenewalCreator
       │    ├─ ResetTrafficCreator
       │    └─ RechargeCreator
       ├─ CheckoutUseCase
       └─ OrderEventWriter

Payment callback / balance checkout / close task / activation task
  └─ OrderStateTransitionService → OrderEventWriter

SSE handler
  └─ OrderEventReader + EventBroadcaster
```

现有 V1 创建逻辑的业务校验、资源预占和支付适配器应被提取到上述服务中；V1 handler 仅成为旧 DTO 的适配层。V2 不应调用 V1 HTTP handler，也不应复制价格或优惠券计算。

## 数据库与发布顺序

1. 新增 `state_version`、`idempotency_key`、`idempotency_hash`、`order_event`，同时提供 MySQL 和 PostgreSQL 迁移；先允许 V1 的空幂等字段。
2. 提取订单创建、Checkout 和状态转移服务，保持 V1 行为与测试不变。
3. 上线 V2 认证用户的 `purchase`、`renewal`、`reset_traffic`、`recharge`，暂不切换前端默认入口。
4. 上线 outbox 发布器与 SSE；发布器每 5 秒扫描未发布事件，清理任务仅删除已发布且超过 30 天的记录。验证断线补发、队列故障恢复和代理配置。
5. 上线访客 purchase，携带访客 checkout capability 与 SSE ticket。
6. 前端切换到 V2；V1 保留至少一个发布周期，仅记录使用量与错误率。

## 必须覆盖的测试

- 同一幂等键并发提交只产生一笔订单、一次资源预占；不同请求体被拒绝。
- 外部支付载荷创建失败后，重试返回同一订单和同一支付快照。
- Stripe 重试只返回一个有效 PaymentIntent；余额支付不会二次扣款。
- 回调、关单与激活竞争时事件顺序和最终状态正确。
- 断线使用 `Last-Event-ID` 可以补齐 `payment_paid`、`fulfilled`、`closed`。
- 用户、其他用户、访客、过期 stream ticket 对 SSE 的访问控制正确。
- 匿名订单在 `user_id=0 → user_id>0` 后，旧 stream ticket 可重连、checkout capability 可刷新 ticket，并且只有账户创建后才能兑换会话。
- 事件发布失败时，outbox 扫描最终补发，且重复发布不改变订单状态。

## 可观测性

增加以下指标与告警：

- V2 下单请求、幂等命中、幂等冲突、渠道载荷失败；
- Pending、Paid、Finished、Closed 订单数及 Paid 停留时长；
- outbox 未发布事件数量和最旧事件年龄；
- SSE 连接数、重连数、授权失败与补发事件数。
