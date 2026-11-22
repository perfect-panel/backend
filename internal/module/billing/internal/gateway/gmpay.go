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
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment/gmpay"
)

type gmpayGateway struct{ client *gmpay.Client }

func openGMPay(r *Registry, method *paymentEntity.Payment) (Gateway, error) {
	var cfg gmpay.Config
	if err := json.Unmarshal([]byte(method.Config), &cfg); err != nil {
		return nil, err
	}
	client, err := gmpay.NewClient(cfg, r.httpClient)
	if err != nil {
		return nil, err
	}
	return &gmpayGateway{client: client}, nil
}

func normalizeGMPay(raw []byte) (string, error) {
	var cfg gmpay.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "", err
	}
	if err := cfg.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(cfg) // #nosec G117 -- persisted merchant configuration for admin-only access.
	return string(data), err
}

func (*gmpayGateway) Platform() payment.Platform         { return payment.GMPay }
func (*gmpayGateway) ChargeCurrency() string             { return "" }
func (*gmpayGateway) NeedsNotifyURL(o *order.Order) bool { return o.PaymentSession == "" }

func (g *gmpayGateway) StartPayment(ctx context.Context, c Checkout) (*dto.CheckoutOrderResponse, error) {
	if c.Order.PaymentSession != "" {
		var session gmpay.Cashier
		if err := json.Unmarshal([]byte(c.Order.PaymentSession), &session); err != nil || !session.Valid() || session.TradeID != c.Order.TradeNo {
			return nil, fmt.Errorf("GMPay checkout awaits reconciliation: %w", ErrUnconfirmed)
		}
		return &dto.CheckoutOrderResponse{Type: "url", CheckoutUrl: session.PaymentURL}, nil
	}
	request := gmpay.CreateRequest{
		OrderID: c.Order.OrderNo, Currency: c.Charge.Currency, Amount: c.Charge.Amount,
		NotifyURL: c.NotifyURL, ReturnURL: c.ReturnURL, Subject: c.Subject,
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if c.BeginSession == nil || c.CompleteSession == nil {
		return nil, errors.New("GMPay checkout requires payment session storage")
	}
	started, err := c.BeginSession(ctx)
	if err != nil {
		return nil, err
	}
	if !started {
		return nil, fmt.Errorf("GMPay checkout already started or order no longer pending: %w", ErrUnconfirmed)
	}
	created, err := g.client.Create(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("GMPay create outcome requires reconciliation: %w: %w", err, ErrUnconfirmed)
	}
	session := created.Cashier
	raw, err := json.Marshal(session)
	if err != nil {
		return nil, err
	}
	saved, err := c.CompleteSession(ctx, session.TradeID, string(raw))
	if err != nil {
		return nil, err
	}
	if !saved {
		return nil, fmt.Errorf("order changed during GMPay checkout: %w", ErrUnconfirmed)
	}
	return &dto.CheckoutOrderResponse{Type: "url", CheckoutUrl: session.PaymentURL}, nil
}

func (g *gmpayGateway) ParseCallback(_ context.Context, n Notification) (*Notice, error) {
	if n.HTTPMethod != http.MethodPost {
		return nil, errors.New("GMPay callback requires POST")
	}
	notice, err := g.client.VerifyNotification(n.Body)
	if err != nil {
		return nil, err
	}
	return &Notice{OrderNo: notice.OrderID, TradeNo: notice.TradeID, Amount: notice.Amount, Paid: true, Status: "2"}, nil
}

func (*gmpayGateway) CheckOrder(o *order.Order, n *Notice) error {
	// An authenticated callback can recover a create whose response was lost.
	if o.TradeNo == "" && o.PaymentSession != order.PaymentSessionStarting {
		return errors.New("GMPay callback does not match the order's payment attempt")
	}
	// The signed callback omits fiat currency. ConfirmPayment independently
	// verifies the gateway's currency against this snapshot before settlement.
	n.Currency = o.PaymentCurrency
	return nil
}

func (g *gmpayGateway) confirmedStatus(ctx context.Context, o *order.Order, tradeID string) (int, error) {
	detail, err := g.client.Detail(ctx, tradeID)
	if err != nil {
		return 0, err
	}
	if detail.OrderID != "" && detail.OrderID != o.OrderNo {
		return 0, errors.New("GMPay transaction belongs to another order")
	}
	if !detail.MatchesCharge(o.PaymentAmount, o.PaymentCurrency) {
		return 0, errors.New("GMPay transaction does not match order amount or currency")
	}
	return detail.Status, nil
}

func (g *gmpayGateway) ConfirmPayment(ctx context.Context, o *order.Order, n *Notice) error {
	status, err := g.confirmedStatus(ctx, o, n.TradeNo)
	if err != nil {
		return err
	}
	if status != gmpay.Paid {
		return errors.New("GMPay transaction has not been confirmed paid")
	}
	return nil
}

func (g *gmpayGateway) Reconcile(ctx context.Context, req CloseRequest) (Reconciliation, error) {
	o := req.Order
	if o.TradeNo == "" {
		if o.PaymentSession == "" {
			return Reconciliation{RequireStableCheckout: true}, nil
		}
		return Reconciliation{}, fmt.Errorf("GMPay create has no confirmed trade ID: %w", ErrUnconfirmed)
	}
	status, err := g.confirmedStatus(ctx, o, o.TradeNo)
	if err != nil {
		return Reconciliation{}, fmt.Errorf("GMPay reconciliation: %w: %w", err, ErrUnconfirmed)
	}
	switch status {
	case gmpay.Paid:
		return Reconciliation{TradeNo: o.TradeNo}, nil
	case gmpay.Expired:
		return Reconciliation{RequireStableCheckout: true}, nil
	default:
		return Reconciliation{}, fmt.Errorf("GMPay transaction in status %d: %w", status, ErrUnconfirmed)
	}
}
