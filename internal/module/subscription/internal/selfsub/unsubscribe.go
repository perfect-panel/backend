package selfsub

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Inbox consumer of the cancellation stage (ADR-001 step 2), keyed by
// user-subscription id; the refund stage's marker is the billing module's.
// The cancellation marker carries "orderID|remaining" so a replay can settle
// the refund without recomputing it.
const unsubscribeCancelConsumer = "subscription.unsubscribe_cancel"

// errNotCancelable rejects cancelling a subscription that ended, was
// refunded or is stopped (usersub.CurrentStatuses may be cancelled).
var errNotCancelable = errors.New("subscription status invalid for cancellation")

// Unsubscribe cancels the subscription in a subscription-domain transaction,
// then has the billing module settle the refund in a billing-domain
// transaction (gift amount first for balance-paid orders, then regular
// balance). A crash between the two is repaired when the user retries: a
// Deducted subscription whose refund was not settled resumes at the refund
// stage.
func (s *Service) Unsubscribe(ctx context.Context, req *dto.UnsubscribeRequest) error {
	lg := logger.WithContext(ctx)
	u, ok := user.FromContext(ctx)
	if !ok {
		lg.Error("current user is not found in context")
		return xerr.NewErrCode(xerr.InvalidAccess)
	}
	userSub, err := s.deps.UserSubs.FindOneSubscribe(ctx, req.Id)
	if err != nil {
		lg.Errorw("[Unsubscribe] Find subscription failed", logger.Field("error", err.Error()), logger.Field("user_subscribe_id", req.Id))
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find subscription %d", req.Id)
	}
	if userSub.UserId != u.Id {
		lg.Errorw("[Unsubscribe] Subscription belongs to another user", logger.Field("user_subscribe_id", req.Id), logger.Field("user_id", u.Id))
		return errNotOwner
	}
	if userSub.EntitlementSource != "" {
		return usersub.ErrProviderManaged
	}

	subKey := strconv.FormatInt(req.Id, 10)
	if usersub.CurrentStatuses.Contains(userSub.Status) {
		if err := s.cancel(ctx, u.Id, req.Id, subKey); err != nil {
			lg.Errorw("[Unsubscribe] Cancel subscription failed", logger.Field("error", err.Error()), logger.Field("user_subscribe_id", req.Id))
			return err
		}
	} else {
		resumable, err := s.hasUnsettledRefund(ctx, userSub.Status, req.Id, subKey)
		if err != nil {
			return err
		}
		if !resumable {
			lg.Errorw("[Unsubscribe] Subscription status invalid for cancellation", logger.Field("user_subscribe_id", req.Id), logger.Field("status", userSub.Status))
			return xerr.Wrapf(errNotCancelable, xerr.ERROR, "subscription %d has status %d", userSub.Id, userSub.Status)
		}
	}

	// Billing-domain stage: settle the refund exactly once.
	if err := s.settleRefundOnce(ctx, u.Id, req.Id, subKey); err != nil {
		lg.Errorw("[Unsubscribe] Settle refund failed", logger.Field("error", err.Error()), logger.Field("user_subscribe_id", req.Id))
		return xerr.Wrapf(err, xerr.ERROR, "settle refund of subscription %d", req.Id)
	}
	if err := s.deps.Cache.ClearSubscribeCache(ctx, userSub); err != nil {
		lg.Errorw("[Unsubscribe] Clear subscription cache failed", logger.Field("error", err.Error()), logger.Field("user_subscribe_id", req.Id))
		return xerr.Wrapf(err, xerr.ERROR, "clear subscription cache")
	}
	if err := s.deps.Plans.ClearCache(ctx, userSub.SubscribeId); err != nil {
		lg.Errorw("[Unsubscribe] Clear plan cache failed", logger.Field("error", err.Error()), logger.Field("subscribe_id", userSub.SubscribeId))
		return xerr.Wrapf(err, xerr.ERROR, "clear plan cache")
	}
	return nil
}

// cancel flips the subscription to Deducted and durably records what the
// billing stage owes, in one subscription-domain transaction.
func (s *Service) cancel(ctx context.Context, userID, subID int64, subKey string) error {
	// The refund is the unused share of the subscription's time and traffic.
	remaining, err := s.remainingAmount(ctx, subID)
	if err != nil {
		return err
	}
	err = s.deps.Store.InSubscriptionTx(ctx, func(store repository.SubscriptionStore) error {
		// Re-read the subscription under a row lock. The context user is
		// only an authorization principal and can be stale.
		locked, err := store.UserSubscription().FindOneSubscribeForUpdate(ctx, subID)
		if err != nil {
			return err
		}
		if locked.UserId != userID {
			return errNotOwner
		}
		if locked.EntitlementSource != "" {
			return usersub.ErrProviderManaged
		}
		if !usersub.CurrentStatuses.Contains(locked.Status) {
			return xerr.Wrapf(errNotCancelable, xerr.ERROR, "subscription %d has status %d", locked.Id, locked.Status)
		}
		locked.Status = usersub.SubscribeStatusDeducted
		if err := store.UserSubscription().UpdateSubscribeColumns(ctx, locked, "status"); err != nil {
			return err
		}
		return store.Inbox().Insert(ctx, unsubscribeCancelConsumer, subKey, fmt.Sprintf("%d|%d", locked.OrderId, remaining))
	})
	return xerr.Wrapf(err, xerr.ERROR, "cancel subscription %d", subID)
}

// hasUnsettledRefund reports whether a non-cancelable subscription is a
// Deducted one whose cancellation committed but whose refund never did.
func (s *Service) hasUnsettledRefund(ctx context.Context, status uint8, subID int64, subKey string) (bool, error) {
	if status != usersub.SubscribeStatusDeducted {
		return false, nil
	}
	cancelled, err := s.deps.Inbox.Find(ctx, unsubscribeCancelConsumer, subKey)
	if err != nil {
		return false, xerr.Wrapf(err, xerr.DatabaseQueryError, "find cancellation marker")
	}
	if cancelled == nil {
		return false, nil
	}
	refunded, err := s.deps.Refunds.UnsubscribeRefundSettled(ctx, subID)
	if err != nil {
		return false, xerr.Wrapf(err, xerr.DatabaseQueryError, "find refund marker")
	}
	return !refunded, nil
}

// settleRefundOnce has the billing module credit the refund recorded by the
// cancellation marker in a billing-domain transaction, guarded by the refund
// marker.
func (s *Service) settleRefundOnce(ctx context.Context, userID, subID int64, subKey string) error {
	cancelled, err := s.deps.Inbox.Find(ctx, unsubscribeCancelConsumer, subKey)
	if err != nil {
		return err
	}
	if cancelled == nil {
		return fmt.Errorf("cancellation marker missing for subscription %s", subKey)
	}
	refunded, err := s.deps.Refunds.UnsubscribeRefundSettled(ctx, subID)
	if err != nil {
		return err
	}
	if refunded {
		return nil
	}
	orderID, remainingAmount, err := parseCancellationMarker(cancelled.Result)
	if err != nil {
		return err
	}
	return s.deps.Refunds.SettleUnsubscribeRefund(ctx, userID, subID, orderID, remainingAmount)
}

func parseCancellationMarker(result string) (orderID, remainingAmount int64, err error) {
	parts := strings.SplitN(result, "|", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("corrupt cancellation marker %q", result)
	}
	if orderID, err = strconv.ParseInt(parts[0], 10, 64); err != nil {
		return 0, 0, fmt.Errorf("corrupt cancellation marker %q: %w", result, err)
	}
	if remainingAmount, err = strconv.ParseInt(parts[1], 10, 64); err != nil {
		return 0, 0, fmt.Errorf("corrupt cancellation marker %q: %w", result, err)
	}
	return orderID, remainingAmount, nil
}
