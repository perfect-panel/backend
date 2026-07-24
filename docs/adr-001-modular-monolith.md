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
2. **拆 Store**（进行中）：为每个模块定义窄 store 接口（如 billing 只见 Order/OrderEvent/Payment/Coupon），
   `InTx` 收窄为模块作用域；按附录 A.1 逐个把跨域事务改为"本模块事务 + outbox 事件 + 对账"
   （审计 Log 按 A.1 结论豁免为横切关注点）。已落地的机制与改造：
   - ✅ `Store.DB()` 逃生口已移除（零调用者）。
   - ✅ **幂等收件箱**（idempotent-consumer/inbox 模式）：`domain_event_inbox` 表 +
     `repository.InboxRepo`。每个域步骤在自己的事务内插入 `(consumer, event_key)` 标记，
     使 at-least-once 投递与对账重放天然安全；唯一约束同时解决并发竞争（输者回滚）。
     拆分为微服务时各消费方带走自己的 consumer 行，成为其私有 inbox 表。
   - ✅ **领域判定澄清（钱包归 billing）**：`User.Balance / GiftAmount / Commission` 的资金变动
     是 billing 域操作（正文模块表已将"余额与提现"划归 billing），这些列暂住 user 表属数据
     债务，第 5 步拆出独立 wallet 表。因此"锁 user 行 + 扣减余额/赠金/佣金 + 审计日志 + 订单写"
     的事务是**单域（billing）事务**，无需事件化。据此重分类为合规的事务点：
     `renewalLogic`、`resetTrafficLogic`、`purchaseCheckoutLogic`（余额结账）、
     `commissionWithdrawLogic`、`updateUserBasicInfoLogic`（管理员调账）、`trafficStatLogic`
     （network 统计 + 审计）。
   - ✅ **checkSubscriptionLogic（2 处）**：事务收窄为纯 subscription 域写（查询 + 批量置状态），
     邮件通知与用户/服务器缓存失效移到提交后执行（可重试副作用）。
   - ✅ **套餐库存生命周期事件化**（`internal/orderflow/inventory.go`）：库存预留/回补是
     subscription 域写，从 purchase/portal purchase/closeOrder 的 billing 事务中拆出，
     以订单号为键做幂等（`subscription.inventory_reserve/restore`）。下单流程：billing 事务
     建单 → subscription 事务预留（缺货则同步关单补偿，回补因无预留标记而自动跳过）；
     关单流程：billing 事务 CAS 关单 → subscription 事务回补（断点由重试的关单任务经
     status==3 分支续跑）。已知窗口：①建单提交后、预留前进程崩溃且用户在 30 分钟内完成网关
     支付 → 单件超卖（极小概率双重巧合）；②部署切换时刻处于 Pending 的新购订单（旧流程无
     预留标记）关单时不回补 → 建议低峰部署或部署后跑一次库存核对。
   - ✅ **领域窄 store 视图与作用域事务**（`internal/repository/domains.go`）：
     `BillingStore / SubscriptionStore / IdentityStore / NetworkStore` 四个视图 +
     `InBillingTx` 等作用域事务，闭包只拿到本域仓储，跨域写**编译失败**。
     `WalletRepo` 把"钱包归 billing"落到代码（billing 事务经 `Wallet()` 而非 `User()`
     动钱包列）。已迁移的调用点：库存预留/回补、订阅检查、退订两段、流量聚合两段、
     激活的建号/充值/佣金/结算段、关单主事务、portal 补偿。仍留在通用 `InTx` 的例外：
     订阅履约段（过渡期 user 行锁）与新购主事务（订阅配额跨域读），均有注释标记。
   - ✅ **退订两段化**（`unsubscribeLogic`）：subscription 事务翻转状态并在取消标记中持久化
     "orderID|应退金额"，billing 事务按标记退款（赠金优先）并写退款标记；两段之间崩溃时，
     用户重试会命中"已扣减但未退款"分支直接续跑退款段。
   - ✅ **流量聚合两段化**（`trafficagg`）：订阅用量计数（subscription）与流量日志（network）
     各自成事务，以 bucket suffix 为幂等键——flush 管线本就重放同一 bucket 直至成功/死信，
     inbox 标记防止已提交的一半被重复计数。
   - ✅ **收件箱保留期**：`InboxRepo.DeleteProcessedBefore` 挂入每日清理任务，与订单事件
     共用 30 天保留契约（所有重放窗口远小于它）。
   - ✅ **首个改造完成：`queue/logic/order/activateOrderLogic`**。原单一跨 4 域事务拆为
     四个单域事务：① identity 访客建号（inbox 存 userId 供重放重绑）→ ② subscription/identity
     履约（开通/续费/重置/充值）→ ③ identity 佣金 → ④ billing 结算（优惠券计数 + Paid→Finished
     CAS + `order.fulfilled` outbox 事件原子提交）。订单在 ④ 之前保持 Paid，
     崩溃由既有 `SchedulerReconcilePaidOrders` 重新驱动，已完成阶段被 inbox 跳过。
     过渡期保留：新购事务内的 user 行锁（按用户串行化配额检查，第 5 步移交 subscription 模块）；
     已知窗口：管理员在履约后、结算前关闭 Paid 订单会留下已履约的 Closed 订单（改造前由行锁互斥），
     补偿属 billing 关单流程的后续工作。
3. **拆 ServiceContext**（进行中）：延续现有 DI 重构，每模块一个 deps 结构，`ServiceContext`
   只在组装根出现。已落地：`internal/arch` 的 `TestSvcImportFreeze` 把 import `internal/svc`
   的包目录冻结为基线（71 项，只许收窄）——新代码必须走模块门面注入依赖；每迁移一个域，
   基线相应删项，收缩过程可度量。billing 模块（admin order/payment）已按此模式落地：
   激活入队、网关模式探测、站点 Host 全部经 Deps 注入，`ActivationEnqueuer` 端口在组装根
   适配 asynq。**结账金流已整体迁入 billing**（`internal/module/billing/internal/checkout`）：
   purchase/renewal/resetTraffic/recharge/preCreate/close 六个流程 + 计价助手，端口化了
   订阅域读取（`PlanReader`/`UserSubscriptionReader`，legacy repo 结构化满足）、订单队列
   （激活 + 延迟关单）、单订阅模式与币种配置；`notify.SettleVerifiedPayment` 的结算逻辑
   （CAS 标记已付 + 激活入队）收编为模块内部函数，close 的网关结算（Stripe/EPay）随迁。
   `v2OrderLogic`（SSE 票据/幂等编排/portal 结账胶水）暂留 legacy 层改调门面，随 portal
   checkout 迁移后收编。
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
**跨 2+ 模块的事务 17 处——已全部处理**（2026-07-24）：4 处随 activateOrderLogic 事件化、
6 处经"钱包归 billing"澄清重分类为单域、2 处（checkSubscription）副作用外移、
2 处（purchase/portal purchase）库存生命周期拆出、1 处（closeOrder）回补拆出、
1 处（unsubscribe）两段化、1 处（trafficagg）两段化。原始清单留档：

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
| ~~`queue/logic/order/activateOrderLogic.go`~~ | ~~billing+subscription+identity+platform~~ | ✅ 已拆为四个单域事务 + 幂等收件箱（见第 2 步），旧版非事务路径已删除 |
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
