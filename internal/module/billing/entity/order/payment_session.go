package order

// PaymentSessionStarting is a durable fence, not a payment result. A process
// crash or timeout after this write leaves an unknown attempt to reconcile.
const PaymentSessionStarting = `{"state":"creating"}`

// PaymentSessionCapturing records that an outbound capture may already be in flight.
const PaymentSessionCapturing = `{"state":"capturing"}`
