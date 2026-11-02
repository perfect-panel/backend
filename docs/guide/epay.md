# EPay integration

[中文](epay-zh.md)

The EPay payment method supports V1 (shared KEY / MD5) and V2 (RSA-SHA256) on the
existing `/v1/notify/EPay/{token}` callback route. Stored configuration selects the
protocol; callback `sign_type` cannot switch versions. Existing configurations
default to V1 page checkout.

## Configuration

Set these fields in the administrator payment method API's `config` object. The
platform remains `EPay`.

| Field | Meaning |
| --- | --- |
| `pid` | Numeric merchant ID, as a string |
| `url` | Gateway address; V2 and V1 API checkout require HTTPS and may include a deployment path |
| `version` | `v1` (default) or `v2` |
| `checkout_mode` | `page` (default) or `api` |
| `type` | Payment type; optional for the page cashier, required for API checkout |
| `key` | V1 shared merchant KEY; must be empty for V2 |
| `private_key` | V2 merchant RSA private key, PEM or bare Base64, at least 2048 bits |
| `public_key` | V2 **platform** RSA public key, PEM or bare Base64, at least 2048 bits |

V2 accepts `alipay`, `wxpay`, and `qqpay`. V1 additionally accepts `bank`, `jdpay`,
`paypal`, and `douyinpay`. Merchant/channel availability is provider-specific.
Configuration validation checks the version, URL, keys, and checkout mode without
calling the gateway. The existing pending-order guard prevents changing credentials
or versions while orders await payment; create another method to switch protocols.

Legacy V1 page checkout continues to accept HTTP, deployment subpaths, and addresses
ending in `submit.php` or `api.php`. V2 and V1 API checkout require HTTPS and accept a
clean deployment path such as `https://pay.example/pay` (no `..`, repeated slashes,
escapes or script names such as `submit.php`). Keys belong in backend administrator configuration, never clients
or source control.

V1 page checkout also accepts site-relative paths such as `/pay`: the browser stays
on its current host, while server queries resolve against the payment method's
required `Domain`. Scheme-relative addresses such as `//pay.example` stay relative
in browser redirects and use HTTPS for server queries. Normalization preserves
these configured relative addresses. V2 page checkout, and V2 and V1 API checkout, accept the same
site-relative and scheme-relative forms: page links stay relative, while create,
query and other server requests resolve against the method's `Domain`, which must
be HTTPS.

## Checkout

Page checkout returns the existing `type=url` / `checkout_url` result. V1 uses
`submit.php`; V2 uses `/api/pay/submit`. V2 page requests require an absolute HTTP(S)
return URL (`returnUrl` on legacy checkout, `return_url` on the V2 order API).
Returning to the browser never settles the order. Use V2 page links promptly:
their signatures carry the current timestamp; request checkout again for a new link.

API checkout uses V1 `/mapi.php` with `device=jump`, or V2 `/api/pay/create` with
`method=jump`. The server supplies the amount, order, notification URL, and client
IP resolved by HTTP middleware. Checkout JSON cannot override the client IP.

Redirects and supported payment-app URL schemes map to `type=url`; QR contents map
to `type=qr`. HTML, native App, JSAPI, and mini-program results need their own client
integration and are rejected by the existing browser flow. They are never executed
as HTML or mislabeled as redirect URLs. Use page checkout for such channels; the
protocol client retains all SDK request methods and raw result types for future clients.

API checkout persists an attempt in `order.payment_session` **before** the remote
request, then atomically binds the provider trade and saves the payment entry while
the order is pending. Repeated checkout reuses the saved entry. Concurrent requests,
timeouts, crashes, and business errors do not automatically reissue creates. Unknown
attempts remain fenced for callbacks, queries, or manual reconciliation; do not clear
the fence to blindly retry. Local validation runs before reserving the attempt.
An API success obtains an entry and does not settle payment. Expired entries are not
automatically replaced: reconcile and close the old trade where supported before
creating a new business order.

## Callbacks and cancellation

Both protocols use billing's existing method binding, amount/currency snapshot,
unique trade constraint, idempotent settlement, and activation queue. Duplicate
parameters are rejected; amounts use integer minor units. Database or queue failures
are not acknowledged as `success`; callback retries can restore queue delivery.

V1 retains standard and EasyPay query compatibility and accepts integer/string
`code` and `status`. When neither query endpoint is supported, a verified callback
matching the local order remains sufficient. V1 has no standard remote close API;
its existing extended wait and explicit cancellation policies remain in place.

V2 verifies every successful response using the platform RSA key and timestamp
(300-second default skew). Query failures, 404s, HTML, and unsigned responses cannot
downgrade the protocol. Cancellation queries first: paid trades settle, unpaid trades
close remotely, and a failed close is queried once more for a payment that won the
race. Unconfirmed closes remain pending, including explicit user closes. Refunds,
partial refunds, frozen/preauthorized trades, and unknown states require reconciliation.

## Protocol coverage and ownership

`internal/module/billing/internal/payment/epay/protocol` adapts the local
`github.com/herta-network/epay` SDK snapshot from 2026-10-25, including its tests.
There is no absolute-path module replacement. The local `Validate` additions let
checkout validate before persisting an attempt. The parent `epay` package retains
legacy V1 compatibility; `gateway`, `callbacks`, and `settle` own business decisions.

| Capability | V1 | V2 |
| --- | --- | --- |
| Page/API payment, order query, notification verification | Typed methods | Typed methods |
| Refund | `Refund`, success code 0 | `Refund`, requires a merchant refund ID |
| Refund query, remote close | Not defined by the reference protocol | `QueryRefund`, `CloseOrder` |
| Merchant, settlement, order lists | `MerchantInfo`, `Settlements`, `Orders` | Documented endpoints through `Call` |
| Transfers, complaints | Not defined by the reference protocol | `Call`, scalar and PHP-array forms |
| Complaint image upload | Not defined by the reference protocol | `UploadComplaintImage` |

No refund, transfer, or complaint business-management endpoints are added. Existing
wallet refunds are unchanged. V2 nested response objects/arrays are excluded from
the reference signature and listed in `Response.UnsignedFields`. V1 responses have
no protocol signature. Both clients bound response sizes, reject redirects, and
perform no automatic retries.

## Upgrade and validation

Apply migration `03024_order_payment_session` for PostgreSQL/MySQL before running
this version. It adds a nullable text column; old orders and configurations need no
backfill. Do not drop the column while API attempts exist: it holds their retry fence.

Run `go test -race ./internal/module/billing/...` and `go vet ./...`. For PostgreSQL
concurrency coverage, set `PPANEL_TEST_EPAY_POSTGRES_DSN` to a disposable database
and run `go test ./internal/module/billing/internal/repo -run TestPaymentSessionsPostgres`.
The test creates and removes a private schema. Protocol/checkout tests use local
HTTPS peers, temporary RSA keys, and test databases. Live merchant channels, client
launch behavior, and real financial outcomes require deployment-specific integration.
