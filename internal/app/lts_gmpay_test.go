package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	billdto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	billingHTTP "github.com/perfect-panel/backend/internal/module/billing/transport/http"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	orderTask "github.com/perfect-panel/backend/internal/transport/task/order"
)

// Run through scripts/acceptance/gmpay.sh. epusdt owns the real cashier,
// SQLite order ledger, callback signature and ACK verification.
func TestLTSGMPayMerchant(t *testing.T) {
	peer := os.Getenv("PPANEL_ACCEPT_GMPAY_URL")
	if peer == "" {
		t.Skip("run with the controlled epusdt peer")
	}
	f := newLTSFixture(t)
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(os.Getenv("PPANEL_ACCEPT_GMPAY_CA"))) {
		t.Fatal("missing local peer CA")
	}
	original := http.DefaultTransport
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	http.DefaultTransport = tr
	t.Cleanup(func() { http.DefaultTransport = original; tr.CloseIdleConnections() })
	h := server.New(server.WithDisablePrintRoute(true))
	billingHTTP.RegisterNotifyHandlers(h, f.Billing)
	merchant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read", 400)
			return
		}
		res := ut.PerformRequest(h.Engine, r.Method, r.URL.RequestURI(), &ut.Body{Body: bytes.NewReader(raw), Len: len(raw)}, ut.Header{Key: "Content-Type", Value: r.Header.Get("Content-Type")})
		if res.Code != 200 || res.Body.String() != "success" {
			t.Logf("local callback %s: %d %s", r.URL.Path, res.Code, res.Body.String())
		}
		w.WriteHeader(res.Code)
		_, _ = w.Write(res.Body.Bytes())
	}))
	defer merchant.Close()
	if err := os.WriteFile(filepath.Join(os.Getenv("PPANEL_ACCEPT_DIR"), "merchant.url"), []byte(merchant.URL), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := json.Marshal(map[string]string{"url": peer, "pid": "1001", "secret_key": "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	on := true
	method := &payment.Payment{Name: "GMPay acceptance", Platform: "GMPay", Domain: "https://93.184.216.34", Token: "local-receiving-method", Enable: &on, Config: string(cfg)}
	f.create(method)
	ctx := user.NewContext(t.Context(), f.account)
	post := func(path, body string, want int) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, peer+path, bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != want {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("peer %s: %d %s", path, resp.StatusCode, raw)
		}
	}
	for _, typ := range []uint8{order.TypeSubscribe, order.TypeRecharge} {
		f.completeGMPayOrder(ctx, method, typ, post)
	}
}

func (f *ltsFixture) completeGMPayOrder(ctx context.Context, method *payment.Payment, typ uint8, post func(string, string, int)) {
	t := f.t
	t.Helper()
	o := &order.Order{UserId: f.account.Id, OrderNo: order.GenerateTradeNo(), Type: typ, Quantity: 1, Status: order.StatusPending, PaymentId: method.Id, Method: "GMPay", SubscribeId: f.plan.Id, Price: 1234, Amount: 1234}
	f.create(o)
	link, err := f.Billing.PortalCheckout(ctx, &billdto.CheckoutOrderRequest{OrderNo: o.OrderNo})
	if err != nil || link.Type != "url" {
		t.Fatalf("checkout: %+v %v", link, err)
	}
	saved, err := f.Store.Order().FindOneByOrderNo(ctx, o.OrderNo)
	if err != nil || saved.TradeNo == "" {
		t.Fatal("missing cashier trade")
	}
	post("/pay/switch-network", `{"trade_id":"`+saved.TradeNo+`","network":"tron","token":"USDT"}`, 200)
	post("/_fixture/confirm/"+saved.TradeNo, "", 200)
	if typ == order.TypeSubscribe {
		f.queueRedis.SetError("ERR simulated queue outage")
		post("/_fixture/notify/"+saved.TradeNo, "", 500)
		f.queueRedis.SetError("")
		paid, err := f.Store.Order().FindOneByOrderNo(ctx, o.OrderNo)
		if err != nil || paid.Status != order.StatusPaid {
			t.Fatalf("queue failure lost committed payment: %v", err)
		}
		post("/_fixture/notify/"+saved.TradeNo, "", 200)
		post("/_fixture/notify/"+saved.TradeNo, "", 200)
	} else {
		if err := f.Billing.CloseOrder(ctx, &billdto.CloseOrderRequest{OrderNo: o.OrderNo}); err != nil {
			t.Fatalf("reconcile without callback: %v", err)
		}
	}
	saved, err = f.Store.Order().FindOneByOrderNo(ctx, o.OrderNo)
	if err != nil || saved.Status != order.StatusPaid || saved.PaymentAmount != 1234 || saved.PaymentCurrency != "CNY" {
		t.Fatalf("settlement: %+v %v", saved, err)
	}
	job, err := f.Inspector.GetTaskInfo("default", taskqueue.ActivationTaskID(o.OrderNo))
	if err != nil || job.Type != taskqueue.ForthwithActivateOrder {
		t.Fatalf("activation not queued: %v", err)
	}
	handler := orderTask.NewActivateOrderHandler(f.Billing)
	for range 2 {
		if err := handler.ProcessTask(ctx, asynq.NewTask(job.Type, job.Payload)); err != nil {
			t.Fatal(err)
		}
	}
	saved, err = f.Store.Order().FindOneByOrderNo(ctx, o.OrderNo)
	if err != nil || saved.Status != order.StatusFinished {
		t.Fatal("fulfilment did not finish")
	}
	if typ == order.TypeSubscribe {
		var n int64
		if err := f.DB.Model(&usersub.Subscribe{}).Where("order_id = ? AND status = ?", o.Id, usersub.SubscribeStatusActive).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("subscription fulfilments = %d, %v", n, err)
		}
	} else {
		var w wallet.Wallet
		if err := f.DB.Where("user_id = ?", o.UserId).First(&w).Error; err != nil || w.Balance != 1234 {
			t.Fatalf("recharge balance = %d, %v", w.Balance, err)
		}
	}
}
