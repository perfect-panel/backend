// Package checkout implements the user-facing money flows of the billing
// module: purchase, renewal, traffic reset, recharge, order preview and
// close. Only the module facade may reach it.
package checkout

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/billing/entity/coupon"
	orderEntity "github.com/perfect-panel/backend/internal/module/billing/entity/order"
	paymentEntity "github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	walletEntity "github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/ledger"
	"github.com/perfect-panel/backend/internal/module/billing/internal/orderaudit"
	"github.com/perfect-panel/backend/internal/module/billing/internal/pricing"
	"github.com/perfect-panel/backend/internal/module/billing/internal/settle"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	subscribeEntity "github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Order lifecycle constants shared with the V2 orchestration layer via the
// module facade.
const (
	CloseOrderTimeMinutes = orderEntity.PaymentWindowMinutes

	// MaxOrderAmount Order amount limits
	MaxOrderAmount    = 2147483647 // int32 max value (2.1 billion)
	MaxRechargeAmount = 2000000000 // 2 billion, slightly lower for safety
	MinRechargeAmount = 100        // minimum recharge amount in minor currency units
	MaxQuantity       = 1000       // Maximum quantity per order
)

// ErrGatewayUnconfirmed reports that a gateway order could not be confirmed
// safe to close, so the order intentionally stays pending.
var ErrGatewayUnconfirmed = gateway.ErrUnconfirmed

// PlanReader is the module's port onto the subscription domain's plan
// catalogue; the legacy subscribe repository satisfies it structurally.
type PlanReader interface {
	FindOne(ctx context.Context, id int64) (*subscribeEntity.Subscribe, error)
}

// UserSubscriptionReader is the module's port onto the subscription domain's
// user subscriptions; the legacy user-subscription repository satisfies it
// structurally.
type UserSubscriptionReader interface {
	HasBlockingSubscription(ctx context.Context, userID int64) (bool, error)
	CountQuotaConsumingSubscriptions(ctx context.Context, userID, subscribeID int64) (int64, error)
	FindOneUserSubscribe(ctx context.Context, id int64) (*usersub.SubscribeDetails, error)
	FindOneSubscribe(ctx context.Context, id int64) (*usersub.Subscribe, error)
}

// Orders is the order persistence the flows use outside a transaction;
// settlement marks orders paid through it.
type Orders interface {
	FindOneByOrderNo(ctx context.Context, orderNo string) (*orderEntity.Order, error)
	ReservePaymentCapture(ctx context.Context, orderNo, tradeNo string) (bool, error)
	MarkOrderPaid(ctx context.Context, orderNo, tradeNo string) (bool, error)
	CountUserCouponUsage(ctx context.Context, userID int64, coupon string) (int64, error)
	IsUserEligibleForNewOrder(ctx context.Context, userID int64) (bool, error)
}

// Wallets reads the buyer's gift credit for a price preview.
type Wallets interface {
	FindWallet(ctx context.Context, userID int64) (*walletEntity.Wallet, error)
}

// Transactor runs billing-scoped transactions.
type Transactor interface {
	InBillingTx(ctx context.Context, fn func(repository.BillingStore) error) error
}

// OrderQueue mirrors the facade's order queue port.
type OrderQueue interface {
	EnqueueActivation(ctx context.Context, orderNo string) error
	EnqueueDeferredClose(ctx context.Context, orderNo string) error
}

type Inventory interface {
	Reserve(context.Context, string, int64) error
	Restore(context.Context, string, int64) error
}

type Deps struct {
	Orders    Orders
	Coupons   pricing.CouponFinder
	Payments  gateway.MethodFinder
	Plans     PlanReader
	UserSubs  UserSubscriptionReader
	Wallets   Wallets
	Tx        Transactor
	Inventory Inventory
	Queue     OrderQueue
	// Gateways opens the gateway of an order's payment method; nil selects
	// the production gateways.
	Gateways *gateway.Registry
	// SingleModel forbids holding more than one blocking subscription;
	// read per request because the admin can change it at runtime.
	SingleModel func() bool
	// CurrencyUnit is the site currency; read per request because the admin
	// can change it at runtime.
	CurrencyUnit func() string
}

type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	if deps.Gateways == nil {
		deps.Gateways = gateway.NewRegistry()
	}
	return &Service{deps: deps}
}

// currentUser returns the authenticated buyer of the request.
func currentUser(ctx context.Context) (*user.User, error) {
	u, ok := user.FromContext(ctx)
	if !ok {
		return nil, xerr.Errorf(xerr.InvalidAccess, "Invalid Access")
	}
	return u, nil
}

// orderQuantity defaults a missing quantity to one unit and caps it.
func orderQuantity(quantity int64) (int64, error) {
	if quantity <= 0 {
		return 1, nil
	}
	if quantity > MaxQuantity {
		return 0, xerr.Errorf(xerr.InvalidParams, "quantity exceeds maximum limit of %d", MaxQuantity)
	}
	return quantity, nil
}

// PlanTerms are what prices an order for a plan: the quantity, the coupon
// checked against the plan, and the payment method whose fee applies.
type PlanTerms struct {
	Plan     *subscribeEntity.Subscribe
	Quantity int64
	Coupon   *coupon.Coupon
	Method   *paymentEntity.Payment
}

// Input is the pricing input of the terms for a buyer holding gift credit.
func (t PlanTerms) Input(giftCredit int64) pricing.Input {
	return pricing.Input{
		UnitPrice:  t.Plan.UnitPrice,
		Quantity:   t.Quantity,
		Tiers:      pricing.ParseTiers(t.Plan.Discount),
		Coupon:     pricing.CouponTerms(t.Coupon),
		GiftCredit: giftCredit,
		Fee:        pricing.FeeTerms(t.Method),
	}
}

// ResolvePlanTerms resolves the coupon an order names and the payment method
// it is paid with. A zero paymentID prices the order without a method, as a
// preview does before the buyer chose one.
func ResolvePlanTerms(ctx context.Context, coupons pricing.CouponFinder, methods gateway.MethodFinder, plan *subscribeEntity.Subscribe, quantity int64, code string, paymentID int64) (PlanTerms, error) {
	terms := PlanTerms{Plan: plan, Quantity: quantity}
	var err error
	if terms.Coupon, err = pricing.ResolveCoupon(ctx, coupons, code, plan.Id, timeutil.Now()); err != nil {
		return terms, err
	}
	if paymentID != 0 {
		if terms.Method, err = gateway.LookupMethod(ctx, methods, paymentID); err != nil {
			return terms, err
		}
	}
	return terms, nil
}

// planOnSale rejects a plan that is not sold.
func planOnSale(plan *subscribeEntity.Subscribe) error {
	if plan.Sell == nil || !*plan.Sell {
		return xerr.Errorf(xerr.ERROR, "subscribe not sell")
	}
	return nil
}

// couponUserLimiter counts a buyer's uses of a coupon; the order repository
// satisfies it both outside and inside a transaction.
type couponUserLimiter interface {
	CountUserCouponUsage(ctx context.Context, userID int64, coupon string) (int64, error)
}

// ensureCouponUserLimit rejects an order that would exceed the coupon's
// per-user limit. The check before the order transaction is only a fast
// path: concurrent orders all pass it. Inside the transaction callers hold
// the buyer's wallet row lock, which serializes the buyer's order creation,
// and repeat the check before any other plain read, so the count sees every
// order committed by a request that held the lock first (MySQL REPEATABLE
// READ fixes the snapshot at the first consistent read).
func ensureCouponUserLimit(ctx context.Context, orders couponUserLimiter, userID int64, c *coupon.Coupon) error {
	if c == nil || c.UserLimit <= 0 {
		return nil
	}
	count, err := orders.CountUserCouponUsage(ctx, userID, c.Code)
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "count coupon uses of user %d", userID)
	}
	if count >= c.UserLimit {
		return xerr.Errorf(xerr.CouponInsufficientUsage, "coupon limit exceeded")
	}
	return nil
}

// EnsureCouponUserLimit is ensureCouponUserLimit for the order creators of
// other subdomains, such as an administrator's.
func EnsureCouponUserLimit(ctx context.Context, orders CouponUserLimiter, userID int64, c *coupon.Coupon) error {
	return ensureCouponUserLimit(ctx, orders, userID, c)
}

// CouponUserLimiter counts a buyer's uses of a coupon; the order repository
// satisfies it both outside and inside a transaction.
type CouponUserLimiter = couponUserLimiter

// ReserveCoupon claims one use of the order's coupon inside its creation
// transaction.
func ReserveCoupon(ctx context.Context, tx repository.BillingStore, o *orderEntity.Order) error {
	if o.Coupon == "" {
		return nil
	}
	reserved, err := tx.Coupon().ReserveUsage(ctx, o.Coupon, timeutil.Now().UnixMilli())
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "reserve coupon %q", o.Coupon)
	}
	if !reserved {
		return xerr.Errorf(xerr.CouponInsufficientUsage, "coupon used or expired")
	}
	o.CouponReserved = true
	return nil
}

// InsertOrder stores the order and its audit record as one atomic billing
// operation; source names who created it.
func InsertOrder(ctx context.Context, tx repository.BillingStore, o *orderEntity.Order, source string) error {
	if err := tx.Order().Insert(ctx, o); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "insert order %s", o.OrderNo)
	}
	if err := orderaudit.InsertCreated(ctx, tx.Log(), o, source); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "record order %s", o.OrderNo)
	}
	return nil
}

// lockWallet locks the buyer's wallet row for the order transaction. The
// request-context user is only an authentication snapshot; the locked row
// is what gift credit may be spent from.
func lockWallet(ctx context.Context, tx repository.BillingStore, userID int64) (*walletEntity.Wallet, error) {
	w, err := tx.Wallet().FindOneForUpdate(ctx, userID)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "lock wallet of user %d", userID)
	}
	return w, nil
}

// spendGift deducts the order's gift credit from the locked wallet and
// records the movement.
func spendGift(ctx context.Context, tx repository.BillingStore, w *walletEntity.Wallet, o *orderEntity.Order, remark string) error {
	if o.GiftAmount <= 0 {
		return nil
	}
	w.GiftAmount -= o.GiftAmount
	if err := tx.Wallet().UpdateBalanceFields(ctx, w); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "deduct gift credit of user %d", w.UserId)
	}
	return ledger.SpendGift(ctx, tx.Log(), ledger.Gift{
		UserID: w.UserId, OrderNo: o.OrderNo, Amount: o.GiftAmount, Balance: w.GiftAmount, Remark: remark,
	})
}

// ApplyQuote copies a quote onto the order being created.
func ApplyQuote(o *orderEntity.Order, q pricing.Quote) {
	o.Price = q.Price
	o.Discount = q.Discount
	o.CouponDiscount = q.CouponDiscount
	o.GiftAmount = q.GiftAmount
	o.FeeAmount = q.FeeAmount
	o.Amount = q.Amount
}

// orderAmountWithinLimit rejects amounts beyond what an order may carry.
func orderAmountWithinLimit(amount int64) error {
	if amount > MaxOrderAmount {
		return xerr.Errorf(xerr.InvalidParams, "order amount exceeds maximum limit")
	}
	return nil
}

// enqueueDeferredClose schedules the pending order's expiry close. Failures
// are logged, not fatal: the pending-order reconciler re-drives expiry.
func (s *Service) enqueueDeferredClose(ctx context.Context, tag, orderNo string) {
	if err := s.deps.Queue.EnqueueDeferredClose(ctx, orderNo); err != nil {
		logger.WithContext(ctx).Errorw(tag+" Enqueue task error", logger.Field("error", err.Error()), logger.Field("orderNo", orderNo))
	} else {
		logger.WithContext(ctx).Infow(tag+" Enqueue task success", logger.Field("orderNo", orderNo))
	}
}

// settleVerifiedPayment marks a gateway-verified payment as paid and enqueues
// activation. Callers must authenticate the gateway response and verify the
// order amount before invoking it. The committed Paid state is the durable
// outbox: an enqueue failure is repaired by paid-order reconciliation.
func (s *Service) settleVerifiedPayment(ctx context.Context, orderInfo *orderEntity.Order, tradeNo string) error {
	return settle.VerifiedPayment(ctx, s.deps.Orders, s.deps.Queue, orderInfo, tradeNo)
}
