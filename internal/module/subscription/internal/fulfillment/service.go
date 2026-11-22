// Package fulfillment applies a paid order's business effect: creating,
// renewing or traffic-resetting the user subscription in the fulfillment
// transaction, idempotent via the inbox marker. Only the module facade may
// reach it.
package fulfillment

import (
	"context"
	"fmt"
	"strconv"
	"time"
	"uuid"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/entitlement"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/period"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

const (
	inboxFulfillment = "subscription.fulfillment"
	// appleIAPMethod is the payment method of an order Apple manages; its
	// entitlement arrives through ReconcileEntitlement, never through local
	// fulfillment.
	appleIAPMethod = "AppleIAP"
)

// ErrInvalidOrderType rejects order types this subdomain does not fulfil.
var ErrInvalidOrderType = fmt.Errorf("invalid order type")

// NotifyKind labels the fulfillment outcome for the caller's notification
// dispatch, without coupling this module to the notification templates.
const (
	NotifyPurchase     = "purchase"
	NotifyRenewal      = "renewal"
	NotifyResetTraffic = "reset_traffic"
)

// Outcome carries what the caller needs after the fulfillment committed:
// notification context only — every domain mutation is already committed.
type Outcome struct {
	UserID     int64
	PlanName   string
	NotifyKind string
	HasSub     bool
	ExpireAt   time.Time
}

// outcomeParts is the internal working set assembled inside the fulfillment
// transaction (entities stay inside the module).
type outcomeParts struct {
	periodStart time.Time
	order       *order.Order
	subscribe   *subscribe.Subscribe
	userSub     *usersub.Subscribe
	notifyType  string
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	// Orders is the billing-domain read port resolving the paid order.
	Orders OrderReader
	// Store carries the subscription-scoped fulfillment transaction; the
	// per-user quota serialization uses the domain's own serial lock.
	Store    Store
	UserSubs repository.UserSubscriptionRepo
	Plans    repository.SubscribeRepo
	Cache    CacheInvalidator
	// SingleModel forbids holding more than one blocking subscription;
	// runtime-mutable, read per request.
	SingleModel func() bool
}

// OrderReader is the billing-domain read port resolving the paid order.
type OrderReader interface {
	FindOne(ctx context.Context, id int64) (*order.Order, error)
	FindOneByOrderNo(ctx context.Context, orderNo string) (*order.Order, error)
}

// CacheInvalidator drops cached subscription rows.
type CacheInvalidator interface {
	ClearSubscribeCache(ctx context.Context, data ...*usersub.Subscribe) error
}

// Service is the fulfillment entry point used by the subscription facade.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// FulfillPaidOrder applies the order's effect exactly once and returns the
// notification context. A replayed delivery whose fulfillment already
// committed rebuilds the context without re-applying anything; post-commit
// cache invalidation runs on every path because it is retryable.
func (s *Service) FulfillPaidOrder(ctx context.Context, orderNo string) (*Outcome, error) {
	orderInfo, err := s.deps.Orders.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if orderInfo.Method == appleIAPMethod {
		return nil, usersub.ErrProviderManaged
	}
	mark, err := s.deps.Store.Inbox().Find(ctx, inboxFulfillment, orderNo)
	if err != nil {
		return nil, err
	}
	var parts *outcomeParts
	alreadyFulfilled := mark != nil
	if !alreadyFulfilled && orderInfo.Type != order.TypeResetTraffic {
		period, err := s.deps.Store.Entitlement().FindPeriod(ctx, entitlementKey("local", orderNo))
		if err != nil {
			return nil, err
		}
		alreadyFulfilled = period != nil
	}
	if alreadyFulfilled {
		parts, err = s.loadOutcome(ctx, orderInfo)
	} else {
		err = s.deps.Store.InSubscriptionTx(ctx, func(store repository.SubscriptionStore) error {
			var txErr error
			parts, txErr = s.processOrderByTypeInTx(ctx, store, orderInfo)
			if txErr != nil {
				return txErr
			}
			if orderInfo.Type != order.TypeResetTraffic {
				if err := store.Entitlement().InsertPeriod(ctx, &entitlement.Period{
					ID: entitlementKey("local", orderNo), EntitlementID: entitlementKey("local", strconv.FormatInt(parts.userSub.Id, 10)),
					TransactionKey: orderNo, UserSubscribeID: parts.userSub.Id, OrderID: orderInfo.Id,
					PlanID: orderInfo.SubscribeId, StartAt: parts.periodStart, EndAt: parts.userSub.ExpireTime,
					TrafficLimit: parts.userSub.Traffic,
				}); err != nil {
					return err
				}
			}
			// A duplicate key here means a concurrent delivery fulfilled
			// first; this transaction rolls back and the retry takes the
			// replay path.
			return store.Inbox().Insert(ctx, inboxFulfillment, orderNo, "")
		})
	}
	if err != nil {
		return nil, err
	}
	s.afterCommit(ctx, parts)
	return &Outcome{
		UserID:     orderInfo.UserId,
		PlanName:   parts.subscribe.Name,
		NotifyKind: parts.notifyType,
		HasSub:     parts.userSub != nil,
		ExpireAt:   expireOf(parts.userSub),
	}, nil
}

func expireOf(sub *usersub.Subscribe) time.Time {
	if sub == nil {
		return time.Time{}
	}
	return sub.ExpireTime
}

// loadOutcome rebuilds the notification context for a replayed delivery.
func (s *Service) loadOutcome(ctx context.Context, orderInfo *order.Order) (*outcomeParts, error) {
	var userSub *usersub.Subscribe
	var err error
	if orderInfo.Type == order.TypeSubscribe {
		// A new purchase created its subscription under this order.
		userSub, err = s.deps.UserSubs.FindOneSubscribeByOrderId(ctx, orderInfo.Id)
	} else {
		userSub, err = s.orderSubscription(ctx, orderInfo)
	}
	if err != nil {
		return nil, err
	}
	subID := orderInfo.SubscribeId
	if orderInfo.Type == order.TypeResetTraffic {
		subID = userSub.SubscribeId
	}
	sub, err := s.deps.Plans.FindOne(ctx, subID)
	if err != nil {
		return nil, err
	}
	parts := &outcomeParts{order: orderInfo, subscribe: sub, userSub: userSub}
	switch orderInfo.Type {
	case order.TypeSubscribe:
		parts.notifyType = NotifyPurchase
	case order.TypeRenewal:
		parts.notifyType = NotifyRenewal
	case order.TypeResetTraffic:
		parts.notifyType = NotifyResetTraffic
	}
	return parts, nil
}

// afterCommit runs the retryable cache invalidation for the committed
// fulfillment.
func (s *Service) afterCommit(ctx context.Context, parts *outcomeParts) {
	if parts.userSub != nil {
		if err := s.deps.Cache.ClearSubscribeCache(ctx, parts.userSub); err != nil {
			logger.WithContext(ctx).Error("[Fulfillment] Clear user subscribe cache failed", logger.Field("error", err.Error()))
		}
	}
	if parts.subscribe != nil {
		if err := s.deps.Plans.ClearCache(ctx, parts.subscribe.Id); err != nil {
			logger.WithContext(ctx).Error("[Fulfillment] Clear plan cache failed", logger.Field("error", err.Error()))
		}
	}
}

func (s *Service) processOrderByTypeInTx(ctx context.Context, store repository.SubscriptionStore, orderInfo *order.Order) (*outcomeParts, error) {
	switch orderInfo.Type {
	case order.TypeSubscribe:
		return s.activateNewPurchaseTx(ctx, store, orderInfo)
	case order.TypeRenewal:
		return s.activateRenewalTx(ctx, store, orderInfo)
	case order.TypeResetTraffic:
		return s.activateResetTrafficTx(ctx, store, orderInfo)
	default:
		return nil, ErrInvalidOrderType
	}
}

func (s *Service) activateNewPurchaseTx(ctx context.Context, store repository.SubscriptionStore, orderInfo *order.Order) (*outcomeParts, error) {
	// Guest accounts are created by ensureGuestAccount before this stage, so
	// UserId is always set here. The domain's serial lock serialises
	// per-user quota checks and subscription creation (it replaced the
	// cross-domain user-row lock).
	if err := store.UserSubscription().LockUserSerial(ctx, orderInfo.UserId); err != nil {
		return nil, err
	}

	sub, err := store.Subscribe().FindOne(ctx, orderInfo.SubscribeId)
	if err != nil {
		return nil, err
	}
	userSub, err := s.createUserSubscriptionTx(ctx, store, orderInfo, sub)
	if err != nil {
		return nil, err
	}
	return &outcomeParts{order: orderInfo, subscribe: sub, userSub: userSub, notifyType: NotifyPurchase, periodStart: userSub.StartTime}, nil
}

func (s *Service) createUserSubscriptionTx(ctx context.Context, store repository.SubscriptionStore, orderInfo *order.Order, sub *subscribe.Subscribe) (*usersub.Subscribe, error) {
	if s.deps.SingleModel() {
		hasBlockingSubscription, err := store.UserSubscription().HasBlockingSubscription(ctx, orderInfo.UserId)
		if err != nil {
			return nil, err
		}
		if hasBlockingSubscription {
			return nil, fmt.Errorf("single subscription mode exceeds limit")
		}
	}
	if sub.Quota > 0 {
		count, err := store.UserSubscription().CountQuotaConsumingSubscriptions(ctx, orderInfo.UserId, orderInfo.SubscribeId)
		if err != nil {
			return nil, err
		}
		if count >= sub.Quota {
			return nil, fmt.Errorf("subscribe quota limit exceeded")
		}
	}
	now := timeutil.Now()
	expireTime, err := termEnd(sub, orderInfo.Quantity, now)
	if err != nil {
		return nil, err
	}
	userSub := &usersub.Subscribe{
		UserId:      orderInfo.UserId,
		OrderId:     orderInfo.Id,
		SubscribeId: orderInfo.SubscribeId,
		StartTime:   now,
		ExpireTime:  expireTime,
		Traffic:     sub.Traffic,
		Token:       usersub.NewToken(),
		UUID:        uuid.NewV4().String(),
		Status:      usersub.SubscribeStatusActive,
	}
	if err := store.UserSubscription().InsertSubscribe(ctx, userSub); err != nil {
		return nil, err
	}
	return userSub, nil
}

// errOrderSubscription rejects a renewal or reset order that names no
// subscription at all: neither the id nor the token it had at checkout. A
// lookup by an empty token could otherwise resolve to a row of an older
// version that stored no token.
var errOrderSubscription = fmt.Errorf("order names no subscription")

// lockOrderSubscription locks the subscription a renewal or reset order is
// for. The order carries the subscription's id since it was introduced; the
// id survives the token rotations (the owner's, an administrator's or the
// rotation of every token) that happen between checkout and payment, which
// used to leave a paid order without a subscription to fulfil. Orders created
// before the id existed carry only the token they saw at checkout.
func lockOrderSubscription(ctx context.Context, store repository.SubscriptionStore, orderInfo *order.Order) (*usersub.Subscribe, error) {
	if orderInfo.UserSubscribeId > 0 {
		return store.UserSubscription().FindOneSubscribeForUpdate(ctx, orderInfo.UserSubscribeId)
	}
	if orderInfo.SubscribeToken == "" {
		return nil, errOrderSubscription
	}
	return store.UserSubscription().FindOneSubscribeByTokenForUpdate(ctx, orderInfo.SubscribeToken)
}

// orderSubscription reads, without locking, the subscription a renewal or
// reset order is for, resolving it like lockOrderSubscription.
func (s *Service) orderSubscription(ctx context.Context, orderInfo *order.Order) (*usersub.Subscribe, error) {
	if orderInfo.UserSubscribeId > 0 {
		return s.deps.UserSubs.FindOneSubscribe(ctx, orderInfo.UserSubscribeId)
	}
	if orderInfo.SubscribeToken == "" {
		return nil, errOrderSubscription
	}
	return s.deps.UserSubs.FindOneSubscribeByToken(ctx, orderInfo.SubscribeToken)
}

func (s *Service) activateRenewalTx(ctx context.Context, store repository.SubscriptionStore, orderInfo *order.Order) (*outcomeParts, error) {
	userSub, err := lockOrderSubscription(ctx, store, orderInfo)
	if err != nil {
		return nil, err
	}
	if userSub.UserId != orderInfo.UserId {
		return nil, fmt.Errorf("renewal subscription ownership mismatch")
	}
	if userSub.EntitlementSource != "" {
		return nil, usersub.ErrProviderManaged
	}
	// The order may have been created before the refund or the hold.
	if usersub.OnHold(userSub.Status) {
		return nil, usersub.ErrSubscriptionOnHold
	}
	sub, err := store.Subscribe().FindOne(ctx, orderInfo.SubscribeId)
	if err != nil {
		return nil, err
	}
	periodStart := userSub.ExpireTime
	if now := timeutil.Now(); periodStart.Before(now) {
		periodStart = now
	}
	if err := s.updateSubscriptionForRenewalTx(ctx, store, userSub, sub, orderInfo, periodStart); err != nil {
		return nil, err
	}
	return &outcomeParts{order: orderInfo, subscribe: sub, userSub: userSub, notifyType: NotifyRenewal, periodStart: periodStart}, nil
}

// updateSubscriptionForRenewalTx extends the locked subscription by the
// order's term from periodStart and writes only the columns a renewal owns,
// so the traffic accounting that waits on the row lock adds to what the
// renewal leaves instead of being overwritten by a stale copy.
func (s *Service) updateSubscriptionForRenewalTx(ctx context.Context, store repository.SubscriptionStore, userSub *usersub.Subscribe, sub *subscribe.Subscribe, orderInfo *order.Order, periodStart time.Time) error {
	expireTime, err := termEnd(sub, orderInfo.Quantity, periodStart)
	if err != nil {
		return err
	}
	columns := []string{"expire_time", "status", "finished_at"}
	if renewalResetsTraffic(period.App(), sub, userSub, timeutil.Now()) {
		userSub.Download, userSub.Upload = 0, 0
		columns = append(columns, "download", "upload")
	}
	userSub.ExpireTime = expireTime
	userSub.Status = usersub.SubscribeStatusActive
	userSub.FinishedAt = nil
	return store.UserSubscription().UpdateSubscribeColumns(ctx, userSub, columns...)
}

// renewalResetsTraffic reports whether paying a renewal at now clears the
// subscription's traffic counters, following the plan's reset rules:
//
//   - a plan with RenewalReset resets on every renewal;
//   - a subscription without a time limit can only buy a new allowance, so
//     its renewal resets;
//   - a subscription still in its term keeps its counters: its calendar
//     resets carry on, and a plan without one grants its quota per term;
//   - a lapsed subscription starts a new traffic period when its plan has no
//     calendar reset, and otherwise catches up on a calendar reset day that
//     passed while it was lapsed (the calendar reset skips expired
//     subscriptions), counting the day it expired.
//
// sub is the subscription before the renewal extends it.
func renewalResetsTraffic(cal period.Calendar, plan *subscribe.Subscribe, sub *usersub.Subscribe, now time.Time) bool {
	if plan.RenewalReset != nil && *plan.RenewalReset {
		return true
	}
	if usersub.NoExpiry(sub.ExpireTime) {
		return true
	}
	if !sub.ExpiredAt(now) {
		return false
	}
	cycle := period.Cycle(plan.ResetCycle)
	if cycle == period.CycleNone {
		return true
	}
	return cal.ResetBetween(cycle, sub.StartTime, sub.ExpireTime, now)
}

// termEnd is the end of a term of quantity plan units beginning at start. A
// plan with an unknown time unit fails the fulfillment instead of granting a
// term that ends where it starts.
func termEnd(plan *subscribe.Subscribe, quantity int64, start time.Time) (time.Time, error) {
	unit, err := period.ParseUnit(plan.UnitTime)
	if err != nil {
		return time.Time{}, xerr.Wrapf(err, xerr.ERROR, "plan %d", plan.Id)
	}
	return period.App().TermEnd(unit, quantity, start)
}

func (s *Service) activateResetTrafficTx(ctx context.Context, store repository.SubscriptionStore, orderInfo *order.Order) (*outcomeParts, error) {
	userSub, err := lockOrderSubscription(ctx, store, orderInfo)
	if err != nil {
		return nil, err
	}
	if userSub.UserId != orderInfo.UserId {
		return nil, fmt.Errorf("reset subscription ownership mismatch")
	}
	if userSub.EntitlementSource != "" {
		return nil, usersub.ErrProviderManaged
	}
	if usersub.OnHold(userSub.Status) {
		return nil, usersub.ErrSubscriptionOnHold
	}
	// The reset reactivates an exhausted subscription inside its term, the
	// rule every traffic reset follows.
	if err := store.UserSubscription().UpdateSubscribeColumns(ctx, userSub, userSub.ResetTraffic(timeutil.Now())...); err != nil {
		return nil, err
	}
	sub, err := store.Subscribe().FindOne(ctx, userSub.SubscribeId)
	if err != nil {
		return nil, err
	}
	resetLog := &log.ResetSubscribe{
		Type:      log.ResetSubscribeTypePaid,
		UserId:    orderInfo.UserId,
		OrderNo:   orderInfo.OrderNo,
		Timestamp: timeutil.Now().UnixMilli(),
	}
	content, err := resetLog.Marshal()
	if err != nil {
		return nil, err
	}
	if err := store.Log().Insert(ctx, &log.SystemLog{
		Type:     log.TypeResetSubscribe.Uint8(),
		Date:     timeutil.Now().Format(time.DateOnly),
		ObjectID: userSub.Id,
		Content:  string(content),
	}); err != nil {
		return nil, err
	}
	return &outcomeParts{order: orderInfo, subscribe: sub, userSub: userSub, notifyType: NotifyResetTraffic}, nil
}

// Store is the persistence capability required by this package. It excludes
// unrelated repositories and application-wide transactions.
type Store interface {
	repository.SubscriptionTransactor
	Inbox() repository.InboxRepo
	Entitlement() repository.EntitlementRepo
}
