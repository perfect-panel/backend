# GMPay checkout

Add a payment method with platform `GMPay` for a gateway implementing GMWalletApp/epusdt's native GMPay v1 merchant API. Set the merchant credentials in the payment method's `config` object.

```json
{
  "url": "https://pay.example.com",
  "pid": "1000",
  "secret_key": "replace-with-merchant-secret"
}
```

Use the HTTPS base URL of the gateway, including its deployment path if applicable. Set the payment method's notification domain or the public site address so the gateway can reach PPanel. Checkout supplies the callback URL `/v1/notify/GMPay/{token}`. Keep the merchant secret on the backend.

PPanel charges the order's fiat amount. Buyers choose their network and token at the hosted cashier. Checkout returns `type=url` and `checkout_url`. GMPay requires a fiat amount greater than 0.01.

Repeated checkout reuses the stored gateway trade ID and cashier link. If the create outcome is unknown, PPanel retains the attempt for a signed callback or manual reconciliation.

PPanel authenticates the HMAC-SHA256 callback and checks the gateway's transaction details for the trade ID, fiat amount, currency and paid status before settlement. Closing a started checkout also checks the gateway. Confirmed payments settle, expired transactions close, and orders awaiting selection, payment, chain confirmation or manual review stay pending.

[Upstream merchant API](https://github.com/GMWalletApp/epusdt/blob/3506640084f9a6e911e37f7e9b6bf61f14723b33/wiki/API.en.md)
