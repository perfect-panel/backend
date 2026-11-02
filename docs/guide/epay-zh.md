# EPay 接入

[English](epay.md)

EPay 支付方式支持 V1（共享 KEY / MD5）和 V2（RSA-SHA256），使用同一个现有回调入口
`/v1/notify/EPay/{token}`。协议由支付方式配置决定，回调的 `sign_type` 不能切换版本。
已有配置未填写 `version`、`checkout_mode` 时，继续使用 V1 页面支付。

## 支付方式配置

以下字段放在管理员支付方式 API 的 `config` 对象中；平台仍为 `EPay`。

| 字段 | 说明 |
| --- | --- |
| `pid` | 数字商户 ID，字符串 |
| `url` | 网关地址；V2 和 V1 API 下单要求 HTTPS（可带部署路径） |
| `version` | `v1`（默认）或 `v2` |
| `checkout_mode` | `page`（默认）或 `api` |
| `type` | 支付类型；页面支付可留空进入收银台，API 下单必填 |
| `key` | V1 商户共享 KEY；V2 必须留空 |
| `private_key` | V2 商户 RSA 私钥，PEM 或裸 Base64，至少 2048 位 |
| `public_key` | V2 **平台** RSA 公钥，PEM 或裸 Base64，至少 2048 位 |

V2 支持 `alipay`、`wxpay`、`qqpay`。V1 另支持 `bank`、`jdpay`、`paypal`、
`douyinpay`；实际可用通道取决于商户权限。V1 不接受 RSA 配置，V2 不接受共享 KEY。
保存配置会校验版本、地址、密钥格式和下单模式，不会联系支付平台。
存在待付款订单时，现有支付配置修改保护仍生效；应创建新的支付方式来切换协议。

旧 V1 页面支付继续接受 HTTP、部署子路径，以及结尾为 `submit.php` / `api.php` 的地址。
也支持 `/pay` 这类站内路径：浏览器保持当前域名，服务端查单使用支付方式的 `Domain`
作为前缀，因此必须填写 `Domain`。`//pay.example` 形式保持浏览器协议相对跳转，服务端
查单固定使用 HTTPS。相对地址不会被改写保存为绝对地址。
V2（页面支付和 API 下单）和 V1 API 下单同样支持 `/pay` 这类站内路径和 `//pay.example` 协议相对地址，规则与上面
相同：页面支付的跳转链接保持相对，下单、查单等服务端请求使用 `Domain`（必须是 HTTPS）作为前缀。
二者也接受带部署路径的 HTTPS 地址，如 `https://pay.example/pay`；路径必须规范，不能含 `..`、
重复斜杠、转义字符或 `submit.php` 这类脚本名。
密钥仅保存在后端管理员配置中，不能放进客户端请求或源码。

## 现有收款流程

### 页面支付

- V1 生成 `submit.php` 签名链接；V2 生成 `/api/pay/submit` RSA 签名链接。
- 现有下单 API 返回 `type=url` 和 `checkout_url`，客户端直接打开。
- V2 页面支付要求结账请求提供绝对 HTTP(S) 回跳地址：旧结账接口为 `returnUrl`，
  V2 订单接口为 `return_url`。回跳只用于展示结果，不能触发入账。
- V2 页面签名包含当前时间，客户端应即时使用链接；需要重新打开时重新调用结账接口。

### API 下单

将 `checkout_mode` 设为 `api`。V1 请求 `/mapi.php`（`device=jump`），V2 请求
`/api/pay/create`（`method=jump`）。支付金额、订单号、通知地址由服务端生成；
`clientip` 来自 HTTP 中间件解析后的请求上下文，不能由结账 JSON 覆盖。

支付入口映射到原有客户端契约：

| 网关结果 | 后端结果 |
| --- | --- |
| 跳转 URL | `type=url`、`checkout_url` |
| 二维码内容 | `type=qr`、`checkout_url`（二维码内容，不一定是图片 URL） |
| 支持的支付宝 / 微信 / QQ URL Scheme | `type=url`、`checkout_url` |

HTML、原生 App、JSAPI、小程序插件等返回值需要专门的客户端集成。本次现有浏览器
收款流程不会把它们误当 URL 或执行 HTML；遇到此类结果返回错误，可改用页面模式。
底层协议客户端包含这些下单参数和原始结果，供后续对应客户端使用。

API 下单在 `order.payment_session` 持久化一次支付尝试：

1. 先写入不可重复领取的创建标记，再发送网关请求。
2. 收到有效响应后，在订单仍待付款时原子绑定交易号并保存支付入口。
3. 重复结账直接复用已保存入口。并发请求、请求超时或进程重启不会自动再次下单。
4. 未知结果保留创建标记，等待回调、主动查单或人工核对。不要手动清空标记后盲目重发。
   即使网关返回业务错误，也不自动重发；协议未提供跨部署统一的创建幂等保证。

本地请求校验在写入创建标记之前完成。API 下单成功仅表示取得支付入口，不代表付款完成。
入口过期时不会自动换单；应先确认旧交易并按通道能力关闭它，再发起新的业务订单。

## 回调、对账和关单

两种版本都走现有 billing 回调流程：拒绝重复参数，验签，核对支付方式绑定、商户、
订单号、交易号、金额与币种快照，查单确认后幂等标记已付并投递激活任务。
使用整数分处理金额，数据库失败或入队失败不会返回 `success`；重复通知可恢复入队。

- V1 保留标准查单和 EasyPay 状态查询兼容。两个查询入口均明确不支持时，可以接受
  已验签并匹配本地订单的回调。查询的 `code`、`status` 兼容整数及整数字符串。
- V2 成功响应必须通过平台 RSA 签名和时间戳检查，默认允许 300 秒时钟偏差。
  查单失败、404、HTML、无签名响应不能降级为 V1 或绕过查单。
- V2 自动/主动关单先查询。已付款则入账；未付款则调用远端关单。
  关单失败时再查一次，处理付款抢先完成的竞态；仍不明确时保留待付款状态。
- 已退款、部分退款、冻结、预授权或未知 V2 状态保留待核对，不自动入账或关闭。
- V1 没有标准远端关单接口，保留现有延长等待窗口和显式取消策略。
  关闭后到达的付款继续通过现有异常支付记录供人工处理。

## 协议层覆盖及业务边界

`internal/module/billing/internal/payment/epay/protocol` 以
`github.com/herta-network/epay` 的 2026-10-25 本地 SDK 为参考，源码及测试纳入本仓库，
不使用本机绝对路径 `replace`。主要本地适配是创建请求的 `Validate` 方法，以便先校验再
持久化支付尝试。`epay` 上层保留已有 V1 网关兼容行为；业务决策属于 `gateway`、
`callbacks`、`settle`，SDK 不操作业务数据库。

| 能力 | V1 | V2 |
| --- | --- | --- |
| 页面 / API 下单、查单、通知验签 | 完整协议方法 | 完整协议方法 |
| 退款 | `Refund`，成功码 0 | `Refund`，带业务退款号 |
| 退款查询、远端关单 | 参考协议未定义 | `QueryRefund`、`CloseOrder` |
| 商户、结算、订单列表 | `MerchantInfo`、`Settlements`、`Orders` | 通过 `Call` 调用已定义接口 |
| 代付、投诉处理 | 参考协议未定义 | `Call`；支持标量和 PHP 数组表单 |
| 投诉图片 | 参考协议未定义 | `UploadComplaintImage` |

退款、代付、投诉没有新增业务管理接口，也没有接入自动资金操作；现有钱包退款不变。
V2 响应中的嵌套对象/数组按参考协议不在签名范围内，`Response.UnsignedFields` 显式列出。
V1 无响应签名。两种客户端均限制响应大小、拒绝重定向、不自动重试。

## 升级与验证

启动前应用迁移 `03024_order_payment_session`（PostgreSQL/MySQL），新增可空文本字段。
已有订单和配置不需要回填。不要在存在 API 支付尝试时回滚删除此字段，否则会丢失防重发依据。

```sh
go test -race ./internal/module/billing/...
go vet ./...
```

可在独立测试库验证 PostgreSQL 并发写入：设置 `PPANEL_TEST_EPAY_POSTGRES_DSN` 后运行
`go test ./internal/module/billing/internal/repo -run TestPaymentSessionsPostgres`。
测试创建并清理独立 schema。协议和收款测试使用本地 HTTPS 模拟网关、临时 RSA 密钥和
测试数据库；实际商户通道、客户端拉起和真实资金结果仍需部署后的联调。
