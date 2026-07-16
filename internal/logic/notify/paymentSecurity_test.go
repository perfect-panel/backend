package notify

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/model/entity/payment"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/payment/alipay"
	"github.com/perfect-panel/server/pkg/payment/epay"
	"github.com/perfect-panel/server/pkg/payment/stripe"
	queueType "github.com/perfect-panel/server/queue/types"
	"gorm.io/gorm"
)

type callbackOrderRepo struct {
	repository.OrderRepo
	order     *order.Order
	markCount int
}

func (r *callbackOrderRepo) FindOneByOrderNo(_ context.Context, orderNo string) (*order.Order, error) {
	if r.order.OrderNo != orderNo {
		return nil, errUnexpectedOrder
	}
	return r.order, nil
}

func (r *callbackOrderRepo) MarkOrderPaid(_ context.Context, orderNo, tradeNo string, _ ...*gorm.DB) (bool, error) {
	if r.order.OrderNo != orderNo || r.order.Status != orderStatusPending {
		return false, nil
	}
	r.order.Status = orderStatusPaid
	r.order.TradeNo = tradeNo
	r.markCount++
	return true, nil
}

type callbackStore struct {
	repository.Store
	orders repository.OrderRepo
}

func (s callbackStore) Order() repository.OrderRepo { return s.orders }

var errUnexpectedOrder = errors.New("unexpected order")

func TestEPayNotifyRejectsInvalidSignatureWhenDebugEnabled(t *testing.T) {
	paymentConfig := &payment.Payment{
		Id:       10,
		Platform: "EPay",
		Config:   `{"pid":"1001","url":"https://pay.example","key":"secret","type":"alipay"}`,
	}
	ctx := context.WithValue(context.Background(), constant.CtxKeyPayment, paymentConfig)
	logic := NewEPayNotifyLogic(ctx, &svc.ServiceContext{Config: config.Config{Debug: true}}, EPayNotifyMeta{
		Method: "POST",
		Params: map[string]string{
			"out_trade_no": "order-1",
			"trade_status": "TRADE_SUCCESS",
			"sign":         "invalid",
		},
	})

	err := logic.EPayNotify(&dto.EPayNotifyRequest{OutTradeNo: "order-1", TradeStatus: "TRADE_SUCCESS", Sign: "invalid"})
	if err == nil || !strings.Contains(err.Error(), "verify sign failed") {
		t.Fatalf("debug mode must still reject invalid signature, got %v", err)
	}
}

func TestCryptoSaaSNotifyRejectsInvalidSignatureWhenDebugEnabled(t *testing.T) {
	paymentConfig := &payment.Payment{
		Id:       11,
		Platform: "CryptoSaaS",
		Config:   `{"endpoint":"https://crypto.example","account_id":"account-1","secret_key":"secret","type":"usdt"}`,
	}
	ctx := context.WithValue(context.Background(), constant.CtxKeyPayment, paymentConfig)
	logic := NewEPayNotifyLogic(ctx, &svc.ServiceContext{Config: config.Config{Debug: true}}, EPayNotifyMeta{
		Method: "POST",
		Params: map[string]string{
			"out_trade_no": "order-1",
			"trade_status": "TRADE_SUCCESS",
			"sign":         "invalid",
		},
	})

	err := logic.EPayNotify(&dto.EPayNotifyRequest{OutTradeNo: "order-1", TradeStatus: "TRADE_SUCCESS", Sign: "invalid"})
	if err == nil || !strings.Contains(err.Error(), "verify sign failed") {
		t.Fatalf("debug mode must still reject invalid CryptoSaaS signature, got %v", err)
	}
}

func TestEPayNotifySettlesOnlyAfterSignedAndQueriedDetailsMatch(t *testing.T) {
	queryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code": 1, "pid": "1001", "trade_no": "trade-1", "out_trade_no": "order-1",
			"type": "alipay", "money": "10.00", "status": 1,
		})
	}))
	defer queryServer.Close()

	redisServer := miniredis.RunT(t)
	queue := asynq.NewClient(asynq.RedisClientOpt{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = queue.Close() })

	paymentConfig := &payment.Payment{
		Id:       10,
		Platform: "EPay",
		Config:   `{"pid":"1001","url":"` + queryServer.URL + `","key":"secret","type":"alipay"}`,
	}
	orders := &callbackOrderRepo{order: &order.Order{
		OrderNo: "order-1", PaymentId: 10, Method: "EPay", Status: orderStatusPending,
		PaymentAmount: 1000, PaymentCurrency: "CNY",
	}}
	params := map[string]string{
		"pid": "1001", "trade_no": "trade-1", "out_trade_no": "order-1", "type": "alipay",
		"name": "product", "money": "10.00", "trade_status": "TRADE_SUCCESS", "param": "", "sign_type": "MD5",
	}
	params["sign"] = signEPayTestParams(params, "secret")
	ctx := context.WithValue(context.Background(), constant.CtxKeyPayment, paymentConfig)
	logic := NewEPayNotifyLogic(ctx, &svc.ServiceContext{
		Store: callbackStore{orders: orders},
		Queue: queue,
	}, EPayNotifyMeta{Method: "POST", Params: params})

	req := &dto.EPayNotifyRequest{
		Pid: "1001", TradeNo: "trade-1", OutTradeNo: "order-1", Type: "alipay", Name: "product",
		Money: "10.00", TradeStatus: "TRADE_SUCCESS", Sign: params["sign"], SignType: "MD5",
	}
	err := logic.EPayNotify(req)
	if err != nil {
		t.Fatalf("EPayNotify: %v", err)
	}
	if err := logic.EPayNotify(req); err != nil {
		t.Fatalf("duplicate EPayNotify must be idempotent: %v", err)
	}
	if orders.markCount != 1 || orders.order.Status != orderStatusPaid || orders.order.TradeNo != "trade-1" {
		t.Fatalf("order was not settled exactly once: %+v, marks=%d", orders.order, orders.markCount)
	}
}

func TestEPayCredentialsUseCryptoSaaSConfiguration(t *testing.T) {
	credentials, err := epayCredentialsForPayment(&payment.Payment{
		Platform: "CryptoSaaS",
		Config:   `{"endpoint":"https://crypto.example","account_id":"account-1","secret_key":"secret","type":"usdt"}`,
	})
	if err != nil {
		t.Fatalf("epayCredentialsForPayment: %v", err)
	}
	if credentials.merchantID != "account-1" || credentials.endpoint != "https://crypto.example" || credentials.key != "secret" || credentials.paymentType != "usdt" {
		t.Fatalf("unexpected credentials: %+v", credentials)
	}
}

func TestValidateOrderPaymentRequiresExactConfigurationBinding(t *testing.T) {
	paymentConfig := &payment.Payment{Id: 10, Platform: "EPay"}
	if err := validateOrderPayment(&order.Order{PaymentId: 10, Method: "EPay"}, paymentConfig); err != nil {
		t.Fatalf("matching payment binding rejected: %v", err)
	}
	if err := validateOrderPayment(&order.Order{PaymentId: 11, Method: "EPay"}, paymentConfig); err == nil {
		t.Fatal("mismatched payment id must be rejected")
	}
	if err := validateOrderPayment(&order.Order{PaymentId: 10, Method: "Stripe"}, paymentConfig); err == nil {
		t.Fatal("mismatched payment platform must be rejected")
	}
}

func TestValidatePaymentExpectationRequiresAmountAndCurrency(t *testing.T) {
	orderInfo := &order.Order{PaymentAmount: 1000, PaymentCurrency: "CNY"}
	if err := validatePaymentExpectation(orderInfo, 1000, "cny"); err != nil {
		t.Fatalf("matching expectation rejected: %v", err)
	}
	if err := validatePaymentExpectation(orderInfo, 999, "CNY"); err == nil {
		t.Fatal("amount mismatch must be rejected")
	}
	if err := validatePaymentExpectation(orderInfo, 1000, "USD"); err == nil {
		t.Fatal("currency mismatch must be rejected")
	}
	if err := validatePaymentExpectation(&order.Order{PaymentAmount: 1000}, 1000, "CNY"); err == nil {
		t.Fatal("missing checkout snapshot must fail closed")
	}
}

func TestValidateQueriedEPayOrderRejectsGatewayMismatch(t *testing.T) {
	req := &dto.EPayNotifyRequest{Pid: "1001", OutTradeNo: "order-1", TradeNo: "trade-1", Type: "alipay", Money: "10.00"}
	credentials := epayCredentials{merchantID: "1001", paymentType: "alipay"}
	valid := &epay.QueryResult{MerchantID: "1001", OrderNo: "order-1", TradeNo: "trade-1", Type: "alipay", Money: "10.00", Paid: true}
	if err := validateQueriedEPayOrder(valid, req, credentials, 1000); err != nil {
		t.Fatalf("matching gateway order rejected: %v", err)
	}
	changed := *valid
	changed.Money = "1.00"
	if err := validateQueriedEPayOrder(&changed, req, credentials, 1000); err == nil {
		t.Fatal("gateway amount mismatch must be rejected")
	}
	changed = *valid
	changed.MerchantID = "other"
	if err := validateQueriedEPayOrder(&changed, req, credentials, 1000); err == nil {
		t.Fatal("gateway merchant mismatch must be rejected")
	}
	changed = *valid
	changed.Paid = false
	if err := validateQueriedEPayOrder(&changed, req, credentials, 1000); err == nil {
		t.Fatal("unpaid gateway order must be rejected")
	}
}

func TestActivationTaskIDIsDeterministicPerOrder(t *testing.T) {
	first := queueType.ActivationTaskID("order-1")
	if first != queueType.ActivationTaskID("order-1") {
		t.Fatal("activation task id must be deterministic")
	}
	if first == queueType.ActivationTaskID("order-2") {
		t.Fatal("different orders must not share an activation task id")
	}
}

func TestFinishedOrderDuplicateRequiresSameTradeNumber(t *testing.T) {
	orderInfo := &order.Order{Status: orderStatusFinished, TradeNo: "trade-1"}
	finished, err := finishedOrderDuplicate(orderInfo, "trade-1")
	if err != nil || !finished {
		t.Fatalf("matching finished duplicate rejected: finished=%t err=%v", finished, err)
	}
	if _, err := finishedOrderDuplicate(orderInfo, "trade-2"); err == nil {
		t.Fatal("finished callback with another trade number must be rejected")
	}
}

func TestCancelledOrFailedOrderCannotSettle(t *testing.T) {
	for _, status := range []uint8{3, 4} {
		if err := validateOrderCanSettle(&order.Order{Status: status}); err == nil {
			t.Fatalf("status %d must not be settled", status)
		}
	}
}

func TestStripeCallbackRequiresBoundConfigAmountCurrencyAndMethod(t *testing.T) {
	paymentConfig := &payment.Payment{Id: 20, Platform: "Stripe"}
	stripeConfig := &payment.StripeConfig{Payment: "card"}
	orderInfo := &order.Order{
		PaymentId: 20, Method: "Stripe", Status: orderStatusPending,
		PaymentAmount: 1000, PaymentCurrency: "USD", TradeNo: "pi_1",
	}
	notify := &stripe.NotifyResult{TradeNo: "pi_1", Method: "card", Amount: 1000, Currency: "usd"}
	if finished, err := validateStripeCallback(orderInfo, paymentConfig, stripeConfig, notify); err != nil || finished {
		t.Fatalf("valid Stripe callback rejected: finished=%t err=%v", finished, err)
	}
	changed := *notify
	changed.Amount = 999
	if _, err := validateStripeCallback(orderInfo, paymentConfig, stripeConfig, &changed); err == nil {
		t.Fatal("Stripe amount mismatch must be rejected")
	}
	changed = *notify
	changed.Currency = "eur"
	if _, err := validateStripeCallback(orderInfo, paymentConfig, stripeConfig, &changed); err == nil {
		t.Fatal("Stripe currency mismatch must be rejected")
	}
	changed = *notify
	changed.Method = "wechat_pay"
	if _, err := validateStripeCallback(orderInfo, paymentConfig, stripeConfig, &changed); err == nil {
		t.Fatal("Stripe payment method mismatch must be rejected")
	}
}

func TestAlipayCallbackRequiresBoundAppAndExactAmount(t *testing.T) {
	paymentConfig := &payment.Payment{Id: 30, Platform: "AlipayF2F"}
	alipayConfig := &payment.AlipayF2FConfig{AppId: "app-1"}
	orderInfo := &order.Order{
		PaymentId: 30, Method: "AlipayF2F", Status: orderStatusPending,
		PaymentAmount: 1000, PaymentCurrency: "CNY",
	}
	notify := &alipay.Notification{TradeNo: "trade-1", AppId: "app-1", Amount: 1000}
	if finished, err := validateAlipayCallback(orderInfo, paymentConfig, alipayConfig, notify); err != nil || finished {
		t.Fatalf("valid Alipay callback rejected: finished=%t err=%v", finished, err)
	}
	changed := *notify
	changed.AppId = "other-app"
	if _, err := validateAlipayCallback(orderInfo, paymentConfig, alipayConfig, &changed); err == nil {
		t.Fatal("Alipay app id mismatch must be rejected")
	}
	changed = *notify
	changed.Amount = 999
	if _, err := validateAlipayCallback(orderInfo, paymentConfig, alipayConfig, &changed); err == nil {
		t.Fatal("Alipay amount mismatch must be rejected")
	}
}

func signEPayTestParams(params map[string]string, key string) string {
	keys := make([]string, 0, len(params))
	for name, value := range params {
		if value != "" && name != "sign" && name != "sign_type" {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, name := range keys {
		parts = append(parts, name+"="+params[name])
	}
	digest := md5.Sum([]byte(strings.Join(parts, "&") + key))
	return hex.EncodeToString(digest[:])
}
