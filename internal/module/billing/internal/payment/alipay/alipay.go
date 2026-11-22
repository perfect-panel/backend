// Package alipay implements the Alipay face-to-face payment protocol: QR code
// trade creation, trade query and close, and signed notification decoding.
package alipay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
	"github.com/smartwalle/alipay/v3"
)

type Config struct {
	AppId       string
	PrivateKey  string
	PublicKey   string
	InvoiceName string
	NotifyURL   string
	Sandbox     bool
	// Gateway overrides the sandbox gateway URL; Alipay has retired sandbox
	// hosts before, and tests point it at a local fake gateway. Ignored in
	// production, where only the official gateway may receive credentials.
	Gateway string
	// HTTPClient sends the gateway requests; nil selects the SDK default.
	HTTPClient *http.Client
}

type Notification struct {
	OrderNo string
	Amount  int64
	Status  Status
	TradeNo string
	AppId   string
}

type Status string

const (
	Success  Status = "TRADE_SUCCESS"
	Pending  Status = "WAIT_BUYER_PAY"
	Closed   Status = "TRADE_CLOSED"
	Finished Status = "TRADE_FINISHED"
	Error    Status = "TRADE_ERROR"
)

// Paid reports whether the status proves the buyer's money was collected;
// TRADE_FINISHED is the terminal paid state after the refund window closes.
func (s Status) Paid() bool {
	return s == Success || s == Finished
}

// ErrTradeNotExist reports that the gateway holds no trade for the order.
// A face-to-face trade is created only when the buyer scans the QR code, so
// a missing trade proves no money was collected for the order.
var ErrTradeNotExist = errors.New("alipay trade does not exist")

const tradeNotExistSubCode = "ACQ.TRADE_NOT_EXIST"

// Trade is the gateway's authoritative view of an order as returned by the
// alipay.trade.query endpoint. Amount is populated only for paid trades.
type Trade struct {
	OrderNo string
	TradeNo string
	Amount  int64
	Status  Status
}

type Client struct {
	Config
	client *alipay.Client
}

// Order is a face-to-face trade to create. Amount is in CNY minor units and
// is sent to the gateway exactly, as FormatAmount renders it. NotifyURL
// overrides the client's configured callback. ExpireAt is when the trade
// stops accepting payment; the zero time falls back to the relative
// fallbackTimeout counted from the pre-creation.
type Order struct {
	OrderNo   string
	Amount    int64
	NotifyURL string
	ExpireAt  time.Time
}

// fallbackTimeout is the relative payment window of a trade created without
// an absolute expiry, the local payment window.
const fallbackTimeout = "15m"

// gatewayZone is the zone Alipay reads absolute times in (UTC+8), whatever
// the server's zone; it is fixed so a host without tzdata renders it too.
var gatewayZone = time.FixedZone("CST", 8*60*60)

// FormatTimeExpire renders an absolute trade expiry as the gateway expects
// it: yyyy-MM-dd HH:mm:ss in UTC+8.
func FormatTimeExpire(at time.Time) string {
	return at.In(gatewayZone).Format(time.DateTime)
}

// NewClient loads the merchant key and the Alipay public key; a key that
// does not parse makes the method unusable.
func NewClient(c Config) (*Client, error) {
	opts := []alipay.OptionFunc{alipay.WithHTTPClient(c.HTTPClient)}
	if c.Gateway != "" {
		opts = append(opts, alipay.WithSandboxGateway(c.Gateway))
	}
	client, err := alipay.New(c.AppId, c.PrivateKey, !c.Sandbox, opts...)
	if err != nil {
		return nil, fmt.Errorf("load Alipay merchant private key: %w", err)
	}
	if err := client.LoadAliPayPublicKey(c.PublicKey); err != nil {
		return nil, fmt.Errorf("load Alipay public key: %w", err)
	}
	return &Client{
		Config: c,
		client: client,
	}, nil
}

func (c *Client) PreCreateTrade(ctx context.Context, order Order) (string, error) {
	trade, err := c.client.TradePreCreate(ctx, c.preCreateRequest(order))
	if err != nil {
		return "", err
	}
	if trade.Code != alipay.CodeSuccess {
		return "", errors.New("PreCreateTrade failed: " + trade.Msg)
	}
	return trade.QRCode, nil
}

// preCreateRequest is the alipay.trade.precreate request for order.
func (c *Client) preCreateRequest(order Order) alipay.TradePreCreate {
	notifyURL := order.NotifyURL
	if notifyURL == "" {
		notifyURL = c.NotifyURL
	}
	trade := alipay.Trade{
		OutTradeNo:  order.OrderNo,
		TotalAmount: payment.FormatAmount(order.Amount),
		Subject:     c.InvoiceName,
		NotifyURL:   notifyURL,
	}
	// The trade must stop accepting payment when the local order closes,
	// or a QR code could be paid after the order was closed and its reserved
	// gift credit, coupon use and inventory were restored. The absolute
	// expiry is the order's own deadline; a relative timeout would run from
	// the pre-creation, letting a checkout late in the window outlive it.
	if order.ExpireAt.IsZero() {
		trade.TimeoutExpress = fallbackTimeout
	} else {
		trade.TimeExpire = FormatTimeExpire(order.ExpireAt)
	}
	return alipay.TradePreCreate{Trade: trade}
}

func (c *Client) QueryTrade(ctx context.Context, orderNo string) (*Trade, error) {
	rsp, err := c.client.TradeQuery(ctx, alipay.TradeQuery{
		OutTradeNo: orderNo,
	})
	if err != nil {
		return nil, asTradeNotExist(err)
	}
	if rsp.Code != alipay.CodeSuccess {
		if rsp.SubCode == tradeNotExistSubCode {
			return nil, ErrTradeNotExist
		}
		return nil, errors.New("QueryTrade failed: " + rsp.Msg + " " + rsp.SubMsg)
	}
	trade := &Trade{
		OrderNo: rsp.OutTradeNo,
		TradeNo: rsp.TradeNo,
	}
	switch rsp.TradeStatus {
	case alipay.TradeStatusSuccess:
		trade.Status = Success
	case alipay.TradeStatusWaitBuyerPay:
		trade.Status = Pending
	case alipay.TradeStatusClosed:
		trade.Status = Closed
	case alipay.TradeStatusFinished:
		trade.Status = Finished
	default:
		return nil, errors.New("QueryTrade failed: unexpected trade status " + string(rsp.TradeStatus))
	}
	if trade.Status.Paid() {
		amount, err := payment.ParseAmount(rsp.TotalAmount)
		if err != nil {
			return nil, fmt.Errorf("invalid trade amount: %w", err)
		}
		trade.Amount = amount
	}
	return trade, nil
}

// CloseTrade voids an unpaid trade at the gateway so its QR code can no
// longer collect money. Closing a trade the gateway never created reports
// ErrTradeNotExist, and the gateway rejects closing a trade that was already
// paid, so a success here proves no payment can arrive afterwards.
func (c *Client) CloseTrade(ctx context.Context, orderNo string) error {
	rsp, err := c.client.TradeClose(ctx, alipay.TradeClose{
		OutTradeNo: orderNo,
	})
	if err != nil {
		return asTradeNotExist(err)
	}
	if rsp.Code != alipay.CodeSuccess {
		if rsp.SubCode == tradeNotExistSubCode {
			return ErrTradeNotExist
		}
		return errors.New("CloseTrade failed: " + rsp.Msg + " " + rsp.SubMsg)
	}
	return nil
}

// asTradeNotExist maps the SDK's business failure for a missing trade — which
// the SDK surfaces as an error when the gateway response carries no signature
// — onto ErrTradeNotExist and passes every other error through unchanged.
func asTradeNotExist(err error) error {
	var gatewayErr *alipay.Error
	if errors.As(err, &gatewayErr) && gatewayErr.SubCode == tradeNotExistSubCode {
		return ErrTradeNotExist
	}
	return err
}

// DecodeNotification verifies and decodes an asynchronous notification;
// ctx bounds the SDK's fetch of Alipay's certificates when they are not
// cached yet.
func (c *Client) DecodeNotification(ctx context.Context, form url.Values) (*Notification, error) {
	notify, err := c.client.DecodeNotification(ctx, form)
	if err != nil {
		return nil, err
	}
	amount, err := payment.ParseAmount(notify.TotalAmount)
	if err != nil {
		return nil, fmt.Errorf("invalid notification amount: %w", err)
	}

	return &Notification{
		OrderNo: notify.OutTradeNo,
		Amount:  amount,
		Status:  Status(notify.TradeStatus),
		TradeNo: notify.TradeNo,
		AppId:   notify.AppId,
	}, nil
}
