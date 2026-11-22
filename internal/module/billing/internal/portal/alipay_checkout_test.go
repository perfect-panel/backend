package portal

import (
	"fmt"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment/alipay"
)

// The face-to-face trade expires with the order's payment window, whenever
// the checkout started: a QR code issued ten minutes into the window must
// not stay payable ten minutes after the order closed.
func TestAlipayCheckoutExpiresWithTheOrder(t *testing.T) {
	f := newPortalFixture(t)
	gw := billingtest.NewFakeAlipay(t, func(method string, _ int, biz map[string]any) billingtest.AlipayAnswer {
		if method != "alipay.trade.precreate" {
			t.Errorf("unexpected gateway call %q", method)
		}
		return billingtest.AlipayAnswer{Signed: true, Biz: fmt.Sprintf(`{"code":"10000","msg":"Success","out_trade_no":%q,"qr_code":"https://qr.alipay.com/test"}`, biz["out_trade_no"])}
	})
	method := f.h.Payment("AlipayF2F", billingtest.AlipayConfig(t, "2021000000000000", gw.URL))
	u, ctx := f.buyer(0, 0)
	created := time.Now().Add(-10 * time.Minute)
	o := f.pendingOrder("alipay-order", u.Id, 1000, method, func(o *order.Order) { o.CreatedAt = created })

	resp, err := f.checkout(ctx, o.OrderNo, "")
	if err != nil || resp.Type != "qr" || resp.CheckoutUrl != "https://qr.alipay.com/test" {
		t.Fatalf("Checkout = (%+v, %v), want the QR code", resp, err)
	}
	biz := gw.LastBiz("alipay.trade.precreate")
	if want := alipay.FormatTimeExpire(created.Add(order.PaymentWindow)); biz["time_expire"] != want {
		t.Fatalf("time_expire = %v, want %q (the order's own deadline)", biz["time_expire"], want)
	}
	if _, relative := biz["timeout_express"]; relative {
		t.Fatalf("a relative timeout was sent alongside the absolute expiry: %v", biz)
	}
}
