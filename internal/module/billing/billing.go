// Package billing is the facade of the billing module: orders and their
// checkout, payment methods and gateway callbacks, coupons, the V2 order
// orchestration with its event stream, the wallet and the paid-order
// activation workflow (ADR-001). Admin and public handlers call the same
// service; access-plane concerns stay in the handlers.
package billing

import (
	"context"
	"time"

	"github.com/perfect-panel/backend/internal/auth/ratelimit"
	"github.com/perfect-panel/backend/internal/infra/rediskeys"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	paymentEntity "github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	walletEntity "github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"github.com/perfect-panel/backend/internal/module/billing/internal/activation"
	"github.com/perfect-panel/backend/internal/module/billing/internal/adminorder"
	"github.com/perfect-panel/backend/internal/module/billing/internal/adminpayment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/callbacks"
	"github.com/perfect-panel/backend/internal/module/billing/internal/checkout"
	"github.com/perfect-panel/backend/internal/module/billing/internal/coupon"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/orderevents"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/portal"
	"github.com/perfect-panel/backend/internal/module/billing/internal/repo"
	"github.com/perfect-panel/backend/internal/module/billing/internal/userorder"
	v2orch "github.com/perfect-panel/backend/internal/module/billing/internal/v2"
	"github.com/perfect-panel/backend/internal/module/billing/internal/wallet"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
)

// Service is the only surface other code may depend on; the implementation
// lives under internal/ where the compiler seals it off.
type Service interface {
	// OrderStatistics serves the other modules' reads of order figures.
	OrderStatistics

	CreateOrder(ctx context.Context, req *dto.CreateOrderRequest) error
	GetOrderList(ctx context.Context, req *dto.GetOrderListRequest) (*dto.GetOrderListResponse, error)
	// UpdateOrderStatus applies the admin's Pending->Paid transition and
	// enqueues activation, or closes the order through the same close flow
	// as owner and expiry closes.
	UpdateOrderStatus(ctx context.Context, req *dto.UpdateOrderStatusRequest) error

	CreatePaymentMethod(ctx context.Context, req *dto.CreatePaymentMethodRequest) (*dto.PaymentConfig, error)
	UpdatePaymentMethod(ctx context.Context, req *dto.UpdatePaymentMethodRequest) (*dto.PaymentConfig, error)
	DeletePaymentMethod(ctx context.Context, req *dto.DeletePaymentMethodRequest) error
	GetPaymentMethodList(ctx context.Context, req *dto.GetPaymentMethodListRequest) (*dto.GetPaymentMethodListResponse, error)
	GetPaymentPlatform(ctx context.Context) (*dto.PaymentPlatformResponse, error)

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

	// FindPaymentMethodByToken reads the payment method whose notify URL
	// carries token, with the repository's error unwrapped: the notify
	// middleware resolves a callback's method with it and answers a failed
	// lookup with that error.
	FindPaymentMethodByToken(ctx context.Context, token string) (*paymentEntity.Payment, error)
	// PaymentNotify authenticates and settles a gateway callback. It reads
	// the payment method the notify middleware resolved from the request
	// context; every gateway goes through the same verification.
	PaymentNotify(ctx context.Context, notification PaymentNotification) error
	// PaymentCallbackStyle reports how a platform delivers its callbacks and
	// expects them answered; false for a platform without a gateway.
	PaymentCallbackStyle(platform string) (PaymentCallbackStyle, bool)

	// The V2 orchestration: idempotent create-and-checkout, guest checkout
	// capabilities and SSE event-stream tickets.
	V2CreateAndCheckout(ctx context.Context, req *dto.V2CreateOrderRequest, idempotencyKey string) (*dto.V2OrderResponse, error)
	V2Checkout(ctx context.Context, orderNo string, req *dto.V2CheckoutOrderRequest) (*dto.V2OrderResponse, error)
	V2GetOrder(ctx context.Context, orderNo, checkoutToken string) (*dto.V2OrderResponse, error)
	V2EventTicket(ctx context.Context, orderNo, checkoutToken string) (*dto.V2EventTicketResponse, error)
	V2Session(ctx context.Context, orderNo, checkoutToken string) (*dto.V2OrderSessionResponse, error)
	// V2StreamOrderEvents serves an order's event stream to sink until ctx
	// ends or the ticket expires: the snapshot, the events after the replay
	// cursor, then live events. It returns an error only when the stream is
	// refused, before anything reached the sink (ErrTooManyEventStreams
	// when the ticket holds its maximum of concurrent streams).
	V2StreamOrderEvents(ctx context.Context, req V2EventStreamRequest, sink V2EventSink) error

	// PublishOrderEvents drains the order-event outbox the streams replay:
	// it broadcasts the oldest unpublished events, 500 at most, on their
	// orders' Redis channels to wake the streams, marking each published.
	// The first failure ends the run with its error, and the next run
	// publishes what this one did not. CleanupOrderEvents deletes the
	// published events created before cutoff and reports how many; an
	// unpublished event is kept whatever its age.
	PublishOrderEvents(ctx context.Context) error
	CleanupOrderEvents(ctx context.Context, cutoff time.Time) (int64, error)

	// The wallet flows resolve the current user from the request context:
	// commission withdrawal, balance/commission statements and the affiliate
	// earnings overview.
	CommissionWithdraw(ctx context.Context, req *dto.CommissionWithdrawRequest) (*dto.WithdrawalLog, error)
	QueryUserBalanceLog(ctx context.Context) (*dto.QueryUserBalanceLogListResponse, error)
	QueryUserCommissionLog(ctx context.Context, req *dto.QueryUserCommissionLogListRequest) (*dto.QueryUserCommissionLogListResponse, error)
	QueryWithdrawalLog(ctx context.Context, req *dto.QueryWithdrawalLogListRequest) (*dto.QueryWithdrawalLogListResponse, error)
	GetWithdrawalList(ctx context.Context, req *dto.GetWithdrawalListRequest) (*dto.GetWithdrawalListResponse, error)
	ReviewWithdrawal(ctx context.Context, req *dto.ReviewWithdrawalRequest) error
	QueryUserAffiliate(ctx context.Context) (*dto.QueryUserAffiliateCountResponse, error)
	QueryUserAffiliateList(ctx context.Context, req *dto.QueryUserAffiliateListRequest) (*dto.QueryUserAffiliateListResponse, error)

	// The wallet as other modules reach it (ADR-001 rules 2 and 4): they read
	// the billing-owned wallet table and move money only through these
	// methods, each movement in a billing transaction of its own that runs
	// after the requesting module committed its part.
	//
	// FindWallet reads a user's wallet; a user without a wallet row reads as
	// nil. FindWallets reads several; users without a row are absent from
	// the map.
	FindWallet(ctx context.Context, userID int64) (*walletEntity.Wallet, error)
	FindWallets(ctx context.Context, userIDs []int64) (map[int64]*walletEntity.Wallet, error)
	// OpenWallet sets the opening balance, gift amount and commission of an
	// account an administrator created. AdjustWallet applies an
	// administrator's wallet edit: under the wallet lock each amount the
	// adjustment sets becomes the wallet's, with an audit log recording the
	// change and the resulting amount; amounts left nil or already equal are
	// left alone.
	OpenWallet(ctx context.Context, opening walletEntity.Wallet) error
	AdjustWallet(ctx context.Context, adjustment walletEntity.Adjustment) error
	// UnsubscribeRefundSettled reports whether the refund of a cancelled user
	// subscription was settled. SettleUnsubscribeRefund settles it: amount,
	// capped at what the order and its paid renewals cost, goes back to the
	// buyer's wallet (gift amount first for a balance-paid order), the
	// referrer loses the commission share the refund takes back, and the
	// refund marker is recorded, all in one billing transaction. A
	// subscription without an order (orderID 0) only gets the marker; a
	// second settlement fails on it.
	UnsubscribeRefundSettled(ctx context.Context, subscriptionID int64) (bool, error)
	SettleUnsubscribeRefund(ctx context.Context, userID, subscriptionID, orderID, amount int64) error
	// QuotaGiftCredited reports whether a quota task's gift for a
	// subscription was credited. CreditQuotaGift credits amount to the gift
	// balance of the subscription's owner with its gift log dated at, exactly
	// once per (task, subscription); a zero amount only records the marker.
	QuotaGiftCredited(ctx context.Context, taskID, subscriptionID int64) (bool, error)
	CreditQuotaGift(ctx context.Context, taskID, subscriptionID, userID, amount int64, at time.Time) error

	// The order reads of the subscription module's fulfillment and refund
	// quote: an order by id or number, and an order with its renewals.
	FindOrder(ctx context.Context, id int64) (*order.Order, error)
	FindOrderByNo(ctx context.Context, orderNo string) (*order.Order, error)
	FindOrderDetails(ctx context.Context, id int64) (*order.Details, error)
	// OrdersByStatusAfter pages the orders in status by ascending id: at most
	// limit (1000 when limit is outside 1..1000) of those after afterID. The
	// pending and paid reconcilers walk their orders with it.
	OrdersByStatusAfter(ctx context.Context, status uint8, afterID int64, limit int) ([]*order.Order, error)

	// The billing-owned paid-order workflow and its idempotent stages:
	// recharge credit, referral commission and final settlement.
	ActivatePaidOrder(ctx context.Context, orderNo string) error
	ActivateRecharge(ctx context.Context, orderNo string) (balance int64, err error)
	SettleOrderCommission(ctx context.Context, orderNo string, buyerID int64) error
	FinalizeOrder(ctx context.Context, orderNo string) error

	// DailyOrderReport totals one day's settled orders for the operations
	// report; plan names are resolved so the caller only formats.
	DailyOrderReport(ctx context.Context, date time.Time) (*DailyOrderReport, error)
}

// DailyOrderReport and its lines re-export the admin order subdomain's daily
// summary for the queue shell that formats and delivers it.
type (
	DailyOrderReport     = adminorder.DailyReport
	DailyOrderReportLine = adminorder.DailyReportLine
)

// ErrIdempotencyKeyReused is handled as HTTP 409 by the V2 handler. It is a
// distinct transport condition: the original order remains intact.
var ErrIdempotencyKeyReused = v2orch.ErrIdempotencyKeyReused

// ErrTooManyEventStreams refuses an order event stream whose ticket already
// holds its maximum of concurrent streams.
var ErrTooManyEventStreams = v2orch.ErrTooManyStreams

// PaymentNotification is a gateway callback as the notify endpoint received
// it, and PaymentCallbackStyle how a gateway delivers and answers callbacks.
type (
	PaymentNotification  = gateway.Notification
	PaymentCallbackStyle = gateway.CallbackStyle
)

// V2EventStreamRequest names an order event stream and V2EventSink receives
// its events.
type (
	V2EventStreamRequest = v2orch.StreamRequest
	V2EventSink          = v2orch.EventSink
)

// ErrGatewayUnconfirmed marks a refused close whose order stays pending until
// the gateway confirms payment; schedulers treat it as an expected outcome.
var ErrGatewayUnconfirmed = checkout.ErrGatewayUnconfirmed

// ErrInvalidPaymentCallback marks a rejected gateway callback that cannot be
// authenticated or does not describe its order, which redelivery cannot
// change; the notify handler answers a gateway that reads HTTP statuses with
// 400 for it and 500 for any other failure.
var ErrInvalidPaymentCallback = gateway.ErrInvalidCallback

// CloseOrderTimeMinutes is the payment window of a pending order; the
// composition root schedules the deferred close after it.
const CloseOrderTimeMinutes = checkout.CloseOrderTimeMinutes

// FormatAmount renders an amount in minor units with two decimal places
// (1990 → "19.90"). It is the one money formatter of the module, shared with
// the gateways, so reports and notifications show what was charged.
func FormatAmount(minor int64) string { return payment.FormatAmount(minor) }

// PlanReader re-exports the checkout subdomain's port onto the subscription
// domain's plan catalogue.
type PlanReader = checkout.PlanReader

// UserSubscriptionReader re-exports the checkout subdomain's port onto the
// subscription domain's user subscriptions.
type UserSubscriptionReader = checkout.UserSubscriptionReader

// Portal re-exports the guest storefront subdomain's ports and configuration
// for the composition root.
type (
	PortalPlanReader   = portal.PlanReader
	GuestAccountReader = portal.GuestAccountReader
	SessionStore       = portal.SessionStore
	GuestCheckoutCache = portal.GuestCheckoutCache
	ExchangeRateCache  = portal.ExchangeRateCache
	PortalConfig       = portal.Config
	GuestVerification  = portal.GuestVerification
	// RegistrationPolicy is the registration settings a guest purchase
	// follows; the composition root snapshots them per request from the
	// same configuration identity's registration reads.
	RegistrationPolicy = portal.RegistrationPolicy
	// EmailAliasReader is the identity port extension that lets guest
	// purchase refuse other spellings of an existing mailbox.
	EmailAliasReader = portal.EmailAliasReader
)

// AffiliateReader and AuthMethodReader re-export the wallet subdomain's
// read-only ports onto the identity domain (referral tree, masked login
// identifiers); the legacy user repository satisfies both structurally.
type (
	AffiliateReader  = wallet.AffiliateReader
	AuthMethodReader = wallet.AuthMethodReader
)

// UserCache drops the identity module's cached projection of users after a
// wallet movement changed what it shows; the identity facade provides it.
type UserCache interface {
	ClearUserCache(ctx context.Context, userIDs ...int64) error
}

// Transactor is the module's window onto billing-scoped transactions; the
// repository store satisfies it structurally.
type Transactor interface {
	InBillingTx(ctx context.Context, fn func(repository.BillingStore) error) error
}

// PaidOrderDependencies supplies the external capabilities of order activation.
// The billing module binds its own order and profile readers when constructed.
type PaidOrderDependencies = activation.WorkflowDeps

// OrderQueue schedules the order lifecycle tasks. The composition root
// adapts the asynq client; an activation delivery that already exists for
// the order is not an error (the Paid state is the durable outbox), and a
// deferred close fires after the pending order's payment window expires.
type OrderQueue interface {
	EnqueueActivation(ctx context.Context, orderNo string) error
	EnqueueDeferredClose(ctx context.Context, orderNo string) error
}

// OrderEventStore is the durable order-event table: the event streams replay
// it, the outbox publisher drains it and the retention cleanup prunes it.
type OrderEventStore interface {
	v2orch.EventReader
	orderevents.Outbox
}

// Deps declares everything the module needs; the composition root
// (internal/app) provides them; each field is scoped to the use cases it serves.
type Deps struct {
	PaidOrders  PaidOrderDependencies
	Orders      repository.OrderRepo
	OrderEvents OrderEventStore
	Payments    repository.PaymentRepo
	Coupons     repository.CouponRepo
	Withdrawals repository.UserWithdrawalRepo
	Plans       PlanReader
	UserSubs    UserSubscriptionReader
	// Store carries the billing-scoped transactions of the order and wallet
	// flows, the wallet view and the inbox markers. Subscription writes are
	// exposed only by the Inventory capability.
	Store     Store
	Inventory checkout.Inventory
	Tx        Transactor
	Queue     OrderQueue
	// Redis carries the outbox's wake-ups to the order event streams and
	// bounds the streams' concurrency.
	Redis       *redis.Client
	RedisPrefix rediskeys.Prefix
	// SingleModel forbids holding more than one blocking subscription;
	// runtime-mutable, read per request.
	SingleModel func() bool
	// CurrencyUnit is the site currency used for gateway verification;
	// runtime-mutable, read per request.
	CurrencyUnit func() string

	// InvitePolicy snapshots the runtime-mutable site-wide referral
	// fallback for the commission stage; UserProfiles resolves referral
	// settings, and the referrer a refund charges back, from the identity
	// domain.
	InvitePolicy func() (percentage uint8, onlyFirstPurchase bool)
	UserProfiles activation.ProfileReader

	// Wallet-specific dependencies: audit-log statements and the
	// identity-domain read ports. UserCache invalidates the identity
	// module's user cache after a balance checkout.
	Logs        repository.LogRepo
	UserCache   UserCache
	Affiliates  AffiliateReader
	AuthMethods AuthMethodReader

	// Portal-specific dependencies.
	PortalPlans        PortalPlanReader
	GuestAccounts      GuestAccountReader
	Sessions           SessionStore
	GuestCheckoutCache GuestCheckoutCache
	ExchangeRate       ExchangeRateCache
	Portal             PortalConfig
}

// NewRepoBuilder exports the module-owned repository implementations for
// store assembly (ADR-001 step-6 preparation).
func NewRepoBuilder() repository.BillingBuilder {
	return repo.NewBuilder()
}

func New(deps Deps) Service {
	gateways := gateway.NewRegistry()
	checkoutSvc := checkout.NewService(checkout.Deps{
		Orders:       deps.Orders,
		Coupons:      deps.Coupons,
		Payments:     deps.Payments,
		Plans:        deps.Plans,
		UserSubs:     deps.UserSubs,
		Wallets:      storeWallets{store: deps.Store},
		Tx:           deps.Store,
		Inventory:    deps.Inventory,
		Queue:        deps.Queue,
		Gateways:     gateways,
		SingleModel:  deps.SingleModel,
		CurrencyUnit: deps.CurrencyUnit,
	})
	portalSvc := portal.NewService(portal.Deps{
		Orders:             deps.Orders,
		OrderEvents:        portalOrderEvents(deps.OrderEvents),
		Coupons:            deps.Coupons,
		Payments:           deps.Payments,
		UserAuths:          deps.GuestAccounts,
		Plans:              deps.PortalPlans,
		Tx:                 deps.Store,
		UserCache:          deps.UserCache,
		Inventory:          deps.Inventory,
		Sessions:           deps.Sessions,
		Queue:              deps.Queue,
		GuestCheckoutCache: deps.GuestCheckoutCache,
		ExchangeRate:       deps.ExchangeRate,
		Gateways:           gateways,
		Config:             deps.Portal,
	})
	activationSvc := activation.NewService(activation.Deps{
		Orders: deps.Orders, Store: deps.Store, Profiles: deps.UserProfiles, InvitePolicy: deps.InvitePolicy,
	})
	workflowDeps := deps.PaidOrders
	workflowDeps.Orders = deps.Orders
	workflowDeps.Profiles = deps.UserProfiles
	if workflowDeps.GuestIdentities == nil {
		// The guest account stage checks the mailbox against the same
		// identity port the guest purchase checks it against.
		workflowDeps.GuestIdentities = deps.GuestAccounts
	}
	return &service{
		statistics: statistics{orders: deps.Orders},
		orders: adminorder.NewService(adminorder.Deps{
			Orders: deps.Orders, Payments: deps.Payments, Coupons: deps.Coupons, UserSubs: deps.UserSubs, Inventory: deps.Inventory,
			Tx: deps.Tx, Queue: deps.Queue, Plans: deps.Plans, Closer: checkoutSvc,
		}),
		payments: adminpayment.NewService(adminpayment.Deps{
			Payments: deps.Payments, Orders: deps.Orders, Gateways: gateways,
			SiteHost: deps.Portal.SiteHost,
		}),
		coupons:    coupon.NewService(deps.Coupons),
		userOrders: userorder.NewService(deps.Orders, deps.Plans),
		callbacks:  callbacks.NewService(deps.Orders, deps.Queue, gateways, callbacks.WithUnmatchedPaymentLog(unmatchedPaymentLog(deps.Logs))),
		gateways:   gateways,
		portal:     portalSvc,
		checkout:   checkoutSvc,
		activation: activationSvc,
		paidOrders: activation.NewWorkflow(workflowDeps, activationSvc),
		wallet: wallet.NewService(wallet.Deps{
			Logs:        deps.Logs,
			Withdrawals: deps.Withdrawals,
			Affiliates:  deps.Affiliates,
			AuthMethods: deps.AuthMethods,
			Tx:          deps.Tx,
			Store:       deps.Store,
			Profiles:    deps.UserProfiles,
		}),
		orderRows:  deps.Orders,
		methodRows: deps.Payments,
		outbox:     orderevents.NewService(deps.OrderEvents, orderevents.RedisBroadcaster{Client: deps.Redis}),
		v2: v2orch.NewService(v2orch.Deps{
			Orders:       deps.Orders,
			Checkout:     checkoutSvc,
			Portal:       portalSvc,
			JwtSecret:    deps.Portal.JwtSecret,
			CurrencyUnit: deps.CurrencyUnit,
			GuestReplays: guestReplayLimits(deps.Redis),
			Stream: v2orch.StreamDeps{
				Events:  deps.OrderEvents,
				Broker:  v2orch.RedisBroker{Client: deps.Redis, Prefix: deps.RedisPrefix},
				Limiter: v2orch.RedisLimiter{Client: deps.Redis},
			},
		}),
	}
}

type service struct {
	statistics
	paidOrders *activation.Workflow
	orders     *adminorder.Service
	payments   *adminpayment.Service
	coupons    *coupon.Service
	userOrders *userorder.Service
	checkout   *checkout.Service
	portal     *portal.Service
	callbacks  *callbacks.Service
	gateways   *gateway.Registry
	v2         *v2orch.Service
	wallet     *wallet.Service
	activation *activation.Service
	outbox     *orderevents.Service
	orderRows  repository.OrderRepo
	methodRows repository.PaymentRepo
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

func (s *service) GetPaymentPlatform(ctx context.Context) (*dto.PaymentPlatformResponse, error) {
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

func (s *service) FindPaymentMethodByToken(ctx context.Context, token string) (*paymentEntity.Payment, error) {
	return s.methodRows.FindOneByPaymentToken(ctx, token)
}

func (s *service) PaymentNotify(ctx context.Context, notification PaymentNotification) error {
	return s.callbacks.Notify(ctx, notification)
}

func (s *service) PaymentCallbackStyle(platform string) (PaymentCallbackStyle, bool) {
	return s.gateways.CallbackStyle(platform)
}

func (s *service) V2CreateAndCheckout(ctx context.Context, req *dto.V2CreateOrderRequest, idempotencyKey string) (*dto.V2OrderResponse, error) {
	return s.v2.CreateAndCheckout(ctx, req, idempotencyKey)
}

func (s *service) V2Checkout(ctx context.Context, orderNo string, req *dto.V2CheckoutOrderRequest) (*dto.V2OrderResponse, error) {
	return s.v2.Checkout(ctx, orderNo, req)
}

func (s *service) V2GetOrder(ctx context.Context, orderNo, checkoutToken string) (*dto.V2OrderResponse, error) {
	return s.v2.GetOrder(ctx, orderNo, checkoutToken)
}

func (s *service) V2EventTicket(ctx context.Context, orderNo, checkoutToken string) (*dto.V2EventTicketResponse, error) {
	return s.v2.EventTicket(ctx, orderNo, checkoutToken)
}

func (s *service) V2Session(ctx context.Context, orderNo, checkoutToken string) (*dto.V2OrderSessionResponse, error) {
	return s.v2.Session(ctx, orderNo, checkoutToken)
}

func (s *service) V2StreamOrderEvents(ctx context.Context, req V2EventStreamRequest, sink V2EventSink) error {
	return s.v2.StreamEvents(ctx, req, sink)
}

func (s *service) PublishOrderEvents(ctx context.Context) error {
	return s.outbox.Publish(ctx)
}

func (s *service) CleanupOrderEvents(ctx context.Context, cutoff time.Time) (int64, error) {
	return s.outbox.Cleanup(ctx, cutoff)
}

func (s *service) CommissionWithdraw(ctx context.Context, req *dto.CommissionWithdrawRequest) (*dto.WithdrawalLog, error) {
	return s.wallet.CommissionWithdraw(ctx, req)
}

func (s *service) QueryUserBalanceLog(ctx context.Context) (*dto.QueryUserBalanceLogListResponse, error) {
	return s.wallet.QueryUserBalanceLog(ctx)
}

func (s *service) QueryUserCommissionLog(ctx context.Context, req *dto.QueryUserCommissionLogListRequest) (*dto.QueryUserCommissionLogListResponse, error) {
	return s.wallet.QueryUserCommissionLog(ctx, req)
}

func (s *service) QueryWithdrawalLog(ctx context.Context, req *dto.QueryWithdrawalLogListRequest) (*dto.QueryWithdrawalLogListResponse, error) {
	return s.wallet.QueryWithdrawalLog(ctx, req)
}

func (s *service) GetWithdrawalList(ctx context.Context, req *dto.GetWithdrawalListRequest) (*dto.GetWithdrawalListResponse, error) {
	return s.wallet.GetWithdrawalList(ctx, req)
}

func (s *service) ReviewWithdrawal(ctx context.Context, req *dto.ReviewWithdrawalRequest) error {
	return s.wallet.ReviewWithdrawal(ctx, req)
}

func (s *service) QueryUserAffiliate(ctx context.Context) (*dto.QueryUserAffiliateCountResponse, error) {
	return s.wallet.QueryUserAffiliate(ctx)
}

func (s *service) QueryUserAffiliateList(ctx context.Context, req *dto.QueryUserAffiliateListRequest) (*dto.QueryUserAffiliateListResponse, error) {
	return s.wallet.QueryUserAffiliateList(ctx, req)
}

func (s *service) FindWallet(ctx context.Context, userID int64) (*walletEntity.Wallet, error) {
	return s.wallet.FindWallet(ctx, userID)
}

func (s *service) FindWallets(ctx context.Context, userIDs []int64) (map[int64]*walletEntity.Wallet, error) {
	return s.wallet.FindWallets(ctx, userIDs)
}

func (s *service) OpenWallet(ctx context.Context, opening walletEntity.Wallet) error {
	return s.wallet.OpenWallet(ctx, opening)
}

func (s *service) AdjustWallet(ctx context.Context, adjustment walletEntity.Adjustment) error {
	return s.wallet.AdjustWallet(ctx, adjustment)
}

func (s *service) UnsubscribeRefundSettled(ctx context.Context, subscriptionID int64) (bool, error) {
	return s.wallet.UnsubscribeRefundSettled(ctx, subscriptionID)
}

func (s *service) SettleUnsubscribeRefund(ctx context.Context, userID, subscriptionID, orderID, amount int64) error {
	return s.wallet.SettleUnsubscribeRefund(ctx, userID, subscriptionID, orderID, amount)
}

func (s *service) QuotaGiftCredited(ctx context.Context, taskID, subscriptionID int64) (bool, error) {
	return s.wallet.QuotaGiftCredited(ctx, taskID, subscriptionID)
}

func (s *service) CreditQuotaGift(ctx context.Context, taskID, subscriptionID, userID, amount int64, at time.Time) error {
	return s.wallet.CreditQuotaGift(ctx, taskID, subscriptionID, userID, amount, at)
}

func (s *service) FindOrder(ctx context.Context, id int64) (*order.Order, error) {
	return s.orderRows.FindOne(ctx, id)
}

func (s *service) FindOrderByNo(ctx context.Context, orderNo string) (*order.Order, error) {
	return s.orderRows.FindOneByOrderNo(ctx, orderNo)
}

func (s *service) FindOrderDetails(ctx context.Context, id int64) (*order.Details, error) {
	return s.orderRows.FindOneDetails(ctx, id)
}

func (s *service) OrdersByStatusAfter(ctx context.Context, status uint8, afterID int64, limit int) ([]*order.Order, error) {
	return s.orderRows.QueryOrdersByStatusAfterID(ctx, status, afterID, limit)
}

func (s *service) ActivateRecharge(ctx context.Context, orderNo string) (int64, error) {
	return s.activation.ActivateRecharge(ctx, orderNo)
}

func (s *service) ActivatePaidOrder(ctx context.Context, orderNo string) error {
	return s.paidOrders.Activate(ctx, orderNo)
}

func (s *service) SettleOrderCommission(ctx context.Context, orderNo string, buyerID int64) error {
	return s.activation.SettleOrderCommission(ctx, orderNo, buyerID)
}

func (s *service) FinalizeOrder(ctx context.Context, orderNo string) error {
	return s.activation.FinalizeOrder(ctx, orderNo)
}

func (s *service) DailyOrderReport(ctx context.Context, date time.Time) (*DailyOrderReport, error) {
	return s.orders.DailyReport(ctx, date)
}

// Store is the persistence capability the order and wallet flows need beyond
// their repositories: billing-scoped transactions, the wallet view and the
// inbox. It excludes unrelated repositories and application-wide
// transactions.
type Store interface {
	InBillingTx(ctx context.Context, fn func(repository.BillingStore) error) error
	Inbox() repository.InboxRepo
	Wallet() repository.WalletRepo
}

// portalOrderEvents passes the order-event table to the storefront, which
// dates a guest order's settlement by its payment event; a facade built
// without the table (some flows' tests) hands it none, and the storefront
// then refuses every guest session exchange.
func portalOrderEvents(events OrderEventStore) portal.OrderEvents {
	if events == nil {
		return nil
	}
	return events
}

// unmatchedPaymentLog passes the system log to the callback flow, which
// records the gateway payments it cannot settle in it; a facade built
// without the log (some flows' tests) only reports them in the process log.
func unmatchedPaymentLog(logs repository.LogRepo) callbacks.UnmatchedPaymentLog {
	if logs == nil {
		return nil
	}
	return logs
}

// Guest replays of a V2 create request are bounded per idempotency key and
// per client IP over the order's payment window: a replay proves the guest
// password against the order, so the bound is what keeps a leaked key from
// becoming a password oracle. A legitimate guest replays a handful of times
// while the page reloads; an office behind one address a few dozen.
const (
	guestReplayPeriod   = order.PaymentWindow
	guestReplaysPerKey  = 20
	guestReplaysPerIP   = 60
	guestReplayKeySpace = "billing:v2:guest-replay:key:"
	guestReplayIPSpace  = "billing:v2:guest-replay:ip:"
)

// guestReplayLimits builds the replay limits over Redis; without Redis (some
// flows' tests) replays are not limited.
func guestReplayLimits(rds *redis.Client) v2orch.GuestReplayLimits {
	if rds == nil {
		return v2orch.GuestReplayLimits{}
	}
	period := int(guestReplayPeriod.Seconds())
	return v2orch.GuestReplayLimits{
		PerKey: ratelimit.NewPeriodLimit(period, guestReplaysPerKey, rds, guestReplayKeySpace),
		PerIP:  ratelimit.NewPeriodLimit(period, guestReplaysPerIP, rds, guestReplayIPSpace),
	}
}

// storeWallets reads wallets through the store, which may be absent in a
// facade built for a subset of the flows.
type storeWallets struct{ store Store }

func (w storeWallets) FindWallet(ctx context.Context, userID int64) (*walletEntity.Wallet, error) {
	if w.store == nil {
		return nil, nil
	}
	return w.store.Wallet().FindWallet(ctx, userID)
}
