package gateway

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	paymentEntity "github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment/stripe"
	"github.com/perfect-panel/backend/pkg/xerr"
	stripeSDK "github.com/stripe/stripe-go/v81"
)

const stripePaymentSucceeded = "payment_intent.succeeded"

type stripeGateway struct {
	config paymentEntity.StripeConfig
	client *stripe.Client
}

func openStripe(r *Registry, method *paymentEntity.Payment) (Gateway, error) {
	var config paymentEntity.StripeConfig
	if err := config.Unmarshal([]byte(method.Config)); err != nil {
		return nil, fmt.Errorf("decode Stripe configuration: %w", err)
	}
	return &stripeGateway{config: config, client: newStripeClient(r, config)}, nil
}

func newStripeClient(r *Registry, config paymentEntity.StripeConfig) *stripe.Client {
	return stripe.NewClient(stripe.Config{
		PublicKey:     config.PublicKey,
		SecretKey:     config.SecretKey,
		WebhookSecret: config.WebhookSecret,
		Backends:      r.stripeBackends,
	})
}

func normalizeStripe(raw []byte) (string, error) {
	var config paymentEntity.StripeConfig
	if err := config.Unmarshal(raw); err != nil {
		return "", err
	}
	content, err := config.Marshal()
	return string(content), err
}

func (g *stripeGateway) Platform() payment.Platform { return payment.Stripe }

func (g *stripeGateway) ChargeCurrency() string { return "" }

// RoundCharge rounds the charge to what Stripe can collect in its currency.
func (g *stripeGateway) RoundCharge(c Charge) Charge {
	c.Amount = stripe.RoundHundredths(c.Amount, c.Currency)
	return c
}

// NeedsNotifyURL is false: Stripe delivers events to the webhook endpoint
// registered when the payment method was created.
func (g *stripeGateway) NeedsNotifyURL(*order.Order) bool { return false }

// StartPayment returns the client secret of the order's only PaymentIntent.
// Reusing it is essential: accepting two client secrets would let a buyer
// pay an older intent whose callback no longer matches the stored trade
// number.
func (g *stripeGateway) StartPayment(ctx context.Context, c Checkout) (*dto.CheckoutOrderResponse, error) {
	intent := g.intentOrder(c.Order.OrderNo, strconv.FormatInt(c.Order.SubscribeId, 10), c.Charge)
	var (
		sheet *stripe.PaymentSheet
		err   error
	)
	if c.Order.TradeNo != "" {
		sheet, err = g.client.GetPaymentSheet(ctx, intent, c.Order.TradeNo)
	} else {
		sheet, err = g.client.CreatePaymentSheet(ctx, intent, &stripe.User{UserId: c.Payer.UserID, Email: c.Payer.Email})
		if err == nil {
			sheet, err = g.claimIntent(ctx, c, intent, sheet)
		}
	}
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "Stripe payment sheet")
	}
	return &dto.CheckoutOrderResponse{
		Type: "stripe",
		Stripe: &dto.StripePayment{
			PublishableKey: g.config.PublicKey,
			ClientSecret:   sheet.ClientSecret,
			Method:         g.config.Payment,
		},
	}, nil
}

// claimIntent binds a new intent to the order. When a concurrent checkout
// won the order's one intent, ours is cancelled and the winner's sheet is
// returned instead.
func (g *stripeGateway) claimIntent(ctx context.Context, c Checkout, intent *stripe.Order, sheet *stripe.PaymentSheet) (*stripe.PaymentSheet, error) {
	winner, err := c.Claim(ctx, sheet.TradeNo)
	if err != nil {
		_ = g.client.CancelPaymentIntent(ctx, sheet.TradeNo)
		return nil, err
	}
	if winner == sheet.TradeNo {
		return sheet, nil
	}
	if err := g.client.CancelPaymentIntent(ctx, sheet.TradeNo); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "cancel duplicate Stripe payment intent")
	}
	return g.client.GetPaymentSheet(ctx, intent, winner)
}

func (g *stripeGateway) intentOrder(orderNo, subscribe string, charge Charge) *stripe.Order {
	return &stripe.Order{
		OrderNo:   orderNo,
		Subscribe: subscribe,
		Amount:    charge.Amount,
		Currency:  strings.ToLower(charge.Currency),
		Payment:   g.config.Payment,
	}
}

// Reconcile settles an intent that succeeded and cancels one that did not,
// so a still-pending client secret cannot be paid after the local close. The
// intent is verified against the payment expectation recorded at checkout.
// An intent that is already canceled is terminal at Stripe and can never be
// paid, so the order closes without asking Stripe to cancel it again, which
// Stripe refuses.
func (g *stripeGateway) Reconcile(ctx context.Context, req CloseRequest) (Reconciliation, error) {
	o := req.Order
	if o.TradeNo == "" {
		// No intent was claimed. The checkout records its expectation before
		// it creates the intent, so the close must find the order unchanged
		// when it commits.
		return Reconciliation{RequireStableCheckout: true}, nil
	}
	charge := Charge{Amount: o.PaymentAmount, Currency: o.PaymentCurrency}
	if o.PaymentCurrency == "" {
		// An intent created before payment expectations were recorded
		// charged the order amount in the site currency.
		charge = Charge{Amount: o.Amount, Currency: req.SystemCurrency}
	}
	intent := g.intentOrder(o.OrderNo, "", charge)
	status, err := g.client.PaymentIntentStatus(ctx, intent, o.TradeNo)
	if err != nil {
		return Reconciliation{}, err
	}
	if verdict, final := stripeVerdict(o.TradeNo, status); final {
		return verdict, nil
	}
	if err := g.client.CancelPaymentIntent(ctx, o.TradeNo); err == nil {
		return Reconciliation{}, nil
	}
	// A payment can finish, or the intent be canceled elsewhere, between the
	// status query and the cancellation. Recheck once so a paid intent is
	// settled rather than closed locally and a canceled one closes.
	status, err = g.client.PaymentIntentStatus(ctx, intent, o.TradeNo)
	if err != nil {
		return Reconciliation{}, err
	}
	if verdict, final := stripeVerdict(o.TradeNo, status); final {
		return verdict, nil
	}
	return Reconciliation{}, fmt.Errorf("cancel Stripe payment intent %s failed", o.TradeNo)
}

// stripeVerdict maps a terminal intent status to the close verdict: a
// succeeded intent settles the order, a canceled one lets it close. Any
// other status is not final.
func stripeVerdict(tradeNo string, status stripeSDK.PaymentIntentStatus) (Reconciliation, bool) {
	switch status {
	case stripeSDK.PaymentIntentStatusSucceeded:
		return Reconciliation{TradeNo: tradeNo}, true
	case stripeSDK.PaymentIntentStatusCanceled:
		return Reconciliation{}, true
	default:
		return Reconciliation{}, false
	}
}

type stripeNotice struct {
	method string
}

// ParseCallback verifies the webhook signature. Only a succeeded
// PaymentIntent concerns the order; other events are acknowledged.
func (g *stripeGateway) ParseCallback(_ context.Context, n Notification) (*Notice, error) {
	notify, err := g.client.ParseNotify(n.Body, n.Signature)
	if err != nil {
		return nil, err
	}
	if notify.EventType != stripePaymentSucceeded {
		return &Notice{Ignore: true, Status: notify.EventType}, nil
	}
	return &Notice{
		OrderNo:  notify.OrderNo,
		TradeNo:  notify.TradeNo,
		Amount:   notify.Amount,
		Currency: strings.ToUpper(notify.Currency),
		Paid:     true,
		Status:   notify.EventType,
		detail:   stripeNotice{method: notify.Method},
	}, nil
}

// CheckOrder requires the intent to use the method's payment type.
func (g *stripeGateway) CheckOrder(_ *order.Order, notice *Notice) error {
	detail, _ := notice.detail.(stripeNotice)
	if detail.method == "" || detail.method != g.config.Payment {
		return errors.New("stripe payment method mismatch")
	}
	return nil
}

// ConfirmPayment requires the intent to have succeeded at Stripe.
func (g *stripeGateway) ConfirmPayment(ctx context.Context, _ *order.Order, notice *Notice) error {
	paid, err := g.client.QueryOrderStatus(ctx, notice.TradeNo)
	if err != nil {
		return err
	}
	if !paid {
		return errors.New("stripe payment intent is not paid")
	}
	return nil
}

// StripeWebhooks manages the webhook endpoint of a Stripe payment method.
type StripeWebhooks interface {
	CreateWebhookEndpoint(ctx context.Context, url string) (id, secret string, err error)
	DeleteWebhookEndpoint(ctx context.Context, id string) error
}

// StripeWebhooks returns the webhook management of the Stripe account whose
// secret key is secretKey.
func (r *Registry) StripeWebhooks(secretKey string) StripeWebhooks {
	return stripeWebhooks{client: newStripeClient(r, paymentEntity.StripeConfig{SecretKey: secretKey})}
}

type stripeWebhooks struct {
	client *stripe.Client
}

func (w stripeWebhooks) CreateWebhookEndpoint(ctx context.Context, url string) (string, string, error) {
	endpoint, err := w.client.CreateWebhookEndpoint(ctx, url)
	if err != nil {
		return "", "", err
	}
	return endpoint.ID, endpoint.Secret, nil
}

func (w stripeWebhooks) DeleteWebhookEndpoint(ctx context.Context, id string) error {
	return w.client.DeleteWebhookEndpoint(ctx, id)
}
