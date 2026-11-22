# PayPal Checkout

Add a `PayPal` payment method to redirect buyers to PayPal Checkout. PPanel captures approved Orders v2 payments on the server and uses the existing `type=url` and `checkout_url` checkout response.

## Merchant setup

| Field | Value |
| --- | --- |
| `client_id` | Client ID of the PayPal REST app |
| `client_secret` | Secret of the same app |
| `environment` | `sandbox` for testing or `live` for production. Defaults to `live`. |
| `webhook_id` | ID PayPal assigns to the registered webhook. Required to enable the method. |
| `locale` | Optional checkout language, such as `zh-CN`, `zh-TW` or `en-US`. Leave empty to let PayPal choose. |

1. Select the appropriate environment and create a REST app in the PayPal Developer Dashboard.
2. Enter the credentials and environment in PPanel and save the payment method as disabled. Use a public HTTPS notification domain or site address.
3. Copy the notification URL from the admin payment method list (`/v1/notify/PayPal/{token}`). Register it on the same PayPal app and environment for `CHECKOUT.ORDER.APPROVED` and `PAYMENT.CAPTURE.COMPLETED`.
4. Copy the webhook ID into PPanel and enable the method. Use live credentials and a live webhook ID when switching to production.

PPanel charges the order in the site currency. JPY, HUF and TWD require whole amounts. Checkout can provide a browser return URL or use the configured site homepage. Currency availability depends on the merchant's country. [PayPal currency support](https://developer.paypal.com/api/rest/reference/currency-codes/)

## Settlement

Repeated checkout reuses the bound PayPal order and approval URL. The browser return only displays the result. PPanel verifies each webhook with PayPal, fetches the order using merchant credentials, and checks the merchant reference, currency, amount and completed full capture before settlement. [Orders v2](https://developer.paypal.com/docs/api/orders/v2/) · [Webhook verification](https://developer.paypal.com/api/rest/webhooks/rest/)

Closing an order also reconciles it with PayPal. Orders awaiting approval can close. Approved payments go through capture and verification. A durable capture reservation serializes the debit with local cancellation. Pending or ambiguous captures stay open for redelivery or reconciliation.
