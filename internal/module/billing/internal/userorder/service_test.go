package userorder_test

import (
	"context"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/billing/internal/userorder"
	"github.com/perfect-panel/backend/pkg/xerr"
)

func assertCode(t *testing.T, err error, want uint32) {
	t.Helper()
	if got := xerr.CodeOf(err); err == nil || got != want {
		t.Fatalf("error = %v (code %d), want code %d", err, got, want)
	}
}

func TestQueryDetailShowsOnlyTheBuyersOrderWithoutCommission(t *testing.T) {
	h := billingtest.New(t)
	svc := userorder.NewService(h.Store.Order(), h.Store.Subscribe())
	owner, stranger := h.User(), h.User()
	plan := h.Plan(1000)
	h.Order(&order.Order{OrderNo: "o-1", UserId: owner.Id, SubscribeId: plan.Id, Status: order.StatusFinished, Amount: 1000, Commission: 200})

	_, err := svc.QueryDetail(context.Background(), &dto.QueryOrderDetailRequest{OrderNo: "o-1"})
	assertCode(t, err, xerr.InvalidAccess)
	_, err = svc.QueryDetail(billingtest.UserContext(stranger), &dto.QueryOrderDetailRequest{OrderNo: "o-1"})
	assertCode(t, err, xerr.InvalidAccess)
	_, err = svc.QueryDetail(billingtest.UserContext(owner), &dto.QueryOrderDetailRequest{OrderNo: "missing"})
	assertCode(t, err, xerr.OrderNotExist)

	detail, err := svc.QueryDetail(billingtest.UserContext(owner), &dto.QueryOrderDetailRequest{OrderNo: "o-1"})
	if err != nil {
		t.Fatalf("QueryDetail: %v", err)
	}
	if detail.Amount != 1000 || detail.Commission != 0 || detail.Subscribe.Name != plan.Name {
		t.Fatalf("detail = %+v, want the order with its plan and without commission", detail)
	}
}

func TestQueryListListsOnlyTheBuyersOrders(t *testing.T) {
	h := billingtest.New(t)
	svc := userorder.NewService(h.Store.Order(), h.Store.Subscribe())
	owner, stranger := h.User(), h.User()
	plan := h.Plan(1000)
	for _, o := range []*order.Order{
		{OrderNo: "mine-1", UserId: owner.Id, SubscribeId: plan.Id, Status: order.StatusFinished, Commission: 100},
		{OrderNo: "mine-2", UserId: owner.Id, Status: order.StatusPending},
		{OrderNo: "theirs", UserId: stranger.Id, Status: order.StatusFinished},
	} {
		h.Order(o)
	}
	_, err := svc.QueryList(context.Background(), &dto.QueryOrderListRequest{Page: 1, Size: 10})
	assertCode(t, err, xerr.InvalidAccess)

	resp, err := svc.QueryList(billingtest.UserContext(owner), &dto.QueryOrderListRequest{Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("QueryList: %v", err)
	}
	if resp.Total != 2 || len(resp.List) != 2 {
		t.Fatalf("list = %+v, want the buyer's two orders", resp)
	}
	for _, item := range resp.List {
		if item.OrderNo == "theirs" || item.Commission != 0 {
			t.Fatalf("list item = %+v", item)
		}
		if item.OrderNo == "mine-1" && item.Subscribe.Name != plan.Name {
			t.Fatalf("list item %s lacks its plan", item.OrderNo)
		}
	}
}
