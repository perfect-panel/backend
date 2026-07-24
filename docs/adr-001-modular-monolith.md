# ADR-001: 演进为模块化单体架构

- 状态：已接受
- 日期：2026-07-24
- 相关：`docs/v2-order-checkout-design.md`（订单 outbox 与对账基础）、`internal/arch`（边界强制测试）

## 背景

当前代码库分层清晰（handler → logic → repository），数据层已按域拆分为 23 个 repo 接口，
订单域具备 outbox 事件与定时对账，异步链路统一走 asynq。但存在三个阻碍未来微服务拆分的结构性问题：

1. **`svc.ServiceContext` 是全局上帝对象**：logic 层 477 处引用、queue/scheduler 41 处引用，
   每个 logic 都能拿到全部依赖，模块边界无从谈起。
2. **`repository.Store` 门面 + `InTx(fn(Store))`**：任何 logic 均可在单个事务内跨任意域读写，
   `DB()` 逃生口直接暴露 `*gorm.DB`。跨域事务是拆分时最难解开的耦合。
3. **代码按访问面（admin/public/auth/…）组织，不按业务域**：同一个域的业务规则散落在
   `logic/admin/<域>`、`logic/public/<域>`、`queue/logic`、`scheduler` 多处。

## 决策

将系统重构为**模块化单体**：按限界上下文划分模块，模块间只允许通过**门面接口（同步）**与
**集成事件（异步）**交互，数据表有唯一属主模块。目标是让"拆出一个微服务"退化为纯机械操作：
门面实现换成 gRPC client、进程内事件换成消息队列、搬表——业务代码不再改动。

### 模块划分

| 模块 | 职责 | 现有资产（repo / 包） |
|---|---|---|
| `identity` | 用户、认证、OAuth、设备、验证码 | UserRepo、AuthRepo、UserAuthRepo、UserDeviceRepo、`pkg/oauth` |
| `billing` | 订单、支付、优惠券、余额与提现 | OrderRepo、OrderEventRepo、PaymentRepo、CouponRepo、UserWithdrawalRepo、`internal/orderflow`、`internal/orderstream`、`pkg/payment` |
| `subscription` | 套餐、用户订阅、配额 | SubscribeRepo、UserSubscriptionRepo、SubscriptionTrafficRepo |
| `network` | 节点、流量、edge、订阅分发 | NodeRepo、TrafficRepo、`internal/edgeauth`、`internal/trafficagg`、`adapter/` |
| `support` | 工单、公告、文档、广告、营销 | TicketRepo、AnnouncementRepo、DocumentRepo、AdsRepo |
| `notification` | email / sms / telegram / 站内通知 | `pkg/email`、`pkg/sms`、`queue/logic/email|sms`、telegram bot |
| `platform`（共享内核） | 配置、系统设置、日志、汇率、GeoIP、缓存、ID 生成 | SystemRepo、LogRepo、ClientRepo、TaskRepo、`pkg/*` 基础库 |

划分原则：粒度对齐"未来的微服务候选"。`platform` 是共享内核，任何模块可依赖它，它不依赖任何模块。

### 模块结构与交互规则

```
internal/module/<name>/
├── <name>.go        # 门面：接口 + 构造函数 New(deps) + 对外 DTO（不泄漏 GORM entity）
├── events/          # 集成事件定义（其他模块可订阅）
└── internal/        # 实现：service / repo / entity —— Go 编译器保证外部不可 import
```

1. **门面**：模块根包只含接口、DTO 与 `New(...)` 构造函数。admin/public handler 都是薄壳，
   调用同一个模块 service；访问面差异（权限、字段裁剪）留在 handler。
2. **集成事件**：模块间的写-写协作一律走事件（`OrderPaid`、`SubscriptionExpired`、
   `TrafficExceeded`…），复用订单域已验证的 outbox + 定时发布 + 对账兜底模式，
   泛化为 `pkg/eventbus`。**禁止新增跨模块事务**。
3. **组装根**：`internal/svc.NewServiceContext` 退化为 composition root，负责构造各模块并
   注入依赖；模块代码不得反向 import `internal/svc`。
4. **数据所有权**：每张表唯一属主。跨模块取数走门面调用后内存组装，或事件驱动的冗余字段；
   禁止跨模块 JOIN。

### 边界强制（已落地）

- **编译器**：模块实现位于嵌套 `internal/` 下，跨模块 import 内部包直接编译失败。
- **架构测试** `internal/arch/arch_test.go`（随 `go test ./...` 与 lefthook pre-commit 运行）：
  - `TestLogicImportFreeze`：冻结存量 logic 跨包依赖为 8 条基线（见测试内
    `legacyLogicImports`），只许收窄，新增即失败；
  - `TestModulePurity`：`internal/module/**` 不得 import `internal/svc` 与 `internal/logic`；
  - `TestModuleLayout`：模块只允许暴露门面包与 `events/`，其余必须在 `internal/` 内。

## 迁移路径

每步独立可交付、可上线，不长期分叉：

1. ✅ **立规矩**：本 ADR + `internal/arch` 边界测试（存量豁免、新增即拦截）。
2. **拆 Store**：为每个模块定义窄 store 接口（如 billing 只见 Order/OrderEvent/Payment/Coupon），
   `InTx` 收窄为模块作用域；按附录 A.1 逐个把跨域事务改为"本模块事务 + outbox 事件 + 对账"
   （审计 Log 按 A.1 结论豁免为横切关注点）；顺手移除已无调用者的 `Store.DB()` 逃生口。
3. **拆 ServiceContext**：延续现有 DI 重构，每模块一个 deps 结构，`ServiceContext` 只在组装根出现。
4. **域优先重组**：把 `logic/admin/<域>` + `logic/public/<域>` + `queue/logic/<域>` 收拢进
   `internal/module/<域>/internal/service`，handler 变薄。试点顺序：`support`（耦合最低）
   → `billing`（刚硬化过、测试最全）→ 其余。
5. **数据所有权清算**：表→模块归属表定稿，清理附录 A.4 的跨模块 JOIN/Preload，
   并把 identity 与 subscription 共用的 `*userRepo` 实现物理分家（见附录 A 开头说明）。
6. **拆分就绪**：门面换 gRPC 实现（`api/` 已有 protobuf 基建）、事件换消息队列、搬表。

过渡期约定：模块实现**暂时允许** import `internal/repository`（包装存量 repo 起步），
目标在第 5 步归零；不允许 import `internal/svc` 与 `internal/logic`（测试强制）。

## 门面接口草案（示意）

```go
// internal/module/billing/billing.go
package billing

type Service interface {
    Checkout(ctx context.Context, req CheckoutRequest) (CheckoutResult, error)
    CloseOrder(ctx context.Context, orderNo string, reason CloseReason) error
    QueryOrder(ctx context.Context, orderNo string) (Order, error)
    // 供 identity/subscription 查询，替代跨域 JOIN：
    UserPaidOrderCount(ctx context.Context, userID int64) (int64, error)
}

func New(deps Deps) Service { ... } // 由 internal/svc 组装根调用

// internal/module/billing/events/events.go
package events

type OrderPaid struct {
    OrderNo     string
    UserID      int64
    SubscribeID int64
    Amount      int64
    PaidAt      time.Time
}
```

订阅方（如 subscription 模块开通订阅、notification 发送通知）通过 `pkg/eventbus` 注册
handler，投递语义为 at-least-once，处理方必须幂等（复用 `internal/orderflow` 的幂等键模式）。

## 风险与对策

- **事务语义变更**：跨域"一个大事务"改为"事务 + 事件"后是最终一致。对策：每类事件配
  对账任务兜底（已有 `SchedulerReconcilePaidOrders` 模式可复制）；先迁读路径、后迁写路径。
- **边界腐化**：靠机器强制（编译器 + arch 测试），基线只减不增；新增基线条目需修订本 ADR。
- **过渡期双轨**：模块化域与遗留域并存期间，遗留代码调用新模块只走门面，避免出现
  "新模块 import 旧 logic"的回头路（测试强制）。

## 附录 A：跨域耦合盘点（2026-07-24 快照）

模块映射同上表。注意：`Store` 的 `UserAuth/UserSubscription/UserDevice/UserWithdrawal/SubscriptionTraffic/UserCache`
六个访问器在实现层返回同一个 `*userRepo`（`internal/repository/store.go:135-143`），拆分时
identity 与 subscription 的 repo 实现需要先物理分家。

### A.1 跨域事务点（第 2 步的改造清单）

单域事务 23 处（identity 8、subscription 7、network 3、billing 2、platform 3），无需改造。
**跨 2+ 模块的事务 17 处**，按业务流分组：

| 调用点 | 跨越模块 | 业务流 |
|---|---|---|
| `internal/logic/public/order/purchaseLogic.go:216` | billing+subscription+identity+platform | 下单购买：扣余额、建订单、开通订阅 |
| `internal/logic/public/order/renewalLogic.go:166` | billing+identity+platform | 续费 |
| `internal/logic/public/order/closeOrderLogic.go:94` | billing+subscription+identity+platform | 关单并退回余额 |
| `internal/logic/public/order/resetTrafficLogic.go:97` | billing+identity+platform | 购买式流量重置 |
| `internal/logic/public/portal/purchaseLogic.go:155` | billing+subscription | portal 预下单 |
| `internal/logic/public/portal/purchaseCheckoutLogic.go:649` | billing+identity+platform | 余额支付结账（经 `CheckoutTransaction` 端口） |
| `internal/logic/public/user/commissionWithdrawLogic.go:45` | billing+identity+platform | 佣金提现 |
| `internal/logic/public/user/unsubscribeLogic.go:73` | billing+subscription+identity+platform | 退订并退款 |
| `internal/logic/admin/user/updateUserBasicInfoLogic.go:37` | identity+platform | 管理员改资料（仅审计 Log 跨域） |
| `queue/logic/order/activateOrderLogic.go:90,518,687,971` | billing+subscription+identity+platform | 异步订单激活/开通（耦合最深） |
| `queue/logic/subscription/checkSubscriptionLogic.go:31,71` | subscription+identity | 订阅检查 + 清用户缓存（仅缓存失效跨域） |
| `queue/logic/traffic/trafficStatLogic.go:33` | network+platform | 流量统计 + 审计 |
| `internal/trafficagg/aggregator.go:445` | network+subscription | 流量聚合写回订阅用量 |

由此得出两条改造策略：

- **审计 Log 是横切关注点**，出现在 17 处中的 11 处。不值得为它引入事件：建议将审计日志
  归入 `platform` 共享内核并明确"允许任何模块在自己事务内写审计表"（追加写、无读依赖，
  拆库时改为异步即可），跨域事务清单立减一半。
- **真正的硬耦合是 billing↔subscription↔identity 的资金/开通链路**（购买、激活、退订、结账），
  集中在 6 个业务流。改造顺序建议：先 `activateOrderLogic`（已在异步侧，天然适合事件化），
  再 checkout/purchase（有幂等键与对账兜底），最后退订/提现。

### A.2 `Store.DB()` 逃生口

**零外部调用者**。全仓 `.DB()` 命中均为 GORM 自身 `*gorm.DB.DB()`（连接池设置/ping：
`initialize/config.go:161,250`、`pkg/orm/mysql.go:133,160`）。可在第 2 步直接从 `Store`
接口移除，防止后续被用起来。

### A.3 logic → repo 访问矩阵（横跨 4+ 模块的重灾区）

- `internal/logic/public/order` → billing(Order/Payment/Coupon) + subscription(Subscribe/UserSubscription) + identity(User) + platform(Log)
- `internal/logic/admin/user` → identity(User/UserAuth/UserDevice/UserCache) + subscription(UserSubscription/Subscribe) + network(TrafficLog) + platform(Log)
- `queue/logic` → billing + identity + subscription + network + platform（几乎全部）
- `internal/logic/admin/order` → 仅 billing（干净）

### A.4 跨模块 JOIN / Preload（第 5 步的改造清单）

- `internal/repository/order.go:218,425,435`：OrderRepo(billing) `Preload("Subscribe")` → subscription 表
- `internal/repository/user.go:494-500`：UserRepo(identity) `JOIN user_subscribe` → subscription 表（邮件收件人筛选）
- `internal/repository/user.go:645`：UserRepo 同时 Preload Subscribe(subscription) 与 User(identity)
- `internal/repository/user.go:692-693,733-734`：UserRepo(identity) LEFT JOIN 基于 order 表(billing)的子查询（新单/续费统计）
- `internal/repository/subscribe.go:108`：同域但绕过 repo 直接 `Table("user_subscribe")` 裸表查询

### A.5 logic 层 import 现状

- logic 内部跨包 import：17 处、8 条边，已冻结为 `internal/arch` 基线（`common` ×7、
  `auth/registerpolicy` ×4、`nodeconfig` ×3、`telegram` ×1、`notify` ×1、`public/portal` ×1）。
  其中 `common` 与 `registerpolicy` 属共享内核候选，迁移时移入 `platform`/`identity` 门面。
- logic 之外的调用方：handler 各子包 →对应 logic（常规布线，模块化后改调门面）；
  **`queue/logic/order` → `internal/logic/public/order`（2 处）与 `internal/logic/telegram`（1 处）**、
  `initialize/telegram.go` → `internal/logic/telegram`（1 处）——这 4 处是队列/初始化直接复用
  域逻辑，billing/notification 模块成型时随域收拢，无需单独冻结。
