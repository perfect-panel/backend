package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	paymentEntity "github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment/paypal"
)

type paypalGateway struct{ client *paypal.Client }

func normalizePayPal(raw []byte) (string, error) {
	var cfg paypal.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "", err
	}
	if err := cfg.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(cfg) // #nosec G117 -- admin-only stored merchant credentials.
	return string(data), err
}

func openPayPal(r *Registry, m *paymentEntity.Payment) (Gateway, error) {
	var cfg paypal.Config
	if err := json.Unmarshal([]byte(m.Config), &cfg); err != nil {
		return nil, err
	}
	client, err := paypal.NewClient(cfg, r.httpClient)
	if err != nil {
		return nil, err
	}
	return &paypalGateway{client: client}, nil
}

func (*paypalGateway) Platform() payment.Platform { return payment.PayPal }

func (*paypalGateway) ChargeCurrency() string { return "" }

func (*paypalGateway) NeedsNotifyURL(*order.Order) bool { return false }

type paypalSession struct {
	ID  string `json:"id"`
	URL string `json:"approval_url"`
}

func (g *paypalGateway) StartPayment(ctx context.Context, c Checkout) (*dto.CheckoutOrderResponse, error) {
	if c.Order.PaymentSession != "" {
		var s paypalSession
		if err := json.Unmarshal([]byte(c.Order.PaymentSession), &s); err != nil || s.ID == "" || s.ID != c.Order.TradeNo || !paypal.ApprovalURL(s.URL) {
			return nil, fmt.Errorf("PayPal checkout awaits confirmation: %w", ErrUnconfirmed)
		}
		return &dto.CheckoutOrderResponse{Type: "url", CheckoutUrl: s.URL}, nil
	}
	returnURL := c.ReturnURL
	if returnURL == "" {
		returnURL, _ = publicBaseURL(firstLine(c.SiteHost))
	}
	body, err := g.client.CreateBody(c.Order.OrderNo, c.Charge.Amount, c.Charge.Currency, returnURL)
	if err != nil {
		return nil, err
	}
	if c.BeginSession == nil || c.CompleteSession == nil {
		return nil, errors.New("PayPal checkout requires payment session storage")
	}
	started, err := c.BeginSession(ctx)
	if err != nil {
		return nil, err
	}
	if !started {
		return nil, ErrUnconfirmed
	}
	remote, err := g.client.Create(ctx, c.Order.OrderNo, body)
	if err != nil {
		return nil, fmt.Errorf("PayPal create: %w: %w", err, ErrUnconfirmed)
	}
	s := paypalSession{ID: remote.ID, URL: remote.ApprovalLink()}
	if s.URL == "" || !remote.Matches(c.Order.OrderNo, c.Charge.Amount, c.Charge.Currency) ||
		(remote.Status != "CREATED" && remote.Status != "PAYER_ACTION_REQUIRED") {
		return nil, fmt.Errorf("unexpected PayPal checkout: %w", ErrUnconfirmed)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	saved, err := c.CompleteSession(ctx, s.ID, string(raw))
	if err != nil {
		return nil, err
	}
	if !saved {
		return nil, ErrUnconfirmed
	}
	return &dto.CheckoutOrderResponse{Type: "url", CheckoutUrl: s.URL}, nil
}

func (g *paypalGateway) Reconcile(ctx context.Context, r CloseRequest) (Reconciliation, error) {
	o := r.Order
	stableClose := Reconciliation{RequireStableCheckout: true}
	if o.TradeNo == "" {
		// This integration can only capture a bound order. A concurrent bind
		// or capture changes the session checked by the closing transaction.
		return stableClose, nil
	}
	remote, err := g.client.Get(ctx, o.TradeNo)
	// PayPal can discard an expired, unapproved order. Only a checkout for
	// which we never reserved a capture can close on a missing resource.
	if errors.Is(err, paypal.ErrOrderNotFound) && o.PaymentSession != order.PaymentSessionCapturing {
		return stableClose, nil
	}
	if err != nil {
		return Reconciliation{}, fmt.Errorf("query PayPal order: %w: %w", err, ErrUnconfirmed)
	}
	if !remote.Matches(o.OrderNo, o.PaymentAmount, o.PaymentCurrency) {
		return Reconciliation{}, fmt.Errorf("PayPal payment expectation mismatch: %w", ErrUnconfirmed)
	}
	if remote.Paid(o.PaymentAmount, o.PaymentCurrency, "") {
		return Reconciliation{TradeNo: o.TradeNo}, nil
	}
	if len(remote.Units[0].Payments.Captures) > 0 || len(remote.Units[0].Payments.Refunds) > 0 {
		return Reconciliation{}, ErrUnconfirmed
	}
	switch remote.Status {
	case "VOIDED":
		return stableClose, nil
	case "CREATED", "SAVED", "PAYER_ACTION_REQUIRED":
		if o.PaymentSession != order.PaymentSessionCapturing {
			return stableClose, nil
		}
	case "APPROVED":
		if o.Status != order.StatusPending {
			return Reconciliation{}, ErrUnconfirmed
		}
		return g.capture(ctx, r)
	}
	return Reconciliation{}, ErrUnconfirmed
}

func (g *paypalGateway) capture(ctx context.Context, r CloseRequest) (Reconciliation, error) {
	o := r.Order
	if o.PaymentSession != order.PaymentSessionCapturing {
		if r.ReserveCapture == nil {
			return Reconciliation{}, errors.New("missing PayPal capture reservation")
		}
		ok, err := r.ReserveCapture(ctx, o.OrderNo, o.TradeNo)
		if err != nil {
			return Reconciliation{}, err
		}
		if !ok {
			return Reconciliation{}, ErrUnconfirmed
		}
	}
	// An ambiguous POST (or an already-captured response) may have succeeded.
	// Re-read the order before settling. Redeliveries use the same capture key.
	captureErr := g.client.Capture(ctx, o.TradeNo)
	remote, err := g.client.Get(ctx, o.TradeNo)
	if err == nil && remote.Matches(o.OrderNo, o.PaymentAmount, o.PaymentCurrency) && remote.Paid(o.PaymentAmount, o.PaymentCurrency, "") {
		return Reconciliation{TradeNo: o.TradeNo}, nil
	}
	return Reconciliation{}, errors.Join(ErrUnconfirmed, captureErr, err)
}

func (g *paypalGateway) ParseCallback(ctx context.Context, n Notification) (*Notice, error) {
	if n.HTTPMethod != http.MethodPost {
		return nil, paypal.ErrInvalidWebhook
	}
	event, err := g.client.Verify(ctx, n.PayPalTransmission, n.Body)
	if errors.Is(err, paypal.ErrInvalidWebhook) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("PayPal verification unavailable: %w: %w", err, ErrUnconfirmed)
	}
	var id, captureID string
	switch event.Type {
	case paypal.Approved:
		id = event.Resource.ID
	case paypal.Captured:
		id = event.Resource.Related.IDs.OrderID
		captureID = event.Resource.ID
		if captureID == "" {
			return nil, paypal.ErrInvalidWebhook
		}
	default:
		return &Notice{Ignore: true}, nil
	}
	if id == "" {
		return nil, paypal.ErrInvalidWebhook
	}
	// Capture events need not include the merchant invoice number. Resolve it
	// using the configured merchant's authenticated order, never a callback URL.
	remote, err := g.client.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("PayPal event order lookup: %w: %w", err, ErrUnconfirmed)
	}
	unit, err := remote.Unit()
	if err != nil {
		return nil, err
	}
	amount, err := payment.ParseAmount(unit.Amount.Value)
	if err != nil || amount <= 0 || unit.Amount.Currency == "" {
		return nil, errors.New("invalid PayPal order amount")
	}
	if event.Type == paypal.Captured && !remote.Paid(amount, unit.Amount.Currency, captureID) {
		return nil, ErrUnconfirmed
	}
	return &Notice{OrderNo: unit.Invoice, TradeNo: id, Amount: amount, Currency: unit.Amount.Currency,
		Paid: event.Type == paypal.Captured, Approved: event.Type == paypal.Approved, Status: event.Type, detail: remote}, nil
}

func (*paypalGateway) CheckOrder(o *order.Order, n *Notice) error {
	if o.TradeNo == "" || o.TradeNo != n.TradeNo {
		return errors.New("PayPal event does not match the bound order")
	}
	return nil
}

func (*paypalGateway) ConfirmPayment(_ context.Context, o *order.Order, n *Notice) error {
	// ParseCallback has fetched this order from PayPal and matched the capture ID.
	remote, ok := n.detail.(*paypal.Order)
	if !ok || remote.ID != o.TradeNo || !remote.Matches(o.OrderNo, o.PaymentAmount, o.PaymentCurrency) || !remote.Paid(o.PaymentAmount, o.PaymentCurrency, "") {
		return ErrUnconfirmed
	}
	return nil
}
