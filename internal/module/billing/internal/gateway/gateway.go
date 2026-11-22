// Package gateway is the billing module's single seam onto payment gateways.
// A Gateway speaks one gateway's protocol for one configured payment method:
// it starts a payment, reconciles a pending order before it closes, and
// authenticates and re-confirms the gateway's callbacks. Checkout, close and
// callback flows are written once against this interface; a Registry opens
// the gateway of a payment method from its typed configuration.
package gateway

import (
	"context"
	"errors"
	"net/url"

	"github.com/perfect-panel/backend/internal/module/billing/internal/payment/paypal"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
	"github.com/perfect-panel/backend/pkg/logger"
)

// ErrUnconfirmed reports that the gateway could not confirm a pending order
// safe to close, so the order intentionally stays pending. Schedulers treat
// it as an expected outcome, not a per-order failure.
var ErrUnconfirmed = errors.New("gateway could not confirm the order as paid")

// ErrInvalidCallback marks a callback that cannot be authenticated or does
// not describe the order it names: a forged or malformed payload, an unknown
// order, an amount that is not the order's. Redelivering it cannot succeed,
// unlike a processing failure, which the next delivery may get past.
var ErrInvalidCallback = errors.New("invalid payment callback")

// InvalidCallback marks err as ErrInvalidCallback without changing its text,
// which some gateways receive as the failure body.
func InvalidCallback(err error) error {
	if err == nil {
		return nil
	}
	return invalidCallbackError{err: err}
}

type invalidCallbackError struct{ err error }

func (e invalidCallbackError) Error() string   { return e.err.Error() }
func (e invalidCallbackError) Unwrap() []error { return []error{ErrInvalidCallback, e.err} }

// Charge is an amount a gateway collects, in minor units of Currency.
type Charge struct {
	Amount   int64
	Currency string
}

// Payer identifies the buyer to gateways that keep customer records.
type Payer struct {
	UserID int64
	Email  string
}

// Checkout is one payment attempt for a pending order.
type Checkout struct {
	// Order is the pending order. Its recorded payment expectation equals
	// Charge; the gateway is asked for exactly that amount.
	Order     *order.Order
	Charge    Charge
	NotifyURL string
	ReturnURL string
	SiteHost  string
	// Subject names the purchase on the gateway's payment page.
	Subject string
	Payer   Payer
	// Claim binds a provider-side payment (a Stripe intent, a Cryptomus
	// invoice) as the order's only one and returns the payment the order
	// holds afterwards: tradeNo when the claim won, the concurrent winner's
	// otherwise.
	Claim func(ctx context.Context, tradeNo string) (string, error)
	// BeginSession fences an API create before any remote request. Once begun,
	// a failed/unknown create is reconciled, never automatically re-issued.
	BeginSession func(context.Context) (bool, error)
	// CompleteSession atomically binds the returned trade and reusable checkout
	// entry while the order is still pending.
	CompleteSession func(ctx context.Context, tradeNo, session string) (bool, error)
}

// CloseRequest asks whether a pending order may close locally.
type CloseRequest struct {
	Order *order.Order
	// Explicit is set when a person gives the order up (its owner or an
	// administrator), which forfeits a payment the gateway cannot confirm.
	Explicit bool
	// SystemCurrency is the site currency, the charge currency of orders
	// checked out before payment expectations were recorded.
	SystemCurrency string
	// ReserveCapture serializes outbound capture with closing the same pending order.
	ReserveCapture func(context.Context, string, string) (bool, error)
}

// Reconciliation is a gateway's verdict on a pending order that is about to
// close.
type Reconciliation struct {
	// TradeNo is set when the gateway proves the order was paid: the order
	// must be settled with it instead of closed.
	TradeNo string
	// RequireStableCheckout makes the close fail with ErrUnconfirmed if the
	// order's checkout changed after the gateway was consulted, because the
	// verdict only covers the checkout the gateway saw.
	RequireStableCheckout bool
}

// Notification is a gateway callback as delivered to the notify URL.
type Notification struct {
	HTTPMethod         string
	PayPalTransmission paypal.Transmission
	// Params holds the form and query parameters when each occurs exactly
	// once; Form holds all of them.
	Params map[string]string
	Form   url.Values
	// Body is the raw request body and Signature the signature header of
	// gateways that sign the body.
	Body      []byte
	Signature string
}

// Notice is an authenticated callback.
type Notice struct {
	OrderNo  string
	TradeNo  string
	Amount   int64
	Currency string
	// Paid reports that the callback announces a completed payment. A notice
	// that is not paid is a lifecycle event: it is checked against the order
	// and acknowledged without settling.
	Paid bool
	// Approved asks the server to capture a buyer-approved payment before settlement.
	Approved bool
	// Ignore marks a callback that concerns no order payment at all; it is
	// acknowledged without looking the order up.
	Ignore bool
	// Status is the gateway's payment status, for logging.
	Status string
	// ManualReview marks a lifecycle event that needs an operator.
	ManualReview bool
	// Fields describe the callback in the log.
	Fields []logger.LogField

	detail any
}

// CallbackStyle is how a gateway delivers its callbacks and expects them to
// be answered.
type CallbackStyle struct {
	// Body reports that the payload is the raw request body, which is
	// capped in size.
	Body bool
	// UniqueParams requires every form parameter to occur exactly once.
	UniqueParams bool
	// TextReply acknowledges success with the plain text "success".
	TextReply bool
	// TextFailure answers a rejected callback with HTTP 400 and the error
	// text instead of the JSON result envelope.
	TextFailure bool
	// StatusFailure answers a rejected callback with an HTTP failure status
	// (400 for an ErrInvalidCallback, 500 otherwise) so the gateway retries
	// what may succeed later; a gateway that only reads the status.
	StatusFailure bool
}

// Gateway is the protocol of one payment gateway, bound to one configured
// payment method.
type Gateway interface {
	Platform() payment.Platform
	// ChargeCurrency is the currency the gateway collects; empty means the
	// system currency, unconverted.
	ChargeCurrency() string
	// NeedsNotifyURL reports whether starting a payment for the order
	// registers a callback URL with the gateway.
	NeedsNotifyURL(o *order.Order) bool
	// StartPayment creates, or resumes, the order's payment at the gateway
	// and returns what the buyer needs to pay.
	StartPayment(ctx context.Context, c Checkout) (*dto.CheckoutOrderResponse, error)
	// Reconcile consults the gateway before a pending order closes locally.
	// An error wrapping ErrUnconfirmed keeps the order pending.
	Reconcile(ctx context.Context, req CloseRequest) (Reconciliation, error)
	// ParseCallback authenticates a callback and extracts what it reports.
	ParseCallback(ctx context.Context, n Notification) (*Notice, error)
	// CheckOrder binds an authenticated notice to the order it names.
	CheckOrder(o *order.Order, notice *Notice) error
	// ConfirmPayment asks the gateway itself whether the notice's payment is
	// complete before it settles the order.
	ConfirmPayment(ctx context.Context, o *order.Order, notice *Notice) error
}
