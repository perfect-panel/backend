// Package adminorder implements the admin-side order management subdomain of
// the billing module. Only the module facade may reach it.
package adminorder

import (
	"context"
	"errors"
	"time"

	"github.com/perfect-panel/backend/internal/infra/mapping"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	couponEntity "github.com/perfect-panel/backend/internal/module/billing/entity/coupon"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	paymentEntity "github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/checkout"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/orderaudit"
	"github.com/perfect-panel/backend/internal/module/billing/internal/pricing"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// Orders is the order persistence administration reads outside a
// transaction.
type Orders interface {
	FindOne(ctx context.Context, id int64) (*order.Order, error)
	QueryOrderListByPage(ctx context.Context, page, size int, status uint8, user, subscribe int64, search string) (int64, []*order.Details, error)
	QueryDailyReport(ctx context.Context, date time.Time) (*order.DailyReport, error)
}

// Transactor mirrors the facade's billing-scoped transaction port.
type Transactor interface {
	InBillingTx(ctx context.Context, fn func(repository.BillingStore) error) error
}

// ActivationEnqueuer mirrors the facade's activation queue port.
type ActivationEnqueuer interface {
	EnqueueActivation(ctx context.Context, orderNo string) error
}

// PlanNameReader is the read port onto the subscription domain's plan
// catalogue, used to label the daily report's plan breakdown.
type PlanNameReader interface {
	FindOne(ctx context.Context, id int64) (*subscribe.Subscribe, error)
}

// UserSubscriptionReader resolves the user subscription a renewal or
// traffic-reset order an administrator creates applies to.
type UserSubscriptionReader interface {
	FindOneUserSubscribe(ctx context.Context, id int64) (*usersub.SubscribeDetails, error)
}

// Inventory reserves a plan unit for a new subscription order, as the
// buyer's own purchase reserves it (ADR-001 step 2).
type Inventory interface {
	Reserve(ctx context.Context, orderNo string, subscribeID int64) error
}

// OrderCloser is the checkout close flow shared with owner and expiry closes;
// it reports whether the call closed the order.
type OrderCloser interface {
	CloseByAdmin(ctx context.Context, orderNo string, adminID int64) (bool, error)
}

type Deps struct {
	Orders   Orders
	Payments gateway.MethodFinder
	// Coupons resolves the coupon an administrator's order names; UserSubs
	// the subscription a renewal or traffic reset applies to; Inventory
	// holds the plan unit of a new subscription order. An order that needs
	// one of them is refused when it is not configured.
	Coupons   pricing.CouponFinder
	UserSubs  UserSubscriptionReader
	Inventory Inventory
	Tx        Transactor
	Queue     ActivationEnqueuer
	// Plans resolves plan names for the daily report; optional so callers
	// that only manage orders need not provide it.
	Plans  PlanNameReader
	Closer OrderCloser
}

type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// Create opens a pending order on an administrator's behalf under the rules
// a buyer's order follows: the type is one the activation knows, a renewal
// or traffic reset names the subscription it applies to, a coupon use and
// the plan unit of a new subscription are reserved while the order is
// pending, and the trade number is left for the gateway that collects the
// payment.
func (s *Service) Create(ctx context.Context, req *dto.CreateOrderRequest) error {
	if req.Status != 0 && req.Status != order.StatusPending {
		return xerr.Errorf(xerr.InvalidInitialOrderStatus, "admin-created orders must start pending")
	}
	if err := validateCreate(req); err != nil {
		return err
	}
	paymentMethod, err := findPaymentMethod(ctx, s.deps.Payments, req.PaymentId)
	if err != nil {
		return err
	}
	orderInfo := &order.Order{
		UserId:         req.UserId,
		OrderNo:        order.GenerateTradeNo(),
		Type:           req.Type,
		Quantity:       max(req.Quantity, 1),
		Price:          req.Price,
		Amount:         req.Amount,
		Discount:       req.Discount,
		Coupon:         req.Coupon,
		CouponDiscount: req.CouponDiscount,
		PaymentId:      req.PaymentId,
		Method:         paymentMethod.Platform,
		FeeAmount:      req.FeeAmount,
		Status:         order.StatusPending,
		SubscribeId:    req.SubscribeId,
	}
	if err := s.bindSubscription(ctx, req, orderInfo); err != nil {
		return err
	}
	c, err := s.resolveCoupon(ctx, req, orderInfo)
	if err != nil {
		return err
	}
	if err := s.deps.Tx.InBillingTx(ctx, func(txStore repository.BillingStore) error {
		if err := checkout.EnsureCouponUserLimit(ctx, txStore.Order(), orderInfo.UserId, c); err != nil {
			return err
		}
		if err := checkout.ReserveCoupon(ctx, txStore, orderInfo); err != nil {
			return err
		}
		return checkout.InsertOrder(ctx, txStore, orderInfo, orderaudit.SourceAdmin)
	}); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "create order")
	}
	// The plan unit is reserved in its own subscription-domain transaction
	// (ADR-001 step 2); a unit that cannot be reserved closes the order
	// again, returning its coupon use.
	if orderInfo.Type == order.TypeSubscribe {
		if err := s.deps.Inventory.Reserve(ctx, orderInfo.OrderNo, orderInfo.SubscribeId); err != nil {
			s.closeUnreservedOrder(ctx, orderInfo)
			if errors.Is(err, subscription.ErrOutOfStock) {
				return xerr.Errorf(xerr.SubscribeOutOfStock, "subscribe out of stock")
			}
			return xerr.Wrapf(err, xerr.ERROR, "reserve inventory of order %s", orderInfo.OrderNo)
		}
	}
	return nil
}

// validateCreate checks what the request validator cannot: the order type
// is one the activation knows, the quantity is at least one unit, a
// renewal or traffic reset names its subscription, a new subscription names
// its plan, and the trade number and coupon fit the type.
func validateCreate(req *dto.CreateOrderRequest) error {
	switch req.Type {
	case order.TypeSubscribe:
		if req.SubscribeId <= 0 {
			return xerr.Errorf(xerr.InvalidParams, "subscribe_id is required for a subscription order")
		}
	case order.TypeRenewal, order.TypeResetTraffic:
		if req.UserSubscribeId <= 0 {
			return xerr.Errorf(xerr.InvalidParams, "user_subscribe_id is required for a renewal or traffic reset order")
		}
	case order.TypeRecharge:
	default:
		return xerr.Errorf(xerr.InvalidParams, "unsupported order type %d", req.Type)
	}
	if req.Quantity < 0 {
		return xerr.Errorf(xerr.InvalidParams, "quantity must be at least 1")
	}
	if req.TradeNo != "" {
		// Stripe and Cryptomus read the trade number as their own payment
		// identifier; an administrator's free text would break their
		// checkout and reconciliation.
		return xerr.Errorf(xerr.InvalidParams, "trade_no is assigned by the payment gateway and cannot be set")
	}
	if req.Coupon != "" && req.Type != order.TypeSubscribe && req.Type != order.TypeRenewal {
		return xerr.Errorf(xerr.InvalidParams, "a coupon applies to subscription and renewal orders only")
	}
	return nil
}

// bindSubscription attaches the user subscription a renewal or traffic reset
// applies to: the order names it by id, which a token rotation does not
// change, and by token for the fulfillment of older releases.
func (s *Service) bindSubscription(ctx context.Context, req *dto.CreateOrderRequest, orderInfo *order.Order) error {
	if req.Type != order.TypeRenewal && req.Type != order.TypeResetTraffic {
		return nil
	}
	if s.deps.UserSubs == nil {
		return xerr.Errorf(xerr.ERROR, "user subscriptions are not available to administration")
	}
	userSubscribe, err := s.deps.UserSubs.FindOneUserSubscribe(ctx, req.UserSubscribeId)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return xerr.Errorf(xerr.InvalidParams, "user subscription %d not found", req.UserSubscribeId)
	}
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find user subscription %d", req.UserSubscribeId)
	}
	if userSubscribe.UserId != req.UserId {
		return xerr.Errorf(xerr.InvalidParams, "user subscription %d does not belong to user %d", req.UserSubscribeId, req.UserId)
	}
	if req.SubscribeId != 0 && req.SubscribeId != userSubscribe.SubscribeId {
		return xerr.Errorf(xerr.InvalidParams, "user subscription %d is not of plan %d", req.UserSubscribeId, req.SubscribeId)
	}
	orderInfo.ParentId = userSubscribe.OrderId
	orderInfo.SubscribeId = userSubscribe.SubscribeId
	orderInfo.UserSubscribeId = userSubscribe.Id
	orderInfo.SubscribeToken = userSubscribe.Token
	return nil
}

// resolveCoupon checks the coupon the order names against its plan, as a
// buyer's order is checked, and returns it for the per-user limit.
func (s *Service) resolveCoupon(ctx context.Context, req *dto.CreateOrderRequest, orderInfo *order.Order) (*couponEntity.Coupon, error) {
	if req.Coupon == "" {
		return nil, nil
	}
	if s.deps.Coupons == nil {
		return nil, xerr.Errorf(xerr.ERROR, "coupons are not available to administration")
	}
	return pricing.ResolveCoupon(ctx, s.deps.Coupons, req.Coupon, orderInfo.SubscribeId, timeutil.Now())
}

// closeUnreservedOrder closes an order whose plan unit could not be reserved
// and releases its coupon use.
func (s *Service) closeUnreservedOrder(ctx context.Context, orderInfo *order.Order) {
	err := s.deps.Tx.InBillingTx(ctx, func(tx repository.BillingStore) error {
		closed, err := tx.Order().UpdateOrderStatusFrom(ctx, orderInfo.OrderNo, order.StatusPending, order.StatusClosed)
		if err != nil {
			return err
		}
		if closed && orderInfo.CouponReserved {
			return tx.Coupon().ReleaseUsage(ctx, orderInfo.Coupon)
		}
		return nil
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("[CreateOrder] Close order after reservation failure failed", logger.Field("error", err.Error()), logger.Field("orderNo", orderInfo.OrderNo))
	}
}

func (s *Service) List(ctx context.Context, req *dto.GetOrderListRequest) (*dto.GetOrderListResponse, error) {
	total, list, err := s.deps.Orders.QueryOrderListByPage(ctx, int(req.Page), int(req.Size), req.Status, req.UserId, req.SubscribeId, req.Search)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "query order list")
	}
	resp := &dto.GetOrderListResponse{Total: total, List: make([]dto.Order, 0)}
	if err := mapping.Copy(&resp.List, list); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "map order list")
	}
	return resp, nil
}

// UpdateStatus applies an administrator's decision on a pending order: mark
// it paid with the gateway's trade number, or close it.
func (s *Service) UpdateStatus(ctx context.Context, req *dto.UpdateOrderStatusRequest) error {
	info, err := s.deps.Orders.FindOne(ctx, req.Id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return xerr.Errorf(xerr.OrderNotExist, "order %d not found", req.Id)
	}
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find order %d", req.Id)
	}
	// Orders have a deliberately narrow state machine. Arbitrary status writes
	// could resurrect terminal orders or skip the activation workflow.
	if req.Status != order.StatusPaid && req.Status != order.StatusClosed {
		return xerr.Errorf(xerr.InvalidOrderTransition, "only pending orders may be marked paid or closed")
	}
	if req.Status == order.StatusPaid && req.TradeNo == "" {
		return xerr.Errorf(xerr.TradeNoRequired, "trade_no is required when marking an order paid")
	}
	if req.Status == order.StatusClosed && (req.PaymentId != 0 || req.TradeNo != "") {
		return xerr.Errorf(xerr.InvalidOrderCloseRequest, "payment_id and trade_no are not allowed when closing an order")
	}
	if req.Status == order.StatusClosed {
		return s.close(ctx, info)
	}

	err = s.deps.Tx.InBillingTx(ctx, func(txStore repository.BillingStore) error {
		orderStore := txStore.Order()
		current, err := orderStore.FindOneByOrderNoForUpdate(ctx, info.OrderNo)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "lock order %s", info.OrderNo)
		}
		if current.Status != order.StatusPending {
			return xerr.Errorf(xerr.OrderStatusError, "order is no longer pending")
		}
		if req.PaymentId != 0 {
			paymentMethod, err := findPaymentMethod(ctx, txStore.Payment(), req.PaymentId)
			if err != nil {
				return err
			}
			current.PaymentId = paymentMethod.Id
			current.Method = paymentMethod.Platform
			if err := orderStore.Update(ctx, current); err != nil {
				return xerr.Wrapf(err, xerr.DatabaseUpdateError, "rebind order %s", info.OrderNo)
			}
		}
		transitioned, err := orderStore.MarkOrderPaid(ctx, current.OrderNo, req.TradeNo)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "mark order %s paid", info.OrderNo)
		}
		if !transitioned {
			return xerr.Errorf(xerr.OrderStatusError, "order is no longer pending")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := s.deps.Queue.EnqueueActivation(ctx, info.OrderNo); err != nil {
		// The order is committed as paid, which is what the administrator
		// asked for; the paid-order reconciler re-drives the activation. A
		// reported failure would only invite a retry that the order, no
		// longer pending, refuses.
		logger.WithContext(ctx).Errorw("[UpdateOrderStatus] enqueue activation failed; the paid-order reconciler retries it",
			logger.Field("order_no", info.OrderNo), logger.Field("error", err.Error()))
	}
	return nil
}

// close routes the administrator's close through the checkout close flow, so
// the coupon, gift deduction and plan inventory are released and a
// cancellable gateway payment is voided first. A payment the gateway confirms
// is settled instead, and the close reports the order as no longer pending.
func (s *Service) close(ctx context.Context, info *order.Order) error {
	var adminID int64
	if admin, ok := user.FromContext(ctx); ok {
		adminID = admin.Id
	}
	closed, err := s.deps.Closer.CloseByAdmin(ctx, info.OrderNo, adminID)
	if err != nil {
		// A gateway that cannot confirm the payment carries its own code
		// (PaymentStatusUnconfirmed) through the generic wrap.
		return xerr.Wrapf(err, xerr.ERROR, "close order %s", info.OrderNo)
	}
	if !closed {
		return xerr.Errorf(xerr.OrderStatusError, "order is no longer pending")
	}
	return nil
}

// findPaymentMethod loads the method an administrator binds an order to. A
// disabled method may still be bound by hand; a missing one may not.
func findPaymentMethod(ctx context.Context, methods gateway.MethodFinder, id int64) (*paymentEntity.Payment, error) {
	method, err := methods.FindOne(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, xerr.Errorf(xerr.PaymentMethodNotFound, "payment method %d not found", id)
	}
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find payment method %d", id)
	}
	return method, nil
}
