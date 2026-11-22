# PayPal 收款

在 PPanel 后台创建 `PayPal` 支付方式。用户跳转到 PayPal 收银台批准付款，PPanel 通过 Orders v2 确认扣款并激活订单。结账接口沿用 `type=url` 和 `checkout_url`。

## 商户配置

| 字段 | 填写内容 |
| --- | --- |
| `client_id` | PayPal REST 应用的 Client ID |
| `client_secret` | 同一应用的 Secret |
| `environment` | `sandbox` 用于测试，`live` 用于正式收款，默认 `live` |
| `webhook_id` | PayPal 为该通知地址分配的 Webhook ID，启用前必填 |
| `locale` | 可选，PayPal 收银台语言，例如 `zh-CN`、`zh-TW` 或 `en-US`。留空由 PayPal 选择。 |

1. 在 PayPal Developer Dashboard 选择环境并创建 REST 应用。
2. 在 PPanel 填写应用凭证和环境，先保存为停用。支付方式的通知域名或站点地址应为公开可访问的 HTTPS 地址。
3. 从后台支付方式列表复制该方式的通知地址（`/v1/notify/PayPal/{token}`）。在同一环境、同一 REST 应用中添加该 Webhook，订阅 `CHECKOUT.ORDER.APPROVED` 和 `PAYMENT.CAPTURE.COMPLETED`。
4. 将 PayPal 返回的 Webhook ID 填回 PPanel，启用支付方式。切换正式环境时使用正式应用的凭证和 Webhook ID。

PPanel 按订单的站点币种计价。JPY、HUF 和 TWD 需要整数金额。客户端可以提供支付完成后的回跳地址；留空时回到站点首页。部分币种受商户账户所在地区限制。[PayPal 支持的币种](https://developer.paypal.com/api/rest/reference/currency-codes/)

## 到账与对账

PPanel 保存 PayPal 订单号与批准链接，重复结账复用同一订单。浏览器回跳仅用于展示。收到 Webhook 后，PPanel 将原始事件与传输签名交给对应环境的 PayPal 验证，再使用商户凭证查询交易。只有订单号、金额、币种和全额扣款记录均匹配且扣款状态为 `COMPLETED` 才入账。[Orders v2](https://developer.paypal.com/docs/api/orders/v2/) · [Webhook 验证](https://developer.paypal.com/api/rest/webhooks/rest/)

关单时也会查单。仍等待买家批准的订单可以取消；已经批准的订单先核对扣款结果。扣款请求和本地关单争用同一订单记录，以免已取消的订单被扣款。结果不明或扣款仍在处理中时，订单保留待支付，等待后续通知或对账。
