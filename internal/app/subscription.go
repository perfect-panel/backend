package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/server/internal/infra/mail"
	"github.com/perfect-panel/server/internal/infra/taskqueue"
	"github.com/perfect-panel/server/internal/module/billing"
	"github.com/perfect-panel/server/internal/module/billing/entity/order"
	"github.com/perfect-panel/server/internal/module/identity/entity/user"
	"github.com/perfect-panel/server/internal/module/notification"
	"github.com/perfect-panel/server/internal/module/subscription"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/transport/devicesocket"
	"github.com/perfect-panel/server/pkg/logger"
)

// newSubscriptionModule wires the subscription module against the shared
// store; device broadcast and the runtime-mutable trial plan are closures
// over the application.
func newSubscriptionModule(store repository.Store, srv *Application) subscription.Service {
	return subscription.New(subscription.Deps{
		Plans:    store.Subscribe(),
		UserSubs: store.UserSubscription(),
		Nodes:    subscriptionNetworkReads{srv},
		Store:    store,
		NotifyPlanChanged: func() {
			if srv.DeviceManager != nil {
				srv.DeviceManager.Broadcast(devicesocket.SubscribeUpdate)
			}
		},
		IsTrialPlan: func(planID int64) bool {
			current := srv.Runtime.Config().Register
			return current.EnableTrial && current.TrialSubscribe == planID
		},
		Clients: store.Client(),
		Logs:    store.Log(),
		// The per-address fetch limit of subscription delivery shares the
		// application's Redis with the other rate limits.
		DeliveryLimiter: subscription.NewDeliveryLimiter(srv.Redis),
		Accounts:        subscriptionAccounts{srv: srv},
		Traffic:         subscriptionNetworkReads{srv},
		Orders:          billingOrders{billing: srv.Billing},
		Refunds:         srv.Billing,
		QuotaGifts:      srv.Billing,
		Inbox:           store.Inbox(),
		Operations:      store,
		SingleModel:     func() bool { return srv.Runtime.Config().Subscribe.SingleModel },
		TrialPolicy: func() subscription.TrialPolicy {
			c := srv.Runtime.Config().Register
			return subscription.TrialPolicy{
				Enabled:  c.EnableTrial,
				PlanID:   c.TrialSubscribe,
				Duration: c.TrialTime,
				TimeUnit: c.TrialTimeUnit,
			}
		},
		LifecycleNotify: lifecycleNotifier{srv: srv},
		DeliveryConfig: func() subscription.DeliveryConfig {
			current := srv.Runtime.Config()
			return subscription.DeliveryConfig{
				SiteName:              current.Site.SiteName,
				SiteHost:              current.Site.Host,
				SubscribeDomain:       current.Subscribe.SubscribeDomain,
				ProfileUpdateInterval: current.Subscribe.ProfileUpdateInterval,
				ProfileWebPageURL:     current.Subscribe.ProfileWebPageURL,
				UserAgentList:         current.Subscribe.UserAgentList,
			}
		},
	})
}

// billingOrders serves the subscription module's order reads from the
// billing facade. Billing is constructed before the subscription module, so
// its facade, like the refund and quota-gift ports, is bound directly.
type billingOrders struct{ billing billing.Service }

func (o billingOrders) FindOne(ctx context.Context, id int64) (*order.Order, error) {
	return o.billing.FindOrder(ctx, id)
}

func (o billingOrders) FindOneByOrderNo(ctx context.Context, orderNo string) (*order.Order, error) {
	return o.billing.FindOrderByNo(ctx, orderNo)
}

func (o billingOrders) FindOneDetails(ctx context.Context, id int64) (*order.Details, error) {
	return o.billing.FindOrderDetails(ctx, id)
}

// lifecycleNotifier adapts the subscription sweep's owner notices to their
// delivery channel: expiry and traffic notices go to the email queue, while
// the pre-expiry reminder goes over Telegram. Site branding is read per send
// because the admin can change it at runtime.
type lifecycleNotifier struct {
	srv *Application
}

func (n lifecycleNotifier) enqueue(ctx context.Context, payload taskqueue.SendEmailPayload, userEmail string) {
	body, err := json.Marshal(payload)
	if err != nil {
		logger.WithContext(ctx).Errorw("[CheckSubscription] Marshal payload failed", logger.Field("error", err.Error()))
		return
	}
	task := asynq.NewTask(taskqueue.ForthwithSendEmail, body)
	info, err := n.srv.Queue.EnqueueContext(ctx, task, asynq.MaxRetry(3))
	if err != nil {
		logger.WithContext(ctx).Errorw("[CheckSubscription] Enqueue task failed", logger.Field("error", err.Error()), logger.Field("payload", string(body)))
		return
	}
	logger.WithContext(ctx).Infow("[CheckSubscription] Send email success",
		logger.Field("taskID", info.ID), logger.Field("Email", userEmail))
}

func (n lifecycleNotifier) NotifySubscriptionExpired(ctx context.Context, email string, expiredAt time.Time) {
	current := n.srv.Runtime.Config()
	n.enqueue(ctx, taskqueue.SendEmailPayload{
		Type:    taskqueue.EmailTypeExpiration,
		Email:   email,
		Subject: mail.DefaultExpirationEmailSubject,
		Content: map[string]any{
			"SiteLogo":   current.Site.SiteLogo,
			"SiteName":   current.Site.SiteName,
			"ExpireDate": expiredAt.Format("2006-01-02 15:04:05"),
		},
	}, email)
}

func (n lifecycleNotifier) NotifyTrafficExceeded(ctx context.Context, email string) {
	current := n.srv.Runtime.Config()
	n.enqueue(ctx, taskqueue.SendEmailPayload{
		Type:    taskqueue.EmailTypeTrafficExceed,
		Email:   email,
		Subject: mail.DefaultTrafficExceedEmailSubject,
		Content: map[string]any{
			"SiteLogo": current.Site.SiteLogo,
			"SiteName": current.Site.SiteName,
		},
	}, email)
}

// NotifySubscriptionExpiring warns the owner over Telegram before the
// subscription stops. Telegram is the only channel here: the email templates
// cover expiry after the fact, and the notice is gated on the operator's
// notification switch like every other bot message.
func (n lifecycleNotifier) NotifySubscriptionExpiring(ctx context.Context, userID int64, planName string, expireAt time.Time, renewalAmount int64) {
	if !n.srv.Runtime.Config().Telegram.EnableNotify {
		return
	}
	if planName == "" {
		planName = "订阅"
	}
	text, err := notification.RenderTelegramMarkdown(notification.SubscribeExpireNotify, map[string]string{
		"SubscribeName": planName,
		"ExpiredAt":     expireAt.Format("2006-01-02 15:04:05"),
		"RenewalAmount": fmt.Sprintf("%.2f", float64(renewalAmount)/100),
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("[RemindExpiring] Render template failed", logger.Field("error", err.Error()))
		return
	}
	if err := n.srv.Notification.NotifyTelegramUser(ctx, userID, text); err != nil {
		logger.WithContext(ctx).Infow("[RemindExpiring] Telegram notice skipped",
			logger.Field("user_id", userID),
			logger.Field("reason", err.Error()),
		)
	}
}

// subscriptionAccounts backs the subscription module's identity port with
// the identity facade. It resolves the facade per call: the subscription
// module is built before identity (see NewApplication).
type subscriptionAccounts struct {
	srv *Application
}

var _ subscription.Accounts = subscriptionAccounts{}

func (a subscriptionAccounts) FindOne(ctx context.Context, id int64) (*user.User, error) {
	return a.srv.Identity.FindUser(ctx, id)
}

func (a subscriptionAccounts) FindAccountState(ctx context.Context, id int64) (*user.AccountState, error) {
	return a.srv.Identity.FindAccountState(ctx, id)
}

func (a subscriptionAccounts) FindUsersByIds(ctx context.Context, ids []int64) ([]*user.User, error) {
	return a.srv.Identity.FindUsersByIDs(ctx, ids)
}

func (a subscriptionAccounts) FindUserAuthMethodsByUserIds(ctx context.Context, method string, userIds []int64) ([]*user.AuthMethods, error) {
	return a.srv.Identity.FindAuthMethodsByUserIDs(ctx, method, userIds)
}

func (a subscriptionAccounts) QueryDevicePageList(ctx context.Context, userID, subscribeID int64, page, size int) ([]*user.Device, int64, error) {
	return a.srv.Identity.ListUserDevices(ctx, userID, subscribeID, page, size)
}

func (a subscriptionAccounts) ClearUserCache(ctx context.Context, userIDs ...int64) error {
	return a.srv.Identity.ClearUserCache(ctx, userIDs...)
}

func (a subscriptionAccounts) ClearUserCacheOf(ctx context.Context, users ...*user.User) error {
	return a.srv.Identity.ClearUserCacheOf(ctx, users...)
}
