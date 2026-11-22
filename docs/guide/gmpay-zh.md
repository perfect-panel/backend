# GMPay 收款

在后台新增支付方式，平台选 `GMPay`。网关需支持 GMWalletApp/epusdt 的原生 GMPay v1 商户接口。在支付方式的 `config` 中填写商户凭证：

```json
{
  "url": "https://pay.example.com",
  "pid": "1000",
  "secret_key": "替换为商户密钥"
}
```

`url` 是网关的 HTTPS 基础地址，可包含部署子路径。通知域名填写网关能访问的 PPanel 公网地址；留空时使用站点地址。PPanel 下单时自动提供 `/v1/notify/GMPay/{token}`。商户密钥只填写在后端。

PPanel 按订单的法币金额下单，用户在网关收银台选择链和币种。结账 API 返回 `type=url` 和 `checkout_url`。GMPay 要求法币金额大于 0.01。

PPanel 保存每次下单的网关交易号和收银台链接。重复打开订单会复用链接。创建请求超时后，PPanel 保留支付尝试，等待已签名的成功回调或人工核对，避免重复下单。

到账以 HMAC-SHA256 签名回调和网关交易详情核对为准。PPanel 核对法币金额、币种和交易号，再幂等入账。关单前也会查单：已付订单转入结算，网关确认过期后才关闭已创建的交易；等待选链、等待付款、链上确认中和人工复核中的订单继续保留。

[上游商户接口](https://github.com/GMWalletApp/epusdt/blob/3506640084f9a6e911e37f7e9b6bf61f14723b33/wiki/API.md)
