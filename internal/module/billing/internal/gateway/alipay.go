package gateway

import (
	"context"
	"errors"
	"fmt"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	paymentEntity "github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment/alipay"
	"github.com/perfect-panel/backend/pkg/logger"
)

// alipayCurrency is the only currency face-to-face trades collect.
const alipayCurrency = "CNY"

type alipayGateway struct {
	config paymentEntity.AlipayF2FConfig
	client *alipay.Client
}

func openAlipay(r *Registry, method *paymentEntity.Payment) (Gateway, error) {
	var config paymentEntity.AlipayF2FConfig
	if err := config.Unmarshal([]byte(method.Config)); err != nil {
		return nil, fmt.Errorf("decode Alipay configuration: %w", err)
	}
	client, err := alipay.NewClient(alipay.Config{
		AppId:       config.AppId,
		PrivateKey:  config.PrivateKey,
		PublicKey:   config.PublicKey,
		InvoiceName: config.InvoiceName,
		Sandbox:     config.Sandbox,
		Gateway:     config.Gateway,
		HTTPClient:  r.httpClient,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Alipay client: %w", err)
	}
	return &alipayGateway{config: config, client: client}, nil
}

func normalizeAlipay(raw []byte) (string, error) {
	var config paymentEntity.AlipayF2FConfig
	if err := config.Unmarshal(raw); err != nil {
		return "", err
	}
	content, err := config.Marshal()
	return string(content), err
}

func (g *alipayGateway) Platform() payment.Platform { return payment.AlipayF2F }

func (g *alipayGateway) ChargeCurrency() string { return alipayCurrency }

func (g *alipayGateway) NeedsNotifyURL(*order.Order) bool { return true }

// StartPayment pre-creates the face-to-face trade and returns its QR code.
// The trade expires when the order's payment window ends, whenever the
// checkout started: a relative timeout counted from the pre-creation let a
// QR code issued late in the window outlive the order's close.
func (g *alipayGateway) StartPayment(ctx context.Context, c Checkout) (*dto.CheckoutOrderResponse, error) {
	qrCode, err := g.client.PreCreateTrade(ctx, alipay.Order{
		OrderNo:   c.Order.OrderNo,
		Amount:    c.Charge.Amount,
		NotifyURL: c.NotifyURL,
		ExpireAt:  c.Order.CreatedAt.Add(order.PaymentWindow),
	})
	if err != nil {
		return nil, err
	}
	return &dto.CheckoutOrderResponse{Type: "qr", CheckoutUrl: qrCode}, nil
}

// Reconcile asks the gateway about the trade. It creates a face-to-face trade
// only when the buyer scans the QR code, so a missing trade proves no money
// was collected so far. The QR code stays scannable until the trade expiry
// set at checkout, the end of the order's payment window, and a scan just
// before it may create the trade right after the query, so the expiry close
// waits until the order is order.UnpaidCloseAge old before it releases stock
// and coupons on a never-scanned code; the owner or an administrator may
// give the order up at once. Any existing trade must be reconciled before
// the local close: a paid trade is settled instead of cancelled — a lost
// payment notification would otherwise void an order the customer already
// paid for — and a scanned-but-unpaid trade is closed at the gateway first
// so its QR code cannot collect money afterwards.
func (g *alipayGateway) Reconcile(ctx context.Context, req CloseRequest) (Reconciliation, error) {
	o := req.Order
	if o.PaymentCurrency == "" {
		// Checkout never started; safe to close if it still has not when
		// the close commits.
		return Reconciliation{RequireStableCheckout: true}, nil
	}
	trade, err := g.client.QueryTrade(ctx, o.OrderNo)
	if errors.Is(err, alipay.ErrTradeNotExist) {
		if !req.Explicit && time.Since(o.CreatedAt) < order.UnpaidCloseAge {
			return Reconciliation{}, fmt.Errorf("unscanned Alipay order %s stays pending until it is %s old: %w", o.OrderNo, order.UnpaidCloseAge, ErrUnconfirmed)
		}
		return Reconciliation{}, nil // the QR code was never scanned and has expired; no money was collected.
	}
	if err != nil {
		if req.Explicit {
			logger.WithContext(ctx).Infow("[CloseOrder] explicit close of Alipay order without gateway confirmation",
				logger.Field("orderNo", o.OrderNo),
				logger.Field("queryError", err.Error()),
			)
			return Reconciliation{}, nil
		}
		return Reconciliation{}, fmt.Errorf("cannot safely expire Alipay order %s: %w: %w", o.OrderNo, err, ErrUnconfirmed)
	}
	if trade.Status.Paid() {
		return paidAlipayTrade(o, trade)
	}
	if trade.Status == alipay.Closed {
		return Reconciliation{}, nil // the gateway already voided the trade without payment.
	}
	// WAIT_BUYER_PAY: the buyer scanned but has not paid. Void the trade so
	// the QR code cannot collect money after the local close releases stock
	// and coupons; the gateway rejects the close once the trade is paid.
	closeErr := g.client.CloseTrade(ctx, o.OrderNo)
	if closeErr == nil || errors.Is(closeErr, alipay.ErrTradeNotExist) {
		return Reconciliation{}, nil
	}
	// A payment can finish between the query and the close attempt. Recheck
	// once so that case is settled rather than closed locally.
	trade, err = g.client.QueryTrade(ctx, o.OrderNo)
	if err == nil && trade.Status.Paid() {
		return paidAlipayTrade(o, trade)
	}
	if req.Explicit {
		return Reconciliation{}, nil // the owner or administrator forfeits the unconfirmed trade.
	}
	return Reconciliation{}, fmt.Errorf("cannot safely expire Alipay order %s: gateway close failed: %w: %w", o.OrderNo, closeErr, ErrUnconfirmed)
}

// paidAlipayTrade settles a trade the gateway reports as paid once the
// signed query response matches the payment expectation recorded at
// checkout.
func paidAlipayTrade(o *order.Order, trade *alipay.Trade) (Reconciliation, error) {
	if trade.OrderNo != o.OrderNo || trade.Amount != o.PaymentAmount || trade.TradeNo == "" {
		return Reconciliation{}, fmt.Errorf("alipay order %s query does not match payment expectation", o.OrderNo)
	}
	return Reconciliation{TradeNo: trade.TradeNo}, nil
}

type alipayNotice struct {
	appID string
}

// ParseCallback verifies the notification signature. Only a successful or
// finished trade concerns the order; other notifications are acknowledged.
func (g *alipayGateway) ParseCallback(ctx context.Context, n Notification) (*Notice, error) {
	notify, err := g.client.DecodeNotification(ctx, n.Form)
	if err != nil {
		return nil, err
	}
	if !notify.Status.Paid() {
		logger.WithContext(ctx).Errorw("[AlipayNotify] Notify status failed", logger.Field("status", string(notify.Status)))
		return &Notice{Ignore: true, Status: string(notify.Status)}, nil
	}
	return &Notice{
		OrderNo:  notify.OrderNo,
		TradeNo:  notify.TradeNo,
		Amount:   notify.Amount,
		Currency: alipayCurrency,
		Paid:     true,
		Status:   string(notify.Status),
		detail:   alipayNotice{appID: notify.AppId},
	}, nil
}

// CheckOrder requires the notification to come from the configured app.
func (g *alipayGateway) CheckOrder(_ *order.Order, notice *Notice) error {
	detail, _ := notice.detail.(alipayNotice)
	if detail.appID != g.config.AppId {
		return errors.New("alipay app id mismatch")
	}
	return nil
}

// ConfirmPayment requires the gateway to report the trade as paid.
func (g *alipayGateway) ConfirmPayment(ctx context.Context, o *order.Order, _ *Notice) error {
	trade, err := g.client.QueryTrade(ctx, o.OrderNo)
	if err != nil {
		return err
	}
	if !trade.Status.Paid() {
		return errors.New("alipay trade is not paid")
	}
	return nil
}
