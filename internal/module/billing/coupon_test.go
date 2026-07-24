package billing_test

import (
	"context"
	"testing"

	"github.com/perfect-panel/server/internal/model/dto"
	couponEntity "github.com/perfect-panel/server/internal/model/entity/coupon"
	orderEntity "github.com/perfect-panel/server/internal/model/entity/order"
	userEntity "github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/constant"
	"gorm.io/gorm"
)

type fakeCouponRepo struct {
	repository.CouponRepo
	coupon   *couponEntity.Coupon
	inserted *couponEntity.Coupon
	updated  *couponEntity.Coupon
}

func (f *fakeCouponRepo) Insert(_ context.Context, data *couponEntity.Coupon) error {
	f.inserted = data
	return nil
}

func (f *fakeCouponRepo) FindOne(_ context.Context, id int64) (*couponEntity.Coupon, error) {
	if f.coupon == nil || f.coupon.Id != id {
		return nil, gorm.ErrRecordNotFound
	}
	copy := *f.coupon
	return &copy, nil
}

func (f *fakeCouponRepo) Update(_ context.Context, data *couponEntity.Coupon) error {
	f.updated = data
	return nil
}

func validCoupon() *dto.CreateCouponRequest {
	return &dto.CreateCouponRequest{
		Name: "c", Type: 1, Discount: 10, StartTime: 1000, ExpireTime: 2000,
	}
}

func TestCreateCouponValidatesInput(t *testing.T) {
	repo := &fakeCouponRepo{}
	svc := newBillingServiceWithCoupons(repo)

	cases := []func(*dto.CreateCouponRequest){
		func(r *dto.CreateCouponRequest) { r.ExpireTime = r.StartTime },   // empty validity window
		func(r *dto.CreateCouponRequest) { r.Type = 9 },                   // unknown type
		func(r *dto.CreateCouponRequest) { r.Discount = 101 },             // percentage over 100
		func(r *dto.CreateCouponRequest) { r.Count = 1; r.UsedCount = 2 }, // used beyond count
	}
	for i, mutate := range cases {
		req := validCoupon()
		mutate(req)
		if err := svc.CreateCoupon(context.Background(), req); err == nil {
			t.Fatalf("case %d: invalid coupon must be rejected: %+v", i, req)
		}
	}
	if repo.inserted != nil {
		t.Fatal("no coupon may be inserted for invalid input")
	}

	if err := svc.CreateCoupon(context.Background(), validCoupon()); err != nil {
		t.Fatalf("valid coupon rejected: %v", err)
	}
	if repo.inserted == nil || repo.inserted.Code == "" {
		t.Fatalf("coupon must be inserted with a generated code: %+v", repo.inserted)
	}
}

func TestUpdateCouponForbidsReducingUsedCount(t *testing.T) {
	repo := &fakeCouponRepo{coupon: &couponEntity.Coupon{Id: 3, UsedCount: 5}}
	svc := newBillingServiceWithCoupons(repo)

	req := &dto.UpdateCouponRequest{
		Id: 3, Name: "c", Type: 1, Discount: 10, StartTime: 1000, ExpireTime: 2000, UsedCount: 4,
	}
	if err := svc.UpdateCoupon(context.Background(), req); err == nil {
		t.Fatal("reducing used count must be rejected")
	}
	if repo.updated != nil {
		t.Fatal("coupon must not be updated")
	}
}

func TestQueryOrderDetailEnforcesOwnershipAndHidesCommission(t *testing.T) {
	orders := &fakeOrderRepo{details: &orderEntity.Details{
		Id: 1, OrderNo: "o-9", UserId: 7, Commission: 500,
	}}
	svc, _ := newBillingService(orders, &fakePaymentRepo{})

	stranger := context.WithValue(context.Background(), constant.CtxKeyUser, &userEntity.User{Id: 8})
	if _, err := svc.QueryOrderDetail(stranger, &dto.QueryOrderDetailRequest{OrderNo: "o-9"}); err == nil {
		t.Fatal("reading someone else's order must be rejected")
	}

	owner := context.WithValue(context.Background(), constant.CtxKeyUser, &userEntity.User{Id: 7})
	got, err := svc.QueryOrderDetail(owner, &dto.QueryOrderDetailRequest{OrderNo: "o-9"})
	if err != nil {
		t.Fatalf("QueryOrderDetail: %v", err)
	}
	if got.Commission != 0 {
		t.Fatalf("commission must never leak to users: %d", got.Commission)
	}
}
