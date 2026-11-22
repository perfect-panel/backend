package billingtest

import (
	"context"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
)

func TestHarnessStoresOrders(t *testing.T) {
	h := New(t)
	u := h.User()
	h.Wallet(u.Id, 100, 50)
	plan := h.Plan(1000)
	method := h.Payment("EPay", `{"pid":"1","url":"https://pay.example","key":"k","type":"alipay"}`)
	if err := h.Store.Order().Insert(context.Background(), &order.Order{
		OrderNo: "o-1", UserId: u.Id, SubscribeId: plan.Id, PaymentId: method.Id, Method: method.Platform, Status: order.StatusPending,
	}); err != nil {
		t.Fatal(err)
	}
	if got := h.ReloadOrder("o-1"); got.StateVersion != 1 || len(h.Events("o-1")) != 1 {
		t.Fatalf("order = %+v events = %v", got, h.Events("o-1"))
	}
	if w := h.ReloadWallet(u.Id); w.GiftAmount != 50 {
		t.Fatalf("wallet = %+v", w)
	}
}
