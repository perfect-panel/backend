package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	dto "github.com/perfect-panel/server/internal/module/billing/contract"
	"github.com/perfect-panel/server/internal/module/billing/entity/order"
	paymentEntity "github.com/perfect-panel/server/internal/module/billing/entity/payment"
	"github.com/perfect-panel/server/internal/module/billing/internal/payment"
	"github.com/perfect-panel/server/internal/module/billing/internal/payment/epay"
	"github.com/perfect-panel/server/internal/module/billing/internal/payment/epay/protocol"
	"github.com/perfect-panel/server/internal/module/billing/internal/settle"
	"github.com/perfect-panel/server/pkg/logger"
)

// epayCurrency is the only currency EPay-compatible gateways collect.
const epayCurrency = "CNY"

type epayGateway struct {
	config         paymentEntity.EPayConfig
	client         *epay.Client
	v1             *protocol.V1Client
	v2             *protocol.Client
	siteRelative   bool
	schemeRelative bool
}

func openEPay(r *Registry, method *paymentEntity.Payment) (Gateway, error) {
	var config paymentEntity.EPayConfig
	if err := config.Unmarshal([]byte(method.Config)); err != nil {
		return nil, fmt.Errorf("decode EPay configuration: %w", err)
	}
	config.Version = strings.ToLower(strings.TrimSpace(config.Version))
	config.CheckoutMode = strings.ToLower(strings.TrimSpace(config.CheckoutMode))
	config.Url = strings.TrimSpace(config.Url)
	schemeRelative := strings.HasPrefix(config.Url, "//")
	endpoint, siteRelative, err := epayEndpoint(config.Url, method.Domain)
	if err != nil {
		return nil, err
	}
	config.Url = endpoint
	gw, err := newEPayGateway(config, r.httpClient)
	if err != nil {
		return nil, err
	}
	gw.siteRelative, gw.schemeRelative = siteRelative, schemeRelative
	return gw, nil
}

func epayEndpoint(raw, domain string) (string, bool, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		// Server-side queries have no browser scheme; use HTTPS while the
		// browser-facing checkout retains the configured scheme-relative URL.
		return "https:" + raw, false, nil
	}
	if !strings.HasPrefix(raw, "/") {
		return raw, false, nil
	}
	base := strings.TrimSuffix(strings.TrimSpace(domain), "/")
	if base == "" {
		return "", false, errors.New("a site-relative EPay URL requires the payment method domain")
	}
	return base + raw, true, nil
}

func normalizeEPay(raw []byte) (string, error) {
	var config paymentEntity.EPayConfig
	if err := config.Unmarshal(raw); err != nil {
		return "", err
	}
	configured := strings.TrimSpace(config.Url)
	strict := strings.EqualFold(strings.TrimSpace(config.Version), "v2") || strings.EqualFold(strings.TrimSpace(config.CheckoutMode), "api")
	if strict && strings.HasPrefix(configured, "/") {
		resolved, _, err := epayEndpoint(configured, "https://relative.invalid")
		if err != nil {
			return "", err
		}
		config.Url = resolved
	}
	validated, err := newEPayGateway(config, nil)
	if err != nil {
		return "", err
	}
	config = validated.config
	config.Url = configured
	content, err := config.Marshal()
	return string(content), err
}

func (g *epayGateway) Platform() payment.Platform { return payment.EPay }

func (g *epayGateway) ChargeCurrency() string { return epayCurrency }

func (g *epayGateway) NeedsNotifyURL(*order.Order) bool { return true }

// StartPayment builds a signed page redirect or reuses a durable API checkout.
func (g *epayGateway) StartPayment(ctx context.Context, c Checkout) (*dto.CheckoutOrderResponse, error) {
	if g.config.CheckoutMode == "api" {
		return g.startAPI(ctx, c)
	}
	if g.v2 != nil {
		form, err := g.v2.PagePayment(protocol.Payment{
			Type: g.config.Type, Name: c.Subject, OutTradeNo: c.Order.OrderNo,
			Money: payment.FormatAmount(c.Charge.Amount), NotifyURL: c.NotifyURL, ReturnURL: c.ReturnURL,
		})
		if err != nil {
			return nil, err
		}
		return &dto.CheckoutOrderResponse{Type: "url", CheckoutUrl: g.browserURL(form.URL())}, nil
	}
	payURL, err := g.client.CreatePayUrl(epay.Order{
		Name:      c.Subject,
		Amount:    c.Charge.Amount,
		OrderNo:   c.Order.OrderNo,
		SignType:  "MD5",
		NotifyUrl: c.NotifyURL,
		ReturnUrl: c.ReturnURL,
	})
	if err != nil {
		return nil, err
	}
	return &dto.CheckoutOrderResponse{Type: "url", CheckoutUrl: g.browserURL(payURL)}, nil
}

func (g *epayGateway) browserURL(payURL string) string {
	if g.siteRelative {
		if parsed, err := url.Parse(payURL); err == nil {
			return parsed.RequestURI()
		}
	} else if g.schemeRelative {
		return strings.TrimPrefix(payURL, "https:")
	}
	return payURL
}

// Reconcile queries the order at the gateway. Legacy V1-compatible gateways have
// no cancellation API, and a late callback on a closed order is rejected
// rather than reopened or refunded. A paid order is settled; an order the
// gateway explicitly lists as awaiting payment closes once it is
// order.UnpaidCloseAge old, so a payer who opened the gateway page near
// expiry still finishes. Any other answer — a failed, unsupported or
// unavailable query, or a status that is neither paid nor awaiting payment —
// leaves the payment state unknown, so the order stays pending for retry or
// manual resolution instead of losing funds. An explicit cancellation is the
// exception: absent any evidence of payment the close proceeds.
func (g *epayGateway) Reconcile(ctx context.Context, req CloseRequest) (Reconciliation, error) {
	o := req.Order
	if o.PaymentCurrency == "" {
		// Checkout never started; safe to close if it still has not when
		// the close commits.
		return Reconciliation{RequireStableCheckout: true}, nil
	}
	if g.v2 != nil {
		return g.reconcileV2(ctx, req)
	}
	result, err := g.client.QueryOrder(ctx, o.OrderNo)
	if err != nil {
		if req.Explicit {
			logger.WithContext(ctx).Infow("[CloseOrder] explicit close of EPay order without gateway confirmation",
				logger.Field("orderNo", o.OrderNo),
				logger.Field("queryError", err.Error()),
			)
			return Reconciliation{}, nil
		}
		return Reconciliation{}, fmt.Errorf("cannot safely expire EPay order %s: %w: %w", o.OrderNo, err, ErrUnconfirmed)
	}
	if !result.Paid {
		if req.Explicit {
			return Reconciliation{}, nil
		}
		if !result.Unpaid {
			return Reconciliation{}, fmt.Errorf("cannot safely expire EPay order %s: gateway reports it neither paid nor awaiting payment: %w", o.OrderNo, ErrUnconfirmed)
		}
		if time.Since(o.CreatedAt) < order.UnpaidCloseAge {
			return Reconciliation{}, fmt.Errorf("unpaid EPay order %s stays pending until it is %s old: %w", o.OrderNo, order.UnpaidCloseAge, ErrUnconfirmed)
		}
		return Reconciliation{}, nil // the gateway confirms no payment after the extended window.
	}
	if result.StatusOnly {
		return Reconciliation{}, fmt.Errorf("cannot safely reconcile paid EPay order %s: gateway query has no transaction details", o.OrderNo)
	}
	amount, err := payment.ParseAmount(result.Money)
	if err != nil || result.OrderNo != o.OrderNo || result.MerchantID != g.config.Pid ||
		(g.config.Type != "" && result.Type != g.config.Type) || result.Type == "" ||
		amount != o.PaymentAmount || result.TradeNo == "" || (o.TradeNo != "" && o.TradeNo != result.TradeNo) {
		return Reconciliation{}, fmt.Errorf("EPay order %s query does not match payment expectation", o.OrderNo)
	}
	return Reconciliation{TradeNo: result.TradeNo}, nil
}

type epayNotice struct {
	paymentType string
}

// ParseCallback verifies the configured protocol's signature and callback fields: the
// merchant, the payment type, a successful trade status and the amount.
func (g *epayGateway) ParseCallback(_ context.Context, n Notification) (*Notice, error) {
	params := n.Params
	if params == nil {
		params = map[string]string{}
	}
	if g.v2 != nil {
		if _, err := g.v2.VerifyNotification(singleValues(params)); err != nil {
			return nil, err
		}
	} else {
		if !g.client.VerifySign(params) {
			return nil, errors.New("verify sign failed")
		}
		if !strings.EqualFold(params["sign_type"], "MD5") {
			return nil, errors.New("unsupported signature type")
		}
	}
	orderNo, tradeNo := params["out_trade_no"], params["trade_no"]
	if orderNo == "" || len(orderNo) > 255 || strings.TrimSpace(orderNo) != orderNo {
		return nil, errors.New("invalid order number")
	}
	if err := settle.ValidateTradeNo(tradeNo); err != nil {
		return nil, err
	}
	if params["pid"] != g.config.Pid {
		return nil, errors.New("merchant id mismatch")
	}
	if g.config.Type != "" && params["type"] != g.config.Type {
		return nil, errors.New("payment type mismatch")
	}
	if params["trade_status"] != "TRADE_SUCCESS" {
		return nil, errors.New("trade status is not success")
	}
	amount, err := payment.ParseAmount(params["money"])
	if err != nil {
		return nil, errors.New("invalid callback money")
	}
	return &Notice{
		OrderNo:  orderNo,
		TradeNo:  tradeNo,
		Amount:   amount,
		Currency: epayCurrency,
		Paid:     true,
		Status:   params["trade_status"],
		detail:   epayNotice{paymentType: params["type"]},
	}, nil
}

func (g *epayGateway) CheckOrder(*order.Order, *Notice) error { return nil }

// ConfirmPayment re-queries the gateway. Only legacy V1 gateways without query
// support may be accepted on the strength of the verified signature alone.
func (g *epayGateway) ConfirmPayment(ctx context.Context, o *order.Order, notice *Notice) error {
	queried, err := g.queryOrder(ctx, o.OrderNo)
	if g.v2 == nil && errors.Is(err, epay.ErrQueryNotSupported) {
		logger.WithContext(ctx).Infow("[EPayNotify] Gateway does not support order query; accepting signature-verified callback",
			logger.Field("orderNo", o.OrderNo),
		)
		return nil
	}
	if err != nil {
		return err
	}
	detail, _ := notice.detail.(epayNotice)
	return g.validateQueried(queried, notice, detail.paymentType)
}

func (g *epayGateway) validateQueried(result *epay.QueryResult, notice *Notice, paymentType string) error {
	if result == nil || !result.Paid {
		return errors.New("gateway order is not paid")
	}
	// Some compatible gateways expose only a payment status from their query
	// endpoint. The callback is still signature-verified and its payment
	// fields are validated before this point, so status confirmation is
	// useful without pretending omitted fields were verified by the query.
	if result.StatusOnly {
		return nil
	}
	return g.validateQueriedDetails(result, notice, paymentType)
}

func (g *epayGateway) validateQueriedDetails(result *epay.QueryResult, notice *Notice, paymentType string) error {
	if result.OrderNo != notice.OrderNo {
		return errors.New("gateway order number mismatch")
	}
	if result.TradeNo == "" || result.TradeNo != notice.TradeNo {
		return errors.New("gateway trade number mismatch")
	}
	if result.MerchantID != g.config.Pid {
		return errors.New("gateway merchant id mismatch")
	}
	if result.Type == "" || result.Type != paymentType || (g.config.Type != "" && result.Type != g.config.Type) {
		return errors.New("gateway payment type mismatch")
	}
	queriedAmount, err := payment.ParseAmount(result.Money)
	if err != nil || queriedAmount != notice.Amount {
		return errors.New("gateway payment amount mismatch")
	}
	return nil
}
