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
	"github.com/perfect-panel/server/internal/module/billing/internal/checkout"
	"github.com/perfect-panel/server/internal/module/billing/internal/coupon"
	"github.com/perfect-panel/server/internal/module/billing/internal/portal"
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

	// The checkout flows resolve the current user from the request context.
	Purchase(ctx context.Context, req *dto.PurchaseOrderRequest) (*dto.PurchaseOrderResponse, error)
	Renewal(ctx context.Context, req *dto.RenewalOrderRequest) (*dto.RenewalOrderResponse, error)
	ResetTraffic(ctx context.Context, req *dto.ResetTrafficOrderRequest) (*dto.ResetTrafficOrderResponse, error)
	Recharge(ctx context.Context, req *dto.RechargeOrderRequest) (*dto.RechargeOrderResponse, error)
	PreCreateOrder(ctx context.Context, req *dto.PurchaseOrderRequest) (*dto.PreOrderResponse, error)
	// CloseOrder settles gateway-collected money instead of closing, releases
	// coupon and gift reservations, and returns reserved plan inventory.
	CloseOrder(ctx context.Context, req *dto.CloseOrderRequest) error

	// The portal flows serve the guest storefront; checkout resolves the
	// client IP from the request context.
	PortalPurchase(ctx context.Context, req *dto.PortalPurchaseRequest) (*dto.PortalPurchaseResponse, error)
	PortalPrePurchase(ctx context.Context, req *dto.PrePurchaseOrderRequest) (*dto.PrePurchaseOrderResponse, error)
	PortalCheckout(ctx context.Context, req *dto.CheckoutOrderRequest) (*dto.CheckoutOrderResponse, error)
	QueryPurchaseOrder(ctx context.Context, req *dto.QueryPurchaseOrderRequest) (*dto.QueryPurchaseOrderResponse, error)
	GetAvailablePaymentMethods(ctx context.Context) (*dto.GetAvailablePaymentMethodsResponse, error)
	GetPortalSubscription(ctx context.Context, req *dto.GetSubscriptionRequest) (*dto.GetSubscriptionResponse, error)
	// IssuePortalSession exchanges a completed guest purchase for a normal
	// authenticated session.
	IssuePortalSession(ctx context.Context, userID int64) (string, error)
}

// Order lifecycle constants shared with the V2 orchestration layer.
const (
	CloseOrderTimeMinutes = checkout.CloseOrderTimeMinutes
	MaxQuantity           = checkout.MaxQuantity
)

// PlanReader re-exports the checkout subdomain's port onto the subscription
// domain's plan catalogue.
type PlanReader = checkout.PlanReader

// UserSubscriptionReader re-exports the checkout subdomain's port onto the
// subscription domain's user subscriptions.
type UserSubscriptionReader = checkout.UserSubscriptionReader

// Portal re-exports the guest storefront subdomain's ports and configuration
// for the composition root.
type (
	PortalPlanReader    = portal.PlanReader
	GuestAccountReader  = portal.GuestAccountReader
	SessionStore        = portal.SessionStore
	GuestCheckoutCache  = portal.GuestCheckoutCache
	ActivationTaskQueue = portal.ActivationQueue
	ExchangeRateCache   = portal.ExchangeRateCache
	PortalConfig        = portal.Config
)

// Transactor is the module's window onto billing-scoped transactions; the
// repository store satisfies it structurally.
type Transactor interface {
	InBillingTx(ctx context.Context, fn func(repository.BillingStore) error) error
}

// OrderQueue schedules the order lifecycle tasks. The composition root
// adapts the asynq client; an activation delivery that already exists for
// the order is not an error (the Paid state is the durable outbox), and a
// deferred close fires after the pending order's payment window expires.
type OrderQueue interface {
	EnqueueActivation(ctx context.Context, orderNo string) error
	EnqueueDeferredClose(ctx context.Context, orderNo string) error
}

// Deps declares everything the module needs; the composition root
// (internal/svc) provides them. The module wraps legacy repositories during
// migration and will own its persistence once the domain data moves in
// (ADR-001 step 5).
type Deps struct {
	Orders   repository.OrderRepo
	Payments repository.PaymentRepo
	Coupons  repository.CouponRepo
	Plans    PlanReader
	UserSubs UserSubscriptionReader
	// Store is the checkout subdomain's transitional full-store dependency
	// (documented inside internal/checkout).
	Store repository.Store
	Tx    Transactor
	Queue OrderQueue
	// SingleModel forbids holding more than one blocking subscription.
	SingleModel bool
	// CurrencyUnit is the site currency used for gateway verification.
	CurrencyUnit string
	// Host is the site host used to derive default payment notify URLs.
	Host string
	// IsGatewayMode reports whether notify URLs must use the gateway prefix.
	IsGatewayMode func() bool

	// Portal-specific dependencies.
	PortalPlans        PortalPlanReader
	GuestAccounts      GuestAccountReader
	Sessions           SessionStore
	GuestCheckoutCache GuestCheckoutCache
	ActivationQueue    ActivationTaskQueue
	ExchangeRate       ExchangeRateCache
	Portal             PortalConfig
}

func New(deps Deps) Service {
	return &service{
		orders:     adminorder.NewService(deps.Orders, deps.Payments, deps.Tx, deps.Queue),
		payments:   adminpayment.NewService(deps.Payments, deps.Orders, deps.Tx, deps.Host, deps.IsGatewayMode),
		coupons:    coupon.NewService(deps.Coupons),
		userOrders: userorder.NewService(deps.Orders),
		portal: portal.NewService(portal.Deps{
			Orders:             deps.Orders,
			Coupons:            deps.Coupons,
			Payments:           deps.Payments,
			UserAuths:          deps.GuestAccounts,
			Plans:              deps.PortalPlans,
			Store:              deps.Store,
			Sessions:           deps.Sessions,
			Queue:              deps.Queue,
			GuestCheckoutCache: deps.GuestCheckoutCache,
			ActivationQueue:    deps.ActivationQueue,
			ExchangeRate:       deps.ExchangeRate,
			Config:             deps.Portal,
		}),
		checkout: checkout.NewService(checkout.Deps{
			Orders:       deps.Orders,
			Coupons:      deps.Coupons,
			Payments:     deps.Payments,
			Plans:        deps.Plans,
			UserSubs:     deps.UserSubs,
			Store:        deps.Store,
			Queue:        deps.Queue,
			SingleModel:  deps.SingleModel,
			CurrencyUnit: deps.CurrencyUnit,
		}),
	}
}

type service struct {
	orders     *adminorder.Service
	payments   *adminpayment.Service
	coupons    *coupon.Service
	userOrders *userorder.Service
	checkout   *checkout.Service
	portal     *portal.Service
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

func (s *service) Purchase(ctx context.Context, req *dto.PurchaseOrderRequest) (*dto.PurchaseOrderResponse, error) {
	return s.checkout.Purchase(ctx, req)
}

func (s *service) Renewal(ctx context.Context, req *dto.RenewalOrderRequest) (*dto.RenewalOrderResponse, error) {
	return s.checkout.Renewal(ctx, req)
}

func (s *service) ResetTraffic(ctx context.Context, req *dto.ResetTrafficOrderRequest) (*dto.ResetTrafficOrderResponse, error) {
	return s.checkout.ResetTraffic(ctx, req)
}

func (s *service) Recharge(ctx context.Context, req *dto.RechargeOrderRequest) (*dto.RechargeOrderResponse, error) {
	return s.checkout.Recharge(ctx, req)
}

func (s *service) PreCreateOrder(ctx context.Context, req *dto.PurchaseOrderRequest) (*dto.PreOrderResponse, error) {
	return s.checkout.PreCreateOrder(ctx, req)
}

func (s *service) CloseOrder(ctx context.Context, req *dto.CloseOrderRequest) error {
	return s.checkout.Close(ctx, req)
}

func (s *service) PortalPurchase(ctx context.Context, req *dto.PortalPurchaseRequest) (*dto.PortalPurchaseResponse, error) {
	return s.portal.Purchase(ctx, req)
}

func (s *service) PortalPrePurchase(ctx context.Context, req *dto.PrePurchaseOrderRequest) (*dto.PrePurchaseOrderResponse, error) {
	return s.portal.PrePurchase(ctx, req)
}

func (s *service) PortalCheckout(ctx context.Context, req *dto.CheckoutOrderRequest) (*dto.CheckoutOrderResponse, error) {
	return s.portal.Checkout(ctx, req)
}

func (s *service) QueryPurchaseOrder(ctx context.Context, req *dto.QueryPurchaseOrderRequest) (*dto.QueryPurchaseOrderResponse, error) {
	return s.portal.QueryPurchaseOrder(ctx, req)
}

func (s *service) GetAvailablePaymentMethods(ctx context.Context) (*dto.GetAvailablePaymentMethodsResponse, error) {
	return s.portal.GetAvailablePaymentMethods(ctx)
}

func (s *service) GetPortalSubscription(ctx context.Context, req *dto.GetSubscriptionRequest) (*dto.GetSubscriptionResponse, error) {
	return s.portal.GetSubscription(ctx, req)
}

func (s *service) IssuePortalSession(ctx context.Context, userID int64) (string, error) {
	return s.portal.IssueSession(ctx, userID)
}
