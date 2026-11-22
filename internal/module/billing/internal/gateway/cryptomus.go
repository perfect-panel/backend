package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	paymentEntity "github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment/cryptomus"
	"github.com/perfect-panel/backend/internal/module/billing/internal/settle"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

type cryptomusGateway struct {
	client *cryptomus.Client
}

func openCryptomus(r *Registry, method *paymentEntity.Payment) (Gateway, error) {
	var config paymentEntity.CryptomusConfig
	if err := config.Unmarshal([]byte(method.Config)); err != nil {
		return nil, fmt.Errorf("decode Cryptomus configuration: %w", err)
	}
	if config.MerchantID == "" || config.APIKey == "" {
		return nil, errors.New("incomplete payment configuration")
	}
	return &cryptomusGateway{client: cryptomus.NewClient(cryptomus.Config{
		MerchantID: config.MerchantID,
		APIKey:     config.APIKey,
		BaseURL:    r.cryptomusBaseURL,
		HTTPClient: r.httpClient,
	})}, nil
}

// normalizeCryptomus trims the credentials and requires both.
func normalizeCryptomus(raw []byte) (string, error) {
	var config paymentEntity.CryptomusConfig
	if err := config.Unmarshal(raw); err != nil {
		return "", err
	}
	config.MerchantID = strings.TrimSpace(config.MerchantID)
	config.APIKey = strings.TrimSpace(config.APIKey)
	if config.MerchantID == "" || config.APIKey == "" {
		return "", errors.New("incomplete payment configuration")
	}
	content, err := config.Marshal()
	return string(content), err
}

func (g *cryptomusGateway) Platform() payment.Platform { return payment.Cryptomus }

func (g *cryptomusGateway) ChargeCurrency() string { return "" }

// NeedsNotifyURL is true only for a new invoice; a claimed invoice already
// carries its callback.
func (g *cryptomusGateway) NeedsNotifyURL(o *order.Order) bool { return o.TradeNo == "" }

// StartPayment returns the hosted checkout of the order's only invoice. The
// claimed invoice keeps every checkout retry on the same URL, so a callback
// can never reference an invoice the order does not know about.
func (g *cryptomusGateway) StartPayment(ctx context.Context, c Checkout) (*dto.CheckoutOrderResponse, error) {
	if c.Order.TradeNo != "" {
		invoice, err := g.client.GetInvoice(ctx, c.Order.TradeNo, "")
		if err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "Cryptomus invoice lookup")
		}
		return invoiceCheckout(invoice, c.Order)
	}
	invoice, err := g.client.CreateInvoice(ctx, cryptomus.Order{
		OrderNo:   c.Order.OrderNo,
		Amount:    c.Charge.Amount,
		Currency:  c.Charge.Currency,
		NotifyURL: c.NotifyURL,
		ReturnURL: c.ReturnURL,
		// The invoice must not outlive the order's close window: a payment
		// made after the order closed could no longer be fulfilled.
		Lifetime: int64(order.PaymentWindow.Seconds()),
	})
	if err != nil {
		// The gateway enforces one active invoice per order number. A
		// previous checkout may have created it and crashed before claiming
		// the trade number, so recover that invoice instead of failing.
		existing, queryErr := g.client.GetInvoice(ctx, "", c.Order.OrderNo)
		if queryErr != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "create Cryptomus invoice")
		}
		invoice = existing
	}
	winner, err := c.Claim(ctx, invoice.UUID)
	if err != nil {
		return nil, err
	}
	if winner != invoice.UUID {
		// A concurrent checkout claimed the order's invoice first; ours is
		// never referenced again and simply expires at the gateway.
		if invoice, err = g.client.GetInvoice(ctx, winner, ""); err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "Cryptomus invoice lookup")
		}
	}
	return invoiceCheckout(invoice, c.Order)
}

// invoiceCheckout guards a reused invoice against drift: the checkout URL
// handed to the payer must belong to this order and charge exactly the
// recorded payment expectation.
func invoiceCheckout(invoice *cryptomus.Invoice, o *order.Order) (*dto.CheckoutOrderResponse, error) {
	if invoice.OrderNo != o.OrderNo {
		return nil, errors.New("cryptomus invoice order mismatch")
	}
	amount, err := cryptomus.ParseMoney(invoice.Amount)
	if err != nil || amount != o.PaymentAmount || !strings.EqualFold(invoice.Currency, o.PaymentCurrency) {
		return nil, errors.New("cryptomus invoice does not match payment expectation")
	}
	if invoice.URL == "" {
		return nil, errors.New("cryptomus invoice has no checkout URL")
	}
	return &dto.CheckoutOrderResponse{Type: "url", CheckoutUrl: invoice.URL}, nil
}

// Reconcile confirms that no money was collected before the order closes.
// Invoices cannot be cancelled through the API, but they expire on their own
// after the checkout lifetime and report a final state. A paid invoice is
// settled instead; active, underpaid, AML-frozen and refund invoices stay
// pending for reconciliation or manual resolution. Nobody's cancellation
// request can invalidate the invoice or forfeit received funds.
func (g *cryptomusGateway) Reconcile(ctx context.Context, req CloseRequest) (Reconciliation, error) {
	o := req.Order
	// Checkout records its payment expectation before creating an invoice,
	// so every close verdict requires that checkout to be unchanged when the
	// close commits: a "never started" snapshot must not close an invoice
	// that is being created.
	if o.PaymentCurrency == "" && o.TradeNo == "" {
		return Reconciliation{RequireStableCheckout: true}, nil // checkout was never started.
	}
	// The trade number is claimed right after invoice creation, but a
	// checkout may have crashed between the two steps; the order-number
	// lookup still finds the invoice the gateway holds for this order.
	invoice, err := g.client.GetInvoice(ctx, o.TradeNo, o.OrderNo)
	if err != nil {
		// Even an explicit payment-not-found answer cannot close a started
		// checkout: invoice creation may still be in flight after a timeout,
		// before its UUID was persisted locally.
		return Reconciliation{}, fmt.Errorf("cannot safely expire Cryptomus order %s: %w: %w", o.OrderNo, err, ErrUnconfirmed)
	}
	// Validate identity and the immutable amount before trusting any state,
	// including a cancellation that would release stock and wallet credit.
	amount, err := cryptomus.ParseMoney(invoice.Amount)
	if err != nil || invoice.OrderNo != o.OrderNo ||
		(o.TradeNo != "" && invoice.UUID != o.TradeNo) ||
		amount != o.PaymentAmount || !strings.EqualFold(invoice.Currency, o.PaymentCurrency) {
		return Reconciliation{}, fmt.Errorf("cryptomus order %s query does not match payment expectation: %w", o.OrderNo, ErrUnconfirmed)
	}
	if invoice.Paid() {
		return Reconciliation{TradeNo: invoice.UUID}, nil
	}
	// is_final alone does not mean unpaid (e.g. refund_fail). Only an
	// explicitly cancelled invoice with zero received funds is safe to close.
	paidAmount, amountErr := cryptomus.ParseMoney(invoice.PaymentAmount)
	if invoice.IsFinal && invoice.State() == cryptomus.StatusCancel && amountErr == nil && paidAmount == 0 {
		return Reconciliation{RequireStableCheckout: true}, nil
	}
	return Reconciliation{}, fmt.Errorf("cannot safely expire Cryptomus order %s with invoice status %q: %w", o.OrderNo, invoice.State(), ErrUnconfirmed)
}

// ParseCallback verifies the webhook signature and the payload: an invoice
// payment of a known status for a well-formed order.
func (g *cryptomusGateway) ParseCallback(_ context.Context, n Notification) (*Notice, error) {
	if !g.client.VerifyNotificationSign(n.Body) {
		return nil, errors.New("verify sign failed")
	}
	notification, err := cryptomus.ParseNotification(n.Body)
	if err != nil {
		return nil, err
	}
	// The gateway also emits wallet-topup webhooks with the same shape; only
	// invoice payments may settle orders.
	if notification.Type != "" && notification.Type != "payment" {
		return nil, errors.New("unsupported notification type")
	}
	if notification.OrderNo == "" || len(notification.OrderNo) > 255 || strings.TrimSpace(notification.OrderNo) != notification.OrderNo {
		return nil, errors.New("invalid order number")
	}
	if err := settle.ValidateTradeNo(notification.UUID); err != nil {
		return nil, err
	}
	if !cryptomus.KnownStatus(notification.Status) {
		return nil, errors.New("unknown payment status")
	}
	amount, err := cryptomus.ParseMoney(notification.Amount)
	if err != nil {
		return nil, errors.New("invalid callback amount")
	}
	notice := &Notice{
		OrderNo:  notification.OrderNo,
		TradeNo:  notification.UUID,
		Amount:   amount,
		Currency: notification.Currency,
		Paid:     cryptomus.PaidStatus(notification.Status),
		Status:   notification.Status,
		Fields: []logger.LogField{
			logger.Field("tradeNo", notification.UUID),
			logger.Field("is_final", notification.IsFinal),
			logger.Field("payment_amount", notification.PaymentAmount),
			logger.Field("payer_currency", notification.PayerCurrency),
		},
	}
	switch notification.Status {
	case cryptomus.StatusWrongAmount, cryptomus.StatusLocked,
		cryptomus.StatusRefundProcess, cryptomus.StatusRefundFail, cryptomus.StatusRefundPaid:
		notice.ManualReview = true
	}
	return notice, nil
}

// CheckOrder requires the invoice to be the one the order claimed.
func (g *cryptomusGateway) CheckOrder(o *order.Order, notice *Notice) error {
	if o.TradeNo != "" && o.TradeNo != notice.TradeNo {
		return errors.New("order trade number mismatch")
	}
	return nil
}

// ConfirmPayment re-reads the invoice from the gateway: the signature proves
// the gateway sent the webhook, the invoice proves the payment.
func (g *cryptomusGateway) ConfirmPayment(ctx context.Context, o *order.Order, notice *Notice) error {
	invoice, err := g.client.GetInvoice(ctx, notice.TradeNo, "")
	if err != nil {
		return err
	}
	if invoice == nil || !invoice.Paid() {
		return errors.New("gateway invoice is not paid")
	}
	if invoice.UUID != notice.TradeNo || invoice.OrderNo != o.OrderNo {
		return errors.New("gateway invoice identity mismatch")
	}
	amount, err := cryptomus.ParseMoney(invoice.Amount)
	if err != nil || amount != o.PaymentAmount || !strings.EqualFold(invoice.Currency, o.PaymentCurrency) {
		return errors.New("gateway invoice amount mismatch")
	}
	return nil
}
