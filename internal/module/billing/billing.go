// Package billing is the facade of the billing module. It starts with the
// admin-side order and payment-method management; the public checkout flows
// join as migration proceeds (ADR-001 step 4). Admin and public handlers call
// the same service; access-plane concerns stay in the handlers.
package billing

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/module/billing/internal/adminorder"
	"github.com/perfect-panel/server/internal/module/billing/internal/adminpayment"
	"github.com/perfect-panel/server/internal/module/billing/internal/coupon"
	"github.com/perfect-panel/server/internal/module/billing/internal/userorder"
	"github.com/perfect-panel/server/internal/repository"
)

// Service is the only surface other code may depend on; the implementation
// lives under internal/ where the compiler seals it off.
type Service interface {
	CreateOrder(ctx context.Context, req *dto.CreateOrderRequest) error
	GetOrderList(ctx context.Context, req *dto.GetOrderListRequest) (*dto.GetOrderListResponse, error)
	// UpdateOrderStatus applies the admin's Pending->Paid/Closed transition
	// and enqueues activation for paid orders.
	UpdateOrderStatus(ctx context.Context, req *dto.UpdateOrderStatusRequest) error

	CreatePaymentMethod(ctx context.Context, req *dto.CreatePaymentMethodRequest) (*dto.PaymentConfig, error)
	UpdatePaymentMethod(ctx context.Context, req *dto.UpdatePaymentMethodRequest) (*dto.PaymentConfig, error)
	DeletePaymentMethod(ctx context.Context, req *dto.DeletePaymentMethodRequest) error
	GetPaymentMethodList(ctx context.Context, req *dto.GetPaymentMethodListRequest) (*dto.GetPaymentMethodListResponse, error)
	GetPaymentPlatform(ctx context.Context) (*dto.PlatformResponse, error)

	CreateCoupon(ctx context.Context, req *dto.CreateCouponRequest) error
	UpdateCoupon(ctx context.Context, req *dto.UpdateCouponRequest) error
	DeleteCoupon(ctx context.Context, req *dto.DeleteCouponRequest) error
	BatchDeleteCoupon(ctx context.Context, req *dto.BatchDeleteCouponRequest) error
	GetCouponList(ctx context.Context, req *dto.GetCouponListRequest) (*dto.GetCouponListResponse, error)

	// The user-facing order queries resolve the current user from the request
	// context, enforce ownership and never expose referrer commission.
	QueryOrderDetail(ctx context.Context, req *dto.QueryOrderDetailRequest) (*dto.OrderDetail, error)
	QueryOrderList(ctx context.Context, req *dto.QueryOrderListRequest) (*dto.QueryOrderListResponse, error)
}

// Transactor is the module's window onto billing-scoped transactions; the
// repository store satisfies it structurally.
type Transactor interface {
	InBillingTx(ctx context.Context, fn func(repository.BillingStore) error) error
}

// ActivationEnqueuer schedules order activation after a paid transition. The
// composition root adapts the asynq client; a delivery that already exists
// for the order is not an error (the Paid state is the durable outbox).
type ActivationEnqueuer interface {
	EnqueueActivation(ctx context.Context, orderNo string) error
}

// Deps declares everything the module needs; the composition root
// (internal/svc) provides them. The module wraps legacy repositories during
// migration and will own its persistence once the domain data moves in
// (ADR-001 step 5).
type Deps struct {
	Orders   repository.OrderRepo
	Payments repository.PaymentRepo
	Coupons  repository.CouponRepo
	Tx       Transactor
	Queue    ActivationEnqueuer
	// Host is the site host used to derive default payment notify URLs.
	Host string
	// IsGatewayMode reports whether notify URLs must use the gateway prefix.
	IsGatewayMode func() bool
}

func New(deps Deps) Service {
	return &service{
		orders:     adminorder.NewService(deps.Orders, deps.Payments, deps.Tx, deps.Queue),
		payments:   adminpayment.NewService(deps.Payments, deps.Orders, deps.Tx, deps.Host, deps.IsGatewayMode),
		coupons:    coupon.NewService(deps.Coupons),
		userOrders: userorder.NewService(deps.Orders),
	}
}

type service struct {
	orders     *adminorder.Service
	payments   *adminpayment.Service
	coupons    *coupon.Service
	userOrders *userorder.Service
}

func (s *service) CreateOrder(ctx context.Context, req *dto.CreateOrderRequest) error {
	return s.orders.Create(ctx, req)
}

func (s *service) GetOrderList(ctx context.Context, req *dto.GetOrderListRequest) (*dto.GetOrderListResponse, error) {
	return s.orders.List(ctx, req)
}

func (s *service) UpdateOrderStatus(ctx context.Context, req *dto.UpdateOrderStatusRequest) error {
	return s.orders.UpdateStatus(ctx, req)
}

func (s *service) CreatePaymentMethod(ctx context.Context, req *dto.CreatePaymentMethodRequest) (*dto.PaymentConfig, error) {
	return s.payments.Create(ctx, req)
}

func (s *service) UpdatePaymentMethod(ctx context.Context, req *dto.UpdatePaymentMethodRequest) (*dto.PaymentConfig, error) {
	return s.payments.Update(ctx, req)
}

func (s *service) DeletePaymentMethod(ctx context.Context, req *dto.DeletePaymentMethodRequest) error {
	return s.payments.Delete(ctx, req)
}

func (s *service) GetPaymentMethodList(ctx context.Context, req *dto.GetPaymentMethodListRequest) (*dto.GetPaymentMethodListResponse, error) {
	return s.payments.List(ctx, req)
}

func (s *service) GetPaymentPlatform(ctx context.Context) (*dto.PlatformResponse, error) {
	return s.payments.Platforms(ctx)
}

func (s *service) CreateCoupon(ctx context.Context, req *dto.CreateCouponRequest) error {
	return s.coupons.Create(ctx, req)
}

func (s *service) UpdateCoupon(ctx context.Context, req *dto.UpdateCouponRequest) error {
	return s.coupons.Update(ctx, req)
}

func (s *service) DeleteCoupon(ctx context.Context, req *dto.DeleteCouponRequest) error {
	return s.coupons.Delete(ctx, req)
}

func (s *service) BatchDeleteCoupon(ctx context.Context, req *dto.BatchDeleteCouponRequest) error {
	return s.coupons.BatchDelete(ctx, req)
}

func (s *service) GetCouponList(ctx context.Context, req *dto.GetCouponListRequest) (*dto.GetCouponListResponse, error) {
	return s.coupons.List(ctx, req)
}

func (s *service) QueryOrderDetail(ctx context.Context, req *dto.QueryOrderDetailRequest) (*dto.OrderDetail, error) {
	return s.userOrders.QueryDetail(ctx, req)
}

func (s *service) QueryOrderList(ctx context.Context, req *dto.QueryOrderListRequest) (*dto.QueryOrderListResponse, error) {
	return s.userOrders.QueryList(ctx, req)
}
