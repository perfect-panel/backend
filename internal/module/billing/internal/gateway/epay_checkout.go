package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment/epay/protocol"
	"github.com/perfect-panel/backend/internal/module/billing/internal/settle"
	"github.com/perfect-panel/backend/pkg/requestmeta"
)

type epayPaymentSession struct {
	State   string `json:"state"`
	TradeNo string `json:"trade_no"`
	PayType string `json:"pay_type"`
	PayInfo string `json:"pay_info"`
}

// startAPI persists one create attempt before sending it. Replays return its
// saved payment entry. A timeout, crash or ambiguous response leaves the attempt
// fenced for callback/query reconciliation instead of issuing another charge.
func (g *epayGateway) startAPI(ctx context.Context, c Checkout) (*dto.CheckoutOrderResponse, error) {
	if c.Order.PaymentSession != "" {
		var session epayPaymentSession
		if err := json.Unmarshal([]byte(c.Order.PaymentSession), &session); err != nil ||
			session.State != "ready" || session.TradeNo == "" || session.TradeNo != c.Order.TradeNo {
			return nil, fmt.Errorf("EPay checkout attempt is awaiting reconciliation: %w", ErrUnconfirmed)
		}
		return session.checkout()
	}
	metadata, _ := requestmeta.From(ctx)
	var create func(context.Context) (*epayPaymentSession, error)
	if g.v2 != nil {
		request := protocol.CreatePaymentRequest{
			Payment: protocol.Payment{
				Type: g.config.Type, OutTradeNo: c.Order.OrderNo, Name: c.Subject,
				Money: payment.FormatAmount(c.Charge.Amount), NotifyURL: c.NotifyURL, ReturnURL: c.ReturnURL,
			},
			Method: "jump", ClientIP: metadata.ClientIP,
		}
		if err := request.Validate(); err != nil {
			return nil, err
		}
		create = func(ctx context.Context) (*epayPaymentSession, error) {
			result, err := g.v2.CreatePayment(ctx, request)
			if err != nil {
				return nil, err
			}
			return &epayPaymentSession{State: "ready", TradeNo: result.TradeNo, PayType: result.PayType, PayInfo: result.PayInfo}, nil
		}
	} else {
		request := protocol.V1CreatePaymentRequest{
			V1Payment: protocol.V1Payment{
				Type: g.config.Type, OutTradeNo: c.Order.OrderNo, Name: c.Subject,
				Money: payment.FormatAmount(c.Charge.Amount), NotifyURL: c.NotifyURL, ReturnURL: c.ReturnURL,
			},
			Device: "jump", ClientIP: metadata.ClientIP,
		}
		if err := request.Validate(); err != nil {
			return nil, err
		}
		create = func(ctx context.Context) (*epayPaymentSession, error) {
			result, err := g.v1.CreatePayment(ctx, request)
			if err != nil {
				return nil, err
			}
			session := &epayPaymentSession{State: "ready", TradeNo: result.TradeNo}
			switch {
			case result.PayURL != "":
				session.PayType, session.PayInfo = "jump", result.PayURL
			case result.QRCode != "":
				session.PayType, session.PayInfo = "qrcode", result.QRCode
			case result.URLScheme != "":
				session.PayType, session.PayInfo = "urlscheme", result.URLScheme
			}
			return session, nil
		}
	}
	if c.BeginSession == nil || c.CompleteSession == nil {
		return nil, errors.New("EPay API checkout requires durable payment session storage")
	}
	started, err := c.BeginSession(ctx)
	if err != nil {
		return nil, err
	}
	if !started {
		return nil, fmt.Errorf("EPay checkout already started or order is no longer pending: %w", ErrUnconfirmed)
	}
	session, err := create(ctx)
	if err != nil {
		return nil, fmt.Errorf("EPay create outcome requires reconciliation: %w: %w", err, ErrUnconfirmed)
	}
	if err := settle.ValidateTradeNo(session.TradeNo); err != nil {
		return nil, err
	}
	content, err := json.Marshal(session)
	if err != nil {
		return nil, err
	}
	saved, err := c.CompleteSession(ctx, session.TradeNo, string(content))
	if err != nil {
		return nil, err
	}
	if !saved {
		return nil, fmt.Errorf("EPay order changed before checkout could be saved: %w", ErrUnconfirmed)
	}
	return session.checkout()
}

func (s *epayPaymentSession) checkout() (*dto.CheckoutOrderResponse, error) {
	if strings.TrimSpace(s.PayInfo) == "" {
		return nil, errors.New("EPay checkout has no payment entry")
	}
	switch s.PayType {
	case "qrcode":
		return &dto.CheckoutOrderResponse{Type: "qr", CheckoutUrl: s.PayInfo}, nil
	case "jump", "urlscheme":
		u, err := url.Parse(s.PayInfo)
		if err != nil || u.User != nil {
			return nil, errors.New("invalid EPay checkout URL")
		}
		allowed := (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
		if s.PayType == "urlscheme" {
			switch u.Scheme {
			case "alipay", "alipays", "weixin", "mqqapi", "mqqwallet":
				allowed = true
			}
		}
		if !allowed {
			return nil, errors.New("unsupported EPay checkout URL scheme")
		}
		return &dto.CheckoutOrderResponse{Type: "url", CheckoutUrl: s.PayInfo}, nil
	default:
		// Existing browser checkout renders only redirects and QR codes. The
		// complete protocol client also exposes native/JSAPI payloads to future
		// integrations, but never labels HTML or native data as a redirect URL.
		return nil, fmt.Errorf("EPay pay_type %q requires a dedicated client integration; use page checkout", s.PayType)
	}
}
