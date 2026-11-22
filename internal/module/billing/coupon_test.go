package billing_test

import (
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/coupon"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

func validCoupon() *dto.CreateCouponRequest {
	return &dto.CreateCouponRequest{Name: "c", Type: coupon.TypePercentage, Discount: 10, StartTime: 1000, ExpireTime: 2000}
}

func TestCreateCouponValidatesInput(t *testing.T) {
	f := newFacade(t)
	for name, tt := range map[string]struct {
		mutate func(*dto.CreateCouponRequest)
		code   uint32
	}{
		"empty validity window":   {func(r *dto.CreateCouponRequest) { r.ExpireTime = r.StartTime }, xerr.InvalidCoupon},
		"used beyond count":       {func(r *dto.CreateCouponRequest) { r.Count, r.UsedCount = 1, 2 }, xerr.InvalidCoupon},
		"unknown type":            {func(r *dto.CreateCouponRequest) { r.Type = 9 }, xerr.InvalidCouponType},
		"percentage over 100":     {func(r *dto.CreateCouponRequest) { r.Discount = 101 }, xerr.InvalidCouponDiscount},
		"fixed without discount":  {func(r *dto.CreateCouponRequest) { r.Type, r.Discount = coupon.TypeFixed, 0 }, xerr.InvalidCouponDiscount},
		"negative per-user limit": {func(r *dto.CreateCouponRequest) { r.UserLimit = -1 }, xerr.InvalidCoupon},
	} {
		t.Run(name, func(t *testing.T) {
			req := validCoupon()
			tt.mutate(req)
			assertCode(t, f.svc.CreateCoupon(adminContext, req), tt.code)
		})
	}
	var count int64
	if err := f.h.DB.Model(&coupon.Coupon{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("coupons = %d, want none stored for invalid input", count)
	}
	if err := f.svc.CreateCoupon(adminContext, validCoupon()); err != nil {
		t.Fatalf("valid coupon rejected: %v", err)
	}
	var created coupon.Coupon
	if err := f.h.DB.First(&created).Error; err != nil || created.Code == "" {
		t.Fatalf("coupon = %+v (%v), want one with a generated code", created, err)
	}
}

func TestUpdateCouponForbidsReducingUsedCount(t *testing.T) {
	f := newFacade(t)
	existing := f.h.Coupon("USED", func(c *coupon.Coupon) { c.Count, c.UsedCount = 10, 5 })
	req := &dto.UpdateCouponRequest{
		Id: existing.Id, Name: "c", Code: "USED", Count: 10, Type: coupon.TypePercentage, Discount: 10,
		StartTime: 1000, ExpireTime: 2000, UsedCount: 4,
	}
	assertCode(t, f.svc.UpdateCoupon(adminContext, req), xerr.CouponUsedCountImmutable)
	if f.h.ReloadCoupon("USED").UsedCount != 5 {
		t.Fatal("the used count was reduced")
	}
}

func TestQueryOrderDetailEnforcesOwnershipAndHidesCommission(t *testing.T) {
	f := newFacade(t)
	owner, stranger := f.h.User(), f.h.User()
	f.h.Order(&order.Order{OrderNo: "o-9", UserId: owner.Id, Status: order.StatusFinished, Commission: 500})

	_, err := f.svc.QueryOrderDetail(billingtest.UserContext(stranger), &dto.QueryOrderDetailRequest{OrderNo: "o-9"})
	assertCode(t, err, xerr.InvalidAccess)
	_, err = f.svc.QueryOrderDetail(billingtest.UserContext(owner), &dto.QueryOrderDetailRequest{OrderNo: "missing"})
	assertCode(t, err, xerr.OrderNotExist)
	got, err := f.svc.QueryOrderDetail(billingtest.UserContext(owner), &dto.QueryOrderDetailRequest{OrderNo: "o-9"})
	if err != nil {
		t.Fatalf("QueryOrderDetail: %v", err)
	}
	if got.OrderNo != "o-9" || got.Commission != 0 {
		t.Fatalf("detail = %+v, want the order without its commission", got)
	}
}

// A coupon whose plan list column is damaged is listed without plans, so the
// administrator can find and repair it, instead of failing the whole page;
// checkout keeps refusing it meanwhile.
func TestGetCouponListSurvivesADamagedPlanList(t *testing.T) {
	f := newFacade(t)
	f.h.Coupon("HEALTHY", func(c *coupon.Coupon) { c.Subscribe = "3" })
	damaged := f.h.Coupon("DAMAGED", func(c *coupon.Coupon) { c.Subscribe = "1,2," })

	resp, err := f.svc.GetCouponList(adminContext, &dto.GetCouponListRequest{Page: 1, Size: 10})
	if err != nil {
		t.Fatalf("GetCouponList: %v", err)
	}
	if resp.Total != 2 || len(resp.List) != 2 {
		t.Fatalf("coupon list = %+v, want both coupons", resp)
	}
	byCode := map[string]dto.Coupon{}
	for _, c := range resp.List {
		byCode[c.Code] = c
	}
	if got := byCode["HEALTHY"].Subscribe; len(got) != 1 || got[0] != 3 {
		t.Fatalf("healthy coupon plans = %v, want [3]", got)
	}
	if got := byCode["DAMAGED"]; got.Id != damaged.Id || len(got.Subscribe) != 0 {
		t.Fatalf("damaged coupon = %+v, want it listed without plans", got)
	}

	buyer := f.h.User()
	plan := f.h.Plan(1000)
	method := f.h.Payment("EPay", epayConfig)
	_, err = f.svc.Purchase(billingtest.UserContext(buyer), &dto.PurchaseOrderRequest{SubscribeId: plan.Id, Quantity: 1, Payment: method.Id, Coupon: "DAMAGED"})
	assertCode(t, err, xerr.CouponNotApplicable)
}
