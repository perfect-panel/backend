package activation

import (
	"context"
	"errors"
	"fmt"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/portal"
	"github.com/perfect-panel/backend/internal/module/identity"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/notification"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

type SubscriptionFulfiller interface {
	FulfillPaidOrder(context.Context, string) (*subscription.FulfillmentOutcome, error)
}

type Notifier interface {
	NotifyTelegramUser(context.Context, int64, string) error
	NotifyAdminsTelegram(context.Context, string) error
}

type LegacyGuestCache interface {
	Get(context.Context, string) *redis.StringCmd
}

// WorkflowOrders is the order persistence the workflow uses outside its
// stages: it reads the paid order and binds a guest order to its account.
type WorkflowOrders interface {
	FindOneByOrderNo(ctx context.Context, orderNo string) (*order.Order, error)
	Update(ctx context.Context, data *order.Order) error
}

type WorkflowDeps struct {
	Orders        WorkflowOrders
	Profiles      ProfileReader
	GuestAccounts identity.GuestAccounts
	// GuestIdentities is the identity port the guest account stage checks
	// the guest's mailbox against before an account is created, the same
	// port the guest purchase checks; nil skips the check.
	GuestIdentities      portal.GuestAccountReader
	Subscriptions        SubscriptionFulfiller
	LegacyGuestCache     LegacyGuestCache
	Notifications        Notifier
	NotificationsEnabled func() bool
}

// Workflow activates a paid order: it has identity create a guest buyer's
// account, credits a recharge or has subscription fulfil the order, settles
// the referral commission, finalizes the order and sends the notices. Every
// stage is idempotent, so a failed activation is retried from the start. A
// fulfillment the subscription domain refuses for good ends the order with
// a refund instead (see RefundUnfulfillable).
type Workflow struct {
	deps   WorkflowDeps
	stages *Service
}

func NewWorkflow(deps WorkflowDeps, stages *Service) *Workflow {
	return &Workflow{deps: deps, stages: stages}
}

// guestAccountTaken reports that the identity a guest order names already
// belongs to an account, so the order cannot open one: the same guest paid
// another of the pending orders the identity may hold, or the identity was
// registered between the purchase and the payment. The order is refunded to
// that account instead of waiting for an account that can never be created.
type guestAccountTaken struct {
	authType, identifier string
	userID               int64
}

func (e *guestAccountTaken) Error() string {
	return fmt.Sprintf("guest identity %s %s already belongs to user %d", e.authType, e.identifier, e.userID)
}

func (w *Workflow) ensureGuestAccount(ctx context.Context, orderInfo *order.Order) error {
	if w.deps.GuestAccounts == nil {
		return fmt.Errorf("guest account service is not configured")
	}
	userID, found, err := w.deps.GuestAccounts.FindGuestAccount(ctx, orderInfo.OrderNo)
	if err != nil {
		return err
	}
	if !found {
		guest, err := w.getGuestOrderInfo(ctx, orderInfo)
		if err != nil {
			return err
		}
		// An identity that gained an account since the purchase, under its
		// exact spelling or another spelling of the same mailbox, must not
		// get a second one: the order is refunded to that account.
		if err := w.ensureIdentityFree(ctx, guest); err != nil {
			return err
		}
		userID, err = w.deps.GuestAccounts.EnsureGuestAccount(ctx, identity.GuestAccountCommand{
			OrderNo: orderInfo.OrderNo, AuthType: guest.AuthType, Identifier: guest.Identifier,
			PasswordHash: guest.PasswordHash, LegacyPassword: guest.Password, InviteCode: guest.InviteCode,
		})
		if xerr.CodeOf(err) == xerr.UserExist {
			// Another request registered the identity between the check and
			// the creation; the account it made is the one to refund to.
			if takenErr := w.ensureIdentityFree(ctx, guest); takenErr != nil {
				return takenErr
			}
		}
		if err != nil {
			return err
		}
	}
	orderInfo.UserId = userID
	return w.deps.Orders.Update(ctx, orderInfo)
}

// ensureIdentityFree reports a guestAccountTaken error when the identity of
// the guest order already belongs to an account; without an identity reader
// the check is skipped and identity's own uniqueness rule applies.
func (w *Workflow) ensureIdentityFree(ctx context.Context, guest *order.TemporaryOrderInfo) error {
	if w.deps.GuestIdentities == nil {
		return nil
	}
	existing, err := portal.FindExistingAccount(ctx, w.deps.GuestIdentities, guest.AuthType, guest.Identifier)
	if err != nil {
		return err
	}
	if existing != 0 {
		return &guestAccountTaken{authType: guest.AuthType, identifier: guest.Identifier, userID: existing}
	}
	return nil
}

func (w *Workflow) Activate(ctx context.Context, orderNo string) error {
	orderInfo, err := w.deps.Orders.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return err
	}
	if orderInfo.Status == order.StatusFinished {
		return nil
	}
	if orderInfo.Status != order.StatusPaid {
		// A redelivery for an order the refund stage closed is complete.
		if orderInfo.Status == order.StatusClosed {
			refunded, err := w.stages.UnfulfillableRefunded(ctx, orderInfo.OrderNo)
			if err != nil {
				return err
			}
			if refunded {
				return nil
			}
		}
		return ErrInvalidOrderStatus
	}

	if orderInfo.Type == order.TypeSubscribe && orderInfo.UserId == 0 {
		if err := w.ensureGuestAccount(ctx, orderInfo); err != nil {
			var taken *guestAccountTaken
			if errors.As(err, &taken) && gateway.Collects(orderInfo.Method) {
				// The order can never open its account; the payment billing
				// collected goes back to the account the identity has.
				return w.refundToExistingAccount(ctx, orderInfo, taken)
			}
			logger.WithContext(ctx).Error("[ActivateOrderLogic] Guest account stage failed", logger.Field("error", err.Error()), logger.Field("order_no", orderInfo.OrderNo))
			return err
		}
	}

	if orderInfo.Type == order.TypeRecharge {
		balance, err := w.stages.ActivateRecharge(ctx, orderInfo.OrderNo)
		if err != nil {
			logger.WithContext(ctx).Error("[ActivateOrderLogic] Recharge stage failed", logger.Field("error", err.Error()), logger.Field("order_no", orderInfo.OrderNo))
			return err
		}
		// Load the notification context BEFORE the finalize CAS: once the
		// order is Finished a retry short-circuits, so failing here (all
		// prior stages are idempotent) keeps the notice at-least-once.
		userInfo, err := w.deps.Profiles.FindOne(ctx, orderInfo.UserId)
		if err != nil {
			logger.WithContext(ctx).Error("[ActivateOrderLogic] Load user for recharge notify failed", logger.Field("error", err.Error()))
			return err
		}
		if err := w.stages.FinalizeOrder(ctx, orderInfo.OrderNo); err != nil {
			logger.WithContext(ctx).Error("[ActivateOrderLogic] Finalize stage failed", logger.Field("error", err.Error()), logger.Field("order_no", orderInfo.OrderNo))
			return err
		}
		w.sendRechargeNotifications(ctx, orderInfo, userInfo, balance)
		return nil
	}

	outcome, err := w.deps.Subscriptions.FulfillPaidOrder(ctx, orderInfo.OrderNo)
	if err != nil {
		if unfulfillable(orderInfo, err) {
			return w.refundUnfulfillable(ctx, orderInfo, err)
		}
		logger.WithContext(ctx).Error("[ActivateOrderLogic] Fulfillment stage failed", logger.Field("error", err.Error()), logger.Field("order_no", orderInfo.OrderNo))
		return err
	}

	if orderInfo.Type == order.TypeSubscribe || orderInfo.Type == order.TypeRenewal {
		if err := w.stages.SettleOrderCommission(ctx, orderInfo.OrderNo, outcome.UserID); err != nil {
			logger.WithContext(ctx).Error("[ActivateOrderLogic] Commission stage failed", logger.Field("error", err.Error()), logger.Field("order_no", orderInfo.OrderNo))
			return err
		}
	}

	// Load the notification context BEFORE the finalize CAS (see the
	// recharge branch above for why).
	userInfo, err := w.deps.Profiles.FindOne(ctx, orderInfo.UserId)
	if err != nil {
		logger.WithContext(ctx).Error("[ActivateOrderLogic] Load user for notify failed", logger.Field("error", err.Error()))
		return err
	}

	if err := w.stages.FinalizeOrder(ctx, orderInfo.OrderNo); err != nil {
		logger.WithContext(ctx).Error("[ActivateOrderLogic] Finalize stage failed", logger.Field("error", err.Error()), logger.Field("order_no", orderInfo.OrderNo))
		return err
	}

	w.notifyFulfillment(ctx, orderInfo, userInfo, outcome)
	return nil
}

// unfulfillable reports a fulfillment refusal no retry can change: the
// order's subscription was refunded or stopped, or is managed by a payment
// provider now, while the payment was collected by billing itself. An order
// a provider collected, such as an app store purchase that reports
// ErrProviderManaged, is the provider's to settle and is never refunded here.
func unfulfillable(orderInfo *order.Order, err error) bool {
	if !errors.Is(err, usersub.ErrSubscriptionOnHold) && !errors.Is(err, usersub.ErrProviderManaged) {
		return false
	}
	return orderInfo.UserId != 0 && gateway.Collects(orderInfo.Method)
}

// refundToExistingAccount ends a paid guest order whose identity already
// has an account: the payment goes to that account's wallet and the order
// closes bound to it, which completes the activation instead of leaving a
// paid order that can never create its account.
func (w *Workflow) refundToExistingAccount(ctx context.Context, orderInfo *order.Order, taken *guestAccountTaken) error {
	if err := w.stages.RefundUnfulfillableToAccount(ctx, orderInfo.OrderNo, taken.userID); err != nil {
		logger.WithContext(ctx).Error("[ActivateOrderLogic] Refund of a guest order to the identity's existing account failed",
			logger.Field("error", err.Error()), logger.Field("order_no", orderInfo.OrderNo), logger.Field("user_id", taken.userID))
		return err
	}
	logger.WithContext(ctx).Infow("[ActivateOrderLogic] Paid guest order names an identity that already has an account; refunded to that account's wallet",
		logger.Field("order_no", orderInfo.OrderNo), logger.Field("user_id", taken.userID),
		logger.Field("amount", orderInfo.Amount), logger.Field("gift_amount", orderInfo.GiftAmount),
		logger.Field("reason", taken.Error()))
	return nil
}

// refundUnfulfillable ends a paid order the subscription domain cannot
// fulfil: the buyer gets the payment back and the order closes, which
// completes the activation instead of failing it forever.
func (w *Workflow) refundUnfulfillable(ctx context.Context, orderInfo *order.Order, cause error) error {
	if err := w.stages.RefundUnfulfillable(ctx, orderInfo.OrderNo); err != nil {
		logger.WithContext(ctx).Error("[ActivateOrderLogic] Refund of unfulfillable order failed", logger.Field("error", err.Error()), logger.Field("order_no", orderInfo.OrderNo))
		return err
	}
	logger.WithContext(ctx).Infow("[ActivateOrderLogic] Paid order could not be fulfilled and was refunded to the buyer's wallet",
		logger.Field("order_no", orderInfo.OrderNo), logger.Field("user_id", orderInfo.UserId),
		logger.Field("amount", orderInfo.Amount), logger.Field("gift_amount", orderInfo.GiftAmount),
		logger.Field("reason", cause.Error()))
	return nil
}

// notifyFulfillment dispatches the post-activation notices using the
// fulfillment outcome's notification context.
func (w *Workflow) notifyFulfillment(ctx context.Context, orderInfo *order.Order, userInfo *user.User, outcome *subscription.FulfillmentOutcome) {
	if outcome == nil {
		return
	}
	notifyType := ""
	switch outcome.NotifyKind {
	case subscription.NotifyKindPurchase:
		notifyType = notification.PurchaseNotify
	case subscription.NotifyKindRenewal:
		notifyType = notification.RenewalNotify
	case subscription.NotifyKindResetTraffic:
		notifyType = notification.ResetTrafficNotify
	default:
		return
	}
	w.sendNotifications(ctx, orderInfo, userInfo, outcome, notifyType)
}

// getTempOrderInfo retrieves temporary order information from Redis cache
func (w *Workflow) getTempOrderInfo(ctx context.Context, orderNo string) (*order.TemporaryOrderInfo, error) {
	cacheKey := fmt.Sprintf(order.TempOrderCacheKey, orderNo)
	data, err := w.deps.LegacyGuestCache.Get(ctx, cacheKey).Result()
	if err != nil {
		logger.WithContext(ctx).Error("Get temp order cache failed",
			logger.Field("error", err.Error()),
			logger.Field("cache_key", cacheKey),
		)
		return nil, err
	}

	var tempOrder order.TemporaryOrderInfo
	if err = tempOrder.Unmarshal([]byte(data)); err != nil {
		logger.WithContext(ctx).Error("Unmarshal temp order cache failed",
			logger.Field("error", err.Error()),
			logger.Field("cache_key", cacheKey),
		)
		return nil, err
	}

	return &tempOrder, nil
}

func (w *Workflow) getGuestOrderInfo(ctx context.Context, orderInfo *order.Order) (*order.TemporaryOrderInfo, error) {
	if orderInfo.GuestAuthType != "" && orderInfo.GuestIdentifier != "" && orderInfo.GuestPasswordHash != "" {
		return &order.TemporaryOrderInfo{
			OrderNo:      orderInfo.OrderNo,
			Identifier:   orderInfo.GuestIdentifier,
			AuthType:     orderInfo.GuestAuthType,
			PasswordHash: orderInfo.GuestPasswordHash,
			InviteCode:   orderInfo.GuestInviteCode,
		}, nil
	}
	return w.getTempOrderInfo(ctx, orderInfo.OrderNo)
}

// sendNotifications sends both user and admin notifications for order completion
func (w *Workflow) sendNotifications(ctx context.Context, orderInfo *order.Order, userInfo *user.User, outcome *subscription.FulfillmentOutcome, notifyType string) {
	// Send user notification
	templateData := w.buildUserNotificationData(orderInfo, outcome)
	if text, err := notification.RenderTelegramMarkdown(notifyType, templateData); err == nil {
		w.sendUserNotifyWithTelegram(ctx, userInfo.Id, text)
	}

	// Send admin notification
	adminData := w.buildAdminNotificationData(orderInfo, userInfo, outcome)
	if text, err := notification.RenderTelegramMarkdown(notification.AdminOrderNotify, adminData); err == nil {
		w.sendAdminNotifyWithTelegram(ctx, text)
	}
}

// sendRechargeNotifications sends specific notifications for balance recharge orders
func (w *Workflow) sendRechargeNotifications(ctx context.Context, orderInfo *order.Order, userInfo *user.User, balance int64) {
	// Send user notification
	templateData := map[string]string{
		"OrderAmount":   payment.FormatAmount(orderInfo.Price),
		"PaymentMethod": orderInfo.Method,
		"Time":          orderInfo.CreatedAt.Format(noticeTimeLayout),
		"Balance":       payment.FormatAmount(balance),
	}
	if text, err := notification.RenderTelegramMarkdown(notification.RechargeNotify, templateData); err == nil {
		w.sendUserNotifyWithTelegram(ctx, userInfo.Id, text)
	}

	// Send admin notification
	adminData := map[string]string{
		"OrderNo":       orderInfo.OrderNo,
		"TradeNo":       orderInfo.TradeNo,
		"UserEmail":     findEmail(userInfo),
		"OrderAmount":   payment.FormatAmount(orderInfo.Price),
		"SubscribeName": noticeRechargeName,
		"OrderStatus":   noticeStatusPaid,
		"OrderTime":     orderInfo.CreatedAt.Format(noticeTimeLayout),
		"PaymentMethod": orderInfo.Method,
	}
	if text, err := notification.RenderTelegramMarkdown(notification.AdminOrderNotify, adminData); err == nil {
		w.sendAdminNotifyWithTelegram(ctx, text)
	}
}

// buildUserNotificationData creates template data for user notifications
func (w *Workflow) buildUserNotificationData(orderInfo *order.Order, outcome *subscription.FulfillmentOutcome) map[string]string {
	data := map[string]string{
		"OrderNo":       orderInfo.OrderNo,
		"SubscribeName": outcome.PlanName,
		"OrderAmount":   payment.FormatAmount(orderInfo.Price),
	}

	if outcome.HasSub {
		data["ExpireTime"] = outcome.ExpireAt.Format(noticeTimeLayout)
		data["ResetTime"] = timeutil.Now().Format(noticeTimeLayout)
	}

	return data
}

// buildAdminNotificationData creates template data for admin notifications
func (w *Workflow) buildAdminNotificationData(orderInfo *order.Order, userInfo *user.User, outcome *subscription.FulfillmentOutcome) map[string]string {
	subscribeName := outcome.PlanName
	if orderInfo.Type == order.TypeResetTraffic {
		subscribeName = noticeResetTrafficName
	}

	return map[string]string{
		"OrderNo":       orderInfo.OrderNo,
		"TradeNo":       orderInfo.TradeNo,
		"UserEmail":     findEmail(userInfo),
		"SubscribeName": subscribeName,
		"OrderAmount":   payment.FormatAmount(orderInfo.Price),
		"OrderStatus":   noticeStatusPaid,
		"OrderTime":     orderInfo.CreatedAt.Format(noticeTimeLayout),
		"PaymentMethod": orderInfo.Method,
	}
}

// sendUserNotifyWithTelegram delivers rendered MarkdownV2 to the buyer's
// bound Telegram; "no binding" and "no bot" both just mean nothing to send.
func (w *Workflow) sendUserNotifyWithTelegram(ctx context.Context, userID int64, text string) {
	if w.deps.NotificationsEnabled == nil || !w.deps.NotificationsEnabled() {
		return
	}
	if err := w.deps.Notifications.NotifyTelegramUser(ctx, userID, text); err != nil {
		logger.WithContext(ctx).Info("Telegram user notice skipped",
			logger.Field("reason", err.Error()), logger.Field("user_id", userID))
	}
}

// sendAdminNotifyWithTelegram posts into the admin group's notification
// topic - the group is the only administrator channel, so an unconfigured
// group means the notice is skipped.
func (w *Workflow) sendAdminNotifyWithTelegram(ctx context.Context, text string) {
	if w.deps.NotificationsEnabled == nil || !w.deps.NotificationsEnabled() {
		return
	}
	if err := w.deps.Notifications.NotifyAdminsTelegram(ctx, text); err != nil {
		logger.WithContext(ctx).Info("Telegram admin notice skipped", logger.Field("reason", err.Error()))
	}
}

// findEmail returns the user's email auth identifier, falling back to the
// numeric id so the admin notification always names the buyer.
func findEmail(u *user.User) string {
	for _, item := range u.AuthMethods {
		if item.AuthType == "email" {
			return item.AuthIdentifier
		}
	}
	return fmt.Sprintf("ID:%d", u.Id)
}
