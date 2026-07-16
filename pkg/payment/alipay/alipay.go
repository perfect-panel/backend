package alipay

import (
	"context"
	"net/url"

	"github.com/perfect-panel/server/pkg/logger"
	paymentUtil "github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/pkg/errors"
	"github.com/smartwalle/alipay/v3"
)

type Config struct {
	AppId       string
	PrivateKey  string
	PublicKey   string
	InvoiceName string
	NotifyURL   string
	Sandbox     bool
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

type Client struct {
	Config
	client *alipay.Client
}
type Order struct {
	OrderNo string
	Amount  int64
}

func NewClient(c Config) *Client {
	client, err := alipay.New(c.AppId, c.PrivateKey, !c.Sandbox)
	if err != nil {
		logger.Error("[Alipay] NewClient failed: ", logger.Field("errors", err), logger.Field("appId", c.AppId), logger.Field("sandbox", c.Sandbox))
		return nil
	}
	err = client.LoadAliPayPublicKey(c.PublicKey)
	if err != nil {
		logger.Error("[Alipay] Load public key failed: ", logger.Field("errors", err), logger.Field("appId", c.AppId), logger.Field("sandbox", c.Sandbox))
		return nil
	}
	return &Client{
		Config: c,
		client: client,
	}
}

func (c *Client) PreCreateTrade(ctx context.Context, order Order) (string, error) {
	amountString := tool.FormatFloat(float64(order.Amount)/float64(100), 2)
	trade, err := c.client.TradePreCreate(ctx, alipay.TradePreCreate{
		Trade: alipay.Trade{
			OutTradeNo:  order.OrderNo,
			TotalAmount: amountString,
			Subject:     c.InvoiceName,
			NotifyURL:   c.NotifyURL,
		},
	})
	if err != nil {
		return "", err
	}
	if trade.Code != alipay.CodeSuccess {
		return "", errors.New("PreCreateTrade failed: " + trade.Msg)
	}
	return trade.QRCode, nil
}

func (c *Client) QueryTrade(ctx context.Context, orderNo string) (Status, error) {
	trade, err := c.client.TradeQuery(ctx, alipay.TradeQuery{
		OutTradeNo: orderNo,
	})
	if err != nil {
		return Error, err
	}
	switch trade.TradeStatus {
	case alipay.TradeStatusSuccess:
		return Success, nil
	case alipay.TradeStatusWaitBuyerPay:
		return Pending, nil
	case alipay.TradeStatusClosed:
		return Closed, nil
	case alipay.TradeStatusFinished:
		return Finished, nil
	default:
		return Error, errors.New("QueryTrade failed: " + trade.Msg)
	}
}

func (c *Client) DecodeNotification(form url.Values) (*Notification, error) {
	notify, err := c.client.DecodeNotification(form)
	if err != nil {
		return nil, err
	}
	amount, err := paymentUtil.ParseAmount(notify.TotalAmount)
	if err != nil {
		return nil, errors.Wrap(err, "invalid notification amount")
	}

	return &Notification{
		OrderNo: notify.OutTradeNo,
		Amount:  amount,
		Status:  Status(notify.TradeStatus),
		TradeNo: notify.TradeNo,
		AppId:   notify.AppId,
	}, nil
}
