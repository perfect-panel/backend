package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	dto "github.com/perfect-panel/server/internal/module/billing/contract"
	"github.com/perfect-panel/server/internal/module/billing/entity/order"
	paymentEntity "github.com/perfect-panel/server/internal/module/billing/entity/payment"
	"github.com/perfect-panel/server/internal/module/billing/internal/payment"
	"github.com/perfect-panel/server/internal/module/billing/internal/payment/nano"
	"github.com/perfect-panel/server/pkg/logger"
)

// nanoGateway accepts Nano (XNO) on the nano:mainnet exact scheme. There is
// no provider: the payer sends a block straight to the configured account,
// then posts its hash to the notify URL. The gateway reads that block from
// the operator's node and settles the order only when the block is a
// confirmed send of exactly the order's raw amount whose representative
// commits to the order. The block hash becomes the order's trade number,
// which the order table keeps unique, so one block settles one order once,
// across restarts.
type nanoGateway struct {
	payTo    string
	currency string
	rate     nano.Rate
	node     *nano.Node
}

func openNano(r *Registry, method *paymentEntity.Payment) (Gateway, error) {
	var config paymentEntity.NanoConfig
	if err := config.Unmarshal([]byte(method.Config)); err != nil {
		return nil, fmt.Errorf("decode Nano configuration: %w", err)
	}
	rate, err := checkNanoConfig(&config)
	if err != nil {
		return nil, err
	}
	return &nanoGateway{
		payTo:    config.PayTo,
		currency: config.Currency,
		rate:     rate,
		node:     &nano.Node{URL: config.NodeURL, HTTPClient: r.httpClient},
	}, nil
}

// normalizeNano trims the configuration and requires a checksummed account,
// an https node (http only on loopback), a currency code and a rate that
// prices every minor unit in whole raw.
func normalizeNano(raw []byte) (string, error) {
	var config paymentEntity.NanoConfig
	if err := config.Unmarshal(raw); err != nil {
		return "", err
	}
	if _, err := checkNanoConfig(&config); err != nil {
		return "", err
	}
	content, err := config.Marshal()
	return string(content), err
}

func checkNanoConfig(config *paymentEntity.NanoConfig) (nano.Rate, error) {
	config.PayTo = strings.TrimSpace(config.PayTo)
	config.NodeURL = strings.TrimSpace(config.NodeURL)
	config.Currency = strings.ToUpper(strings.TrimSpace(config.Currency))
	config.XNOPerUnit = strings.TrimSpace(config.XNOPerUnit)
	if err := nano.ValidateAccount(config.PayTo); err != nil {
		return nano.Rate{}, fmt.Errorf("nano pay_to: %w", err)
	}
	if err := nano.ValidateNodeURL(config.NodeURL); err != nil {
		return nano.Rate{}, err
	}
	if len(config.Currency) != 3 || strings.Trim(config.Currency, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
		return nano.Rate{}, errors.New("nano currency must be a three-letter code")
	}
	rate, err := nano.ParseRate(config.XNOPerUnit)
	if err != nil {
		return nano.Rate{}, fmt.Errorf("nano xno_per_unit: %w", err)
	}
	return rate, nil
}

func (g *nanoGateway) Platform() payment.Platform { return payment.Nano }

// ChargeCurrency is the currency the configured rate prices; ChargeFor
// converts the order into it when it is not the site currency.
func (g *nanoGateway) ChargeCurrency() string { return g.currency }

// NeedsNotifyURL is true while the order is unpaid: the payer posts the
// block hash there.
func (g *nanoGateway) NeedsNotifyURL(o *order.Order) bool { return o.TradeNo == "" }

// expectation is the one payment the order accepts, derived from the
// expectation recorded at checkout.
func (g *nanoGateway) expectation(o *order.Order) (nano.Expectation, error) {
	if !strings.EqualFold(o.PaymentCurrency, g.currency) {
		return nano.Expectation{}, errors.New("nano payment currency does not match the order")
	}
	raw, err := g.rate.Raw(o.PaymentAmount)
	if err != nil {
		return nano.Expectation{}, err
	}
	return nano.Expectation{OrderNo: o.OrderNo, PayTo: g.payTo, Raw: raw}, nil
}

// StartPayment returns the nano: URI of the order: the account, the exact
// amount in raw and the representative that binds the send to this order.
// Nothing is created remotely, so every retry returns the same URI.
func (g *nanoGateway) StartPayment(_ context.Context, c Checkout) (*dto.CheckoutOrderResponse, error) {
	if c.Charge.Amount != c.Order.PaymentAmount || !strings.EqualFold(c.Charge.Currency, g.currency) {
		return nil, errors.New("nano charge does not match payment expectation")
	}
	want, err := g.expectation(c.Order)
	if err != nil {
		return nil, err
	}
	raw := want.Raw.String()
	uri := nano.PaymentURI(g.payTo, raw, nano.CommitmentAccount(want.OrderNo, g.payTo, raw))
	if c.NotifyURL != "" {
		uri += "&callback=" + url.QueryEscape(c.NotifyURL)
	}
	return &dto.CheckoutOrderResponse{Type: "qr", CheckoutUrl: uri}, nil
}

// Reconcile cannot ask the ledger whether an unknown block paid the order,
// so a started checkout stays pending for the unpaid-close age, during which
// the payer can still post its block. After that, or when a person gives the
// order up, it closes; a block posted later is recorded as an unmatched
// payment for the operator.
func (g *nanoGateway) Reconcile(_ context.Context, req CloseRequest) (Reconciliation, error) {
	o := req.Order
	if o.PaymentCurrency == "" && o.TradeNo == "" {
		return Reconciliation{RequireStableCheckout: true}, nil // checkout was never started.
	}
	if !req.Explicit && time.Since(o.CreatedAt) < order.UnpaidCloseAge {
		return Reconciliation{}, fmt.Errorf("unpaid Nano order %s stays pending until it is %s old: %w", o.OrderNo, order.UnpaidCloseAge, ErrUnconfirmed)
	}
	return Reconciliation{RequireStableCheckout: true}, nil
}

// nanoCallback is what the payer posts: the order and the block that pays it.
type nanoCallback struct {
	OrderNo string `json:"order_no"`
	Block   string `json:"block"`
}

// ParseCallback reads the posted block from the node. The callback itself
// is unauthenticated; the block is the proof, and ConfirmPayment checks it
// against the order before anything settles. The notice reports the amount
// the block carries, in the order's minor units.
func (g *nanoGateway) ParseCallback(ctx context.Context, n Notification) (*Notice, error) {
	var cb nanoCallback
	if err := json.Unmarshal(n.Body, &cb); err != nil {
		return nil, errors.New("invalid Nano callback")
	}
	if cb.OrderNo == "" || len(cb.OrderNo) > 255 || strings.TrimSpace(cb.OrderNo) != cb.OrderNo {
		return nil, errors.New("invalid order number")
	}
	hash, err := nano.NormalizeBlockHash(cb.Block)
	if err != nil {
		return nil, err
	}
	block, err := g.node.BlockInfo(ctx, hash)
	if err != nil {
		return nil, err
	}
	if !block.Confirmed || !block.Send {
		return nil, nano.ErrNotConfirmed
	}
	if !nano.SameAccount(block.Destination, g.payTo) {
		return nil, nano.ErrWrongPayTo
	}
	amount, err := g.rate.Minor(block.Amount)
	if err != nil {
		return nil, nano.ErrWrongAmount
	}
	return &Notice{
		OrderNo:  cb.OrderNo,
		TradeNo:  hash,
		Amount:   amount,
		Currency: g.currency,
		Paid:     true,
		Status:   "confirmed",
		Fields: []logger.LogField{
			logger.Field("tradeNo", hash),
			logger.Field("payer", block.Account),
			logger.Field("raw", block.Amount.String()),
		},
	}, nil
}

// CheckOrder requires the block to be the one the order was settled with,
// if any.
func (g *nanoGateway) CheckOrder(o *order.Order, notice *Notice) error {
	if o.TradeNo != "" && o.TradeNo != notice.TradeNo {
		return errors.New("order trade number mismatch")
	}
	return nil
}

// ConfirmPayment re-reads the block and checks it against the order's own
// expectation: destination, exact raw amount and the representative that
// commits to this order. A block bound to another order never settles this
// one.
func (g *nanoGateway) ConfirmPayment(ctx context.Context, o *order.Order, notice *Notice) error {
	want, err := g.expectation(o)
	if err != nil {
		return err
	}
	block, err := g.node.BlockInfo(ctx, notice.TradeNo)
	if err != nil {
		return err
	}
	if block.Hash != notice.TradeNo {
		return errors.New("nano block identity mismatch")
	}
	return nano.Verify(block, want)
}
