package svc

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/module/billing"
	"github.com/perfect-panel/server/internal/module/identity"
	"github.com/perfect-panel/server/internal/module/platform"
	"github.com/perfect-panel/server/internal/module/subscription"
	"github.com/perfect-panel/server/internal/module/support"
	"github.com/perfect-panel/server/internal/report"
	"github.com/perfect-panel/server/internal/repository"
	emailworker "github.com/perfect-panel/server/internal/worker/email"
	"github.com/perfect-panel/server/pkg/device"
	"github.com/perfect-panel/server/pkg/exchangeRate"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	queuetypes "github.com/perfect-panel/server/queue/types"
	"github.com/redis/go-redis/v9"
)

// newBillingModule wires the billing module against the legacy store and the
// asynq client (ADR-001 step 4).
func newBillingModule(c config.Config, store repository.Store, queue *asynq.Client, rds *redis.Client, rate *exchangeRate.Cache) billing.Service {
	return billing.New(billing.Deps{
		Orders:        store.Order(),
		Payments:      store.Payment(),
		Coupons:       store.Coupon(),
		Plans:         store.Subscribe(),
		UserSubs:      store.UserSubscription(),
		Store:         store,
		Tx:            store,
		Queue:         activationQueue{client: queue},
		SingleModel:   c.Subscribe.SingleModel,
		CurrencyUnit:  c.Currency.Unit,
		Host:          c.Host,
		IsGatewayMode: report.IsGatewayMode,

		Logs:        store.Log(),
		UserCache:   store.UserCache(),
		Affiliates:  store.User(),
		AuthMethods: store.UserAuth(),

		PortalPlans:        store.Subscribe(),
		GuestAccounts:      store.UserAuth(),
		Sessions:           rds,
		GuestCheckoutCache: rds,
		ActivationQueue:    queue,
		ExchangeRate:       rate,
		Portal: billing.PortalConfig{
			Host:              c.Host,
			SiteName:          c.Site.SiteName,
			CurrencyUnit:      c.Currency.Unit,
			CurrencyAccessKey: c.Currency.AccessKey,
			JwtSecret:         c.JwtAuth.AccessSecret,
			JwtExpire:         c.JwtAuth.AccessExpire,
			IsGatewayMode:     report.IsGatewayMode,
		},
	})
}

// activationQueue adapts the asynq client to the billing module's activation
// port. A task-id conflict means a delivery already exists for the order,
// which is success, not an error.
type activationQueue struct {
	client *asynq.Client
}

func (q activationQueue) EnqueueActivation(ctx context.Context, orderNo string) error {
	payload, err := json.Marshal(queuetypes.ForthwithActivateOrderPayload{OrderNo: orderNo})
	if err != nil {
		return err
	}
	task := asynq.NewTask(queuetypes.ForthwithActivateOrder, payload)
	_, err = q.client.EnqueueContext(ctx, task, asynq.TaskID(queuetypes.ActivationTaskID(orderNo)))
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}

// EnqueueDeferredClose schedules the pending order's expiry close after the
// payment window elapses.
func (q activationQueue) EnqueueDeferredClose(ctx context.Context, orderNo string) error {
	payload, err := json.Marshal(queuetypes.DeferCloseOrderPayload{OrderNo: orderNo})
	if err != nil {
		return err
	}
	task := asynq.NewTask(queuetypes.DeferCloseOrder, payload, asynq.MaxRetry(3))
	_, err = q.client.EnqueueContext(ctx, task, asynq.ProcessIn(billing.CloseOrderTimeMinutes*time.Minute))
	return err
}

// newPlatformModule wires the platform module against the legacy store. The
// log-retention callbacks read and mutate the running configuration exactly
// as the legacy logic did.
func newPlatformModule(store repository.Store, srv *ServiceContext) platform.Service {
	return platform.New(platform.Deps{
		Logs:    store.Log(),
		System:  store.System(),
		Traffic: store.TrafficLog(),
		Store:   store,
		Orders:  store.Order(),
		Users:   store.User(),
		Tickets: store.Ticket(),
		Nodes:   store.Node(),
		Cache:   srv.Redis,
		OnLogSettingChanged: func(autoClear bool, clearDays int64) {
			srv.Config.Log = config.Log{AutoClear: autoClear, ClearDays: clearDays}
		},
		LogRetention: func() (bool, int64) {
			return srv.Config.Log.AutoClear, srv.Config.Log.ClearDays
		},
		Reinitialize: func(subsystem string) {
			if srv.ReinitSubsystem != nil {
				srv.ReinitSubsystem(subsystem)
			}
		},
		Restart: func() error {
			if srv.Restart == nil {
				return nil
			}
			return srv.Restart()
		},
		SubscribePath: func() string { return srv.Config.Subscribe.SubscribePath },
		ApplyVerifyConfig: func(req *dto.VerifyConfig) {
			tool.DeepCopy(&srv.Config.Verify, req)
		},
		Multiplier: func(at time.Time) float32 {
			return srv.NodeMultiplierManager.GetMultiplier(at)
		},
	})
}

// newSubscriptionModule wires the subscription module against the legacy
// store; device broadcast and the runtime-mutable trial plan are closures
// over the service context.
func newSubscriptionModule(store repository.Store, srv *ServiceContext) subscription.Service {
	return subscription.New(subscription.Deps{
		Plans:    store.Subscribe(),
		UserSubs: store.UserSubscription(),
		Nodes:    store.Node(),
		Store:    store,
		NotifyPlanChanged: func() {
			if srv.DeviceManager != nil {
				srv.DeviceManager.Broadcast(device.SubscribeUpdate)
			}
		},
		Host: srv.Config.Host,
		IsTrialPlan: func(planID int64) bool {
			return srv.Config.Register.EnableTrial && srv.Config.Register.TrialSubscribe == planID
		},
		Clients:     store.Client(),
		Users:       store.User(),
		Logs:        store.Log(),
		Devices:     store.UserDevice(),
		Cache:       store.UserCache(),
		Traffic:     store.TrafficLog(),
		Orders:      store.Order(),
		Inbox:       store.Inbox(),
		FullStore:   store,
		SingleModel: srv.Config.Subscribe.SingleModel,
		DeliveryConfig: func() subscription.DeliveryConfig {
			return subscription.DeliveryConfig{
				SiteName:              srv.Config.Site.SiteName,
				Host:                  srv.Config.Host,
				SubscribeDomain:       srv.Config.Subscribe.SubscribeDomain,
				ProfileUpdateInterval: srv.Config.Subscribe.ProfileUpdateInterval,
				ProfileWebPageURL:     srv.Config.Subscribe.ProfileWebPageURL,
				UserAgentList:         srv.Config.Subscribe.UserAgentList,
				GatewayMode:           report.IsGatewayMode(),
			}
		},
	})
}

// newIdentityModule wires the identity module against the legacy store;
// device kicking is a closure over the service context's device manager.
func newIdentityModule(store repository.Store, srv *ServiceContext) identity.Service {
	return identity.New(identity.Deps{
		Users:     store.User(),
		UserAuths: store.UserAuth(),
		Devices:   store.UserDevice(),
		Cache:     store.UserCache(),
		UserSubs:  store.UserSubscription(),
		Plans:     store.Subscribe(),
		Traffic:   store.TrafficLog(),
		Logs:      store.Log(),
		Store:     store,
		KickDevice: func(userID int64, identifier string) {
			if srv.DeviceManager != nil {
				srv.DeviceManager.KickDevice(userID, identifier)
			}
		},
	})
}

// newSupportModule wires the support module against the legacy store. The
// adapters below satisfy the module's ports until the owning modules exist
// (ADR-001).
func newSupportModule(store repository.Store, queue *asynq.Client) support.Service {
	return support.New(support.Deps{
		Announcements: store.Announcement(),
		Ads:           store.Ads(),
		Documents:     store.Document(),
		Tickets:       store.Ticket(),
		Tasks:         store.Task(),
		Subscriptions: subscriptionReader{store: store},
		Recipients:    store.User(),
		QuotaTargets:  store.UserSubscription(),
		Queue:         marketingQueue{client: queue},
		EmailStopper:  emailWorkerStopper{},
	})
}

// marketingQueue adapts the asynq client to the support module's
// MarketingQueue port, keeping queue task types out of the module.
type marketingQueue struct {
	client *asynq.Client
}

func (q marketingQueue) EnqueueBatchEmail(ctx context.Context, taskID int64, processAt time.Time) (string, error) {
	t := asynq.NewTask(queuetypes.ScheduledBatchSendEmail, []byte(strconv.FormatInt(taskID, 10)))
	info, err := q.client.EnqueueContext(ctx, t, asynq.ProcessAt(processAt))
	if err != nil {
		return "", err
	}
	return info.ID, nil
}

func (q marketingQueue) EnqueueQuota(ctx context.Context, taskID int64) error {
	t := asynq.NewTask(queuetypes.ForthwithQuotaTask, []byte(strconv.FormatInt(taskID, 10)))
	_, err := q.client.EnqueueContext(ctx, t)
	return err
}

// emailWorkerStopper adapts the global batch-email worker manager to the
// support module's BatchEmailStopper port.
type emailWorkerStopper struct{}

func (emailWorkerStopper) StopBatchEmail(taskID int64) {
	if emailworker.Manager == nil {
		logger.Error("[StopBatchSendEmailTaskLogic] email worker manager is nil, cannot stop task")
		return
	}
	emailworker.Manager.RemoveWorker(taskID)
}

// subscriptionReader adapts the legacy user-subscription repository to the
// support module's SubscriptionReader port.
type subscriptionReader struct {
	store repository.Store
}

func (r subscriptionReader) HasActiveSubscription(ctx context.Context, userID int64) (bool, error) {
	// status 1 = active
	subs, err := r.store.UserSubscription().QueryUserSubscribe(ctx, userID, 1)
	if err != nil {
		return false, err
	}
	return len(subs) > 0, nil
}
