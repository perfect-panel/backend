package svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	tgbot "github.com/go-telegram/bot"
	"strconv"
	"time"

	"github.com/hibiken/asynq"
	"github.com/oschwald/geoip2-golang"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/eventbus"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/module/billing"
	"github.com/perfect-panel/server/internal/module/identity"
	"github.com/perfect-panel/server/internal/module/network"
	"github.com/perfect-panel/server/internal/module/notification"
	"github.com/perfect-panel/server/internal/module/platform"
	"github.com/perfect-panel/server/internal/module/subscription"
	"github.com/perfect-panel/server/internal/module/support"
	ticket "github.com/perfect-panel/server/internal/module/support/entity/ticket"
	"github.com/perfect-panel/server/internal/report"
	"github.com/perfect-panel/server/internal/repository"
	emailworker "github.com/perfect-panel/server/internal/worker/email"
	"github.com/perfect-panel/server/pkg/asynqx"
	"github.com/perfect-panel/server/pkg/device"
	emailpkg "github.com/perfect-panel/server/pkg/email"
	"github.com/perfect-panel/server/pkg/exchangeRate"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	queuetypes "github.com/perfect-panel/server/queue/types"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// newBillingModule wires the billing module against the legacy store and the
// asynq client (ADR-001 step 4).
func newBillingModule(c config.Config, store repository.Store, queue *asynqx.Client, rds *redis.Client, rate *exchangeRate.Cache, srv *ServiceContext) billing.Service {
	return billing.New(billing.Deps{
		Orders:        store.Order(),
		Payments:      store.Payment(),
		Coupons:       store.Coupon(),
		Plans:         store.Subscribe(),
		UserSubs:      store.UserSubscription(),
		Store:         store,
		Tx:            store,
		Queue:         activationQueue{client: queue},
		SingleModel:   func() bool { return srv.Config.Subscribe.SingleModel },
		CurrencyUnit:  func() string { return srv.Config.Currency.Unit },
		Host:          c.Host,
		IsGatewayMode: report.IsGatewayMode,

		Logs:        store.Log(),
		UserCache:   store.UserCache(),
		Affiliates:  store.User(),
		AuthMethods: store.UserAuth(),

		UserProfiles: store.User(),
		InvitePolicy: func() (uint8, bool) {
			return uint8(srv.Config.Invite.ReferralPercentage), srv.Config.Invite.OnlyFirstPurchase
		},

		PortalPlans:        store.Subscribe(),
		GuestAccounts:      store.UserAuth(),
		Sessions:           rds,
		GuestCheckoutCache: rds,
		ActivationQueue:    queue,
		ExchangeRate:       rate,
		Portal: billing.PortalConfig{
			Host:              c.Host,
			SiteName:          func() string { return srv.Config.Site.SiteName },
			CurrencyUnit:      func() string { return srv.Config.Currency.Unit },
			CurrencyAccessKey: func() string { return srv.Config.Currency.AccessKey },
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
	client *asynqx.Client
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
	task := asynq.NewTask(queuetypes.DeferCloseOrder, payload)
	_, err = q.client.EnqueueContext(ctx, task, asynq.MaxRetry(3), asynq.ProcessIn(billing.CloseOrderTimeMinutes*time.Minute))
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
		FullStore: store,
		Redis:     srv.Redis,
		PublicConfig: func() platform.GlobalConfigSnapshot {
			c := srv.Config
			return platform.GlobalConfigSnapshot{
				Site:      c.Site,
				Subscribe: c.Subscribe,
				Email:     c.Email,
				Mobile:    c.Mobile,
				Register:  c.Register,
				Verify:    c.Verify,
				Invite:    c.Invite,
			}
		},
		LogPath: srv.Config.Logger.Path,
		GeoIP: func() *geoip2.Reader {
			if srv.GeoIP == nil {
				return nil
			}
			return srv.GeoIP.DB
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
		SingleModel: func() bool { return srv.Config.Subscribe.SingleModel },
		TrialPolicy: func() subscription.TrialPolicy {
			c := srv.Config.Register
			return subscription.TrialPolicy{
				Enabled:  c.EnableTrial,
				PlanID:   c.TrialSubscribe,
				Duration: c.TrialTime,
				TimeUnit: c.TrialTimeUnit,
			}
		},
		UserAuths:       store.UserAuth(),
		LifecycleNotify: lifecycleNotifier{srv: srv},
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

// lifecycleNotifier adapts the subscription sweep's owner notices to their
// delivery channel: expiry and traffic notices go to the email queue, while
// the pre-expiry reminder goes over Telegram. Site branding is read per send
// because the admin can change it at runtime.
type lifecycleNotifier struct {
	srv *ServiceContext
}

func (n lifecycleNotifier) enqueue(ctx context.Context, payload queuetypes.SendEmailPayload, userEmail string) {
	body, err := json.Marshal(payload)
	if err != nil {
		logger.Errorw("[CheckSubscription] Marshal payload failed", logger.Field("error", err.Error()))
		return
	}
	task := asynq.NewTask(queuetypes.ForthwithSendEmail, body)
	info, err := n.srv.Queue.EnqueueContext(ctx, task, asynq.MaxRetry(3))
	if err != nil {
		logger.Errorw("[CheckSubscription] Enqueue task failed", logger.Field("error", err.Error()), logger.Field("payload", string(body)))
		return
	}
	logger.Infow("[CheckSubscription] Send email success",
		logger.Field("taskID", info.ID), logger.Field("Email", userEmail))
}

func (n lifecycleNotifier) NotifySubscriptionExpired(ctx context.Context, email string, expiredAt time.Time) {
	n.enqueue(ctx, queuetypes.SendEmailPayload{
		Type:    queuetypes.EmailTypeExpiration,
		Email:   email,
		Subject: emailpkg.DefaultExpirationEmailSubject,
		Content: map[string]interface{}{
			"SiteLogo":   n.srv.Config.Site.SiteLogo,
			"SiteName":   n.srv.Config.Site.SiteName,
			"ExpireDate": expiredAt.Format("2006-01-02 15:04:05"),
		},
	}, email)
}

func (n lifecycleNotifier) NotifyTrafficExceeded(ctx context.Context, email string) {
	n.enqueue(ctx, queuetypes.SendEmailPayload{
		Type:    queuetypes.EmailTypeTrafficExceed,
		Email:   email,
		Subject: emailpkg.DefaultTrafficExceedEmailSubject,
		Content: map[string]interface{}{
			"SiteLogo": n.srv.Config.Site.SiteLogo,
			"SiteName": n.srv.Config.Site.SiteName,
		},
	}, email)
}

// NotifySubscriptionExpiring warns the owner over Telegram before the
// subscription stops. Telegram is the only channel here: the email templates
// cover expiry after the fact, and the notice is gated on the operator's
// notification switch like every other bot message.
func (n lifecycleNotifier) NotifySubscriptionExpiring(ctx context.Context, userID int64, planName string, expireAt time.Time, renewalAmount int64) {
	if !n.srv.Config.Telegram.EnableNotify {
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
		logger.Errorw("[RemindExpiring] Render template failed", logger.Field("error", err.Error()))
		return
	}
	if err := n.srv.Notification.NotifyTelegramUser(ctx, userID, text); err != nil {
		logger.Infow("[RemindExpiring] Telegram notice skipped",
			logger.Field("user_id", userID),
			logger.Field("reason", err.Error()),
		)
	}
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

		Wallet: store.Wallet(),
		Auths:  store.Auth(),
		Redis:  srv.Redis,
		EmailDomains: func() (string, bool) {
			return srv.Config.Email.DomainSuffixList, srv.Config.Email.EnableDomainSuffix
		},
		TelegramBotName: func() string { return srv.Config.Telegram.BotName },
		NotifyTelegramUnbind: func(userID, chatID int64) error {
			return srv.Notification.NotifyTelegramUnbind(userID, chatID)
		},
		AuthConfig: func() identity.AuthSnapshot {
			c := srv.Config
			return identity.AuthSnapshot{
				JWTAccessSecret: c.JwtAuth.AccessSecret,
				JWTAccessExpire: c.JwtAuth.AccessExpire,

				EmailEnabled:            c.Email.Enable,
				EmailVerifyEnabled:      c.Email.EnableVerify,
				EmailDomainSuffixList:   c.Email.DomainSuffixList,
				EmailEnableDomainSuffix: c.Email.EnableDomainSuffix,
				MobileEnabled:           c.Mobile.Enable,
				DeviceEnabled:           c.Device.Enable,
				DeviceOnlyReal:          c.Device.OnlyRealDevice,

				InviteForced:      c.Invite.ForcedInvite,
				OnlyFirstPurchase: c.Invite.OnlyFirstPurchase,
				TrialEnabled:      c.Register.EnableTrial,
				TrialSubscribeID:  c.Register.TrialSubscribe,
				TrialTime:         c.Register.TrialTime,
				TrialTimeUnit:     c.Register.TrialTimeUnit,

				StopRegister:            c.Register.StopRegister,
				RegisterVerify:          c.Verify.RegisterVerify,
				TurnstileSecret:         c.Verify.TurnstileSecret,
				EnableIpRegisterLimit:   c.Register.EnableIpRegisterLimit,
				IpRegisterLimit:         c.Register.IpRegisterLimit,
				IpRegisterLimitDuration: c.Register.IpRegisterLimitDuration,

				SiteHost: c.Site.Host,
			}
		},
		VerifyQueue: srv.Queue,
		SenderConfig: func() identity.SenderSnapshot {
			c := srv.Config
			return identity.SenderSnapshot{
				EmailPlatform:        c.Email.Platform,
				EmailPlatformConfig:  c.Email.PlatformConfig,
				MobilePlatform:       c.Mobile.Platform,
				MobilePlatformConfig: c.Mobile.PlatformConfig,
				SiteName:             c.Site.SiteName,
			}
		},
		Reinitialize: func(subsystem string) {
			if srv.ReinitSubsystem != nil {
				srv.ReinitSubsystem(subsystem)
			}
		},
		VerifyCodeConfig: func() identity.VerifyCodeSnapshot {
			c := srv.Config
			return identity.VerifyCodeSnapshot{
				DomainSuffixList:   c.Email.DomainSuffixList,
				EnableDomainSuffix: c.Email.EnableDomainSuffix,
				VerifyCodeInterval: c.VerifyCode.Interval,
				VerifyCodeLimit:    c.VerifyCode.Limit,
				VerifyCodeExpire:   c.VerifyCode.ExpireTime,
				SiteLogo:           c.Site.SiteLogo,
				SiteName:           c.Site.SiteName,
			}
		},
	})
}

// newNetworkModule wires the network module against the legacy store; the
// node/subscribe configuration is runtime-mutable, so the module receives a
// per-request snapshot closure.
func newNetworkModule(store repository.Store, srv *ServiceContext) network.Service {
	return network.New(network.Deps{
		Store: store,
		Redis: srv.Redis,
		Config: func() network.Snapshot {
			return network.Snapshot{
				Node:      srv.Config.Node,
				Subscribe: srv.Config.Subscribe,
			}
		},
		Multiplier: func(at time.Time) float32 {
			if srv.NodeMultiplierManager == nil {
				return 1
			}
			return srv.NodeMultiplierManager.GetMultiplier(at)
		},
	})
}

// asynqEventPublisher puts domain events on the asynq queue. The task id is
// derived from the outbox event id, so a replayed enqueue (mark-published
// failed after a successful enqueue) hits the id conflict and is success,
// not an error; the retention window keeps the id claimed briefly after the
// delivery completes to widen that dedup.
type asynqEventPublisher struct {
	client *asynqx.Client
}

func (p asynqEventPublisher) Publish(ctx context.Context, event eventbus.Event) error {
	payload, err := json.Marshal(queuetypes.EventDeliverPayload{
		ID: event.ID, Topic: event.Topic, Key: event.Key, Payload: event.Payload,
	})
	if err != nil {
		return err
	}
	task := asynq.NewTask(queuetypes.EventDeliver, payload)
	// The delivery joins the ORIGINATING request's trace (stored on the
	// outbox row), not the publish pump's, so the wrap happens here with
	// the resumed origin context and the enqueue below goes through the
	// raw client to avoid re-stamping the pump's own span.
	task = asynqx.Wrap(originContext(ctx, event.TraceCarrier), task)
	_, err = p.client.Client.EnqueueContext(ctx, task,
		asynq.TaskID(queuetypes.EventTaskID(event.ID)),
		asynq.Retention(time.Hour))
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}

// originContext resumes the trace context serialized on the outbox row;
// rows without one (pre-trace events, traceless producers) fall back to the
// pump's context.
func originContext(ctx context.Context, carrier string) context.Context {
	if carrier == "" {
		return ctx
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(carrier), &m); err != nil || len(m) == 0 {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(context.Background(), propagation.MapCarrier(m))
}

// newEventBus wires the domain-event bus onto the asynq broker: producers
// append to the outbox inside their transactions; the queue's publish pump
// enqueues each event as an events:deliver task; the queue worker delivers
// it through these subscriptions. Handlers call module facades and rely on
// the modules' inbox idempotency.
func newEventBus(store repository.Store, srv *ServiceContext) *eventbus.Bus {
	bus := eventbus.New(store.Outbox(), asynqEventPublisher{client: srv.Queue})
	bus.Subscribe("identity.user_registered", "subscription.trial_grant", func(ctx context.Context, event eventbus.Event) error {
		userID, err := strconv.ParseInt(event.Key, 10, 64)
		if err != nil {
			logger.Errorw("[EventBus] corrupt user_registered key; dropping", logger.Field("key", event.Key))
			return nil
		}
		return srv.Subscription.GrantTrial(ctx, userID)
	})
	return bus
}

// newNotificationModule wires the notification module against the legacy
// store; the bot client is runtime-recreated, so the module reads it per
// call.
func newNotificationModule(store repository.Store, srv *ServiceContext) notification.Service {
	return notification.New(notification.Deps{
		Bot:           func() *tgbot.Bot { return srv.TelegramBot },
		GroupChatID:   func() int64 { return srv.Config.Telegram.GroupChatID },
		Topics:        store.TelegramTopic(),
		Redis:         srv.Redis,
		Users:         store.User(),
		UserAuth:      store.UserAuth(),
		UserCache:     store.UserCache(),
		Tickets:       store.Ticket(),
		Orders:        store.Order(),
		Subscriptions: store.UserSubscription(),
		Plans:         store.Subscribe(),
		Logs:          store.Log(),
		Wallet:        store.Wallet(),
	})
}

// newSupportModule wires the support module against the legacy store. The
// adapters below satisfy the module's ports until the owning modules exist
// (ADR-001).
func newSupportModule(store repository.Store, queue *asynqx.Client, srv *ServiceContext) support.Service {
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
		TicketNotify:  ticketTopicNotifier{srv: srv},
	})
}

// ticketTopicNotifier mirrors ticket lifecycle into the Telegram admin
// group. Best-effort by the port's contract: the group being unconfigured
// or unreachable only logs — the ticket operation already succeeded. The
// mirror runs detached from the request: a user submitting a ticket must
// not wait on Telegram round-trips (the bot client's HTTP timeout is 60s).
type ticketTopicNotifier struct{ srv *ServiceContext }

func (n ticketTopicNotifier) enabled() bool {
	return n.srv.Config.Telegram.GroupChatID != 0 && n.srv.Notification != nil
}

func (n ticketTopicNotifier) mirror(ctx context.Context, ticketID int64, what string, call func(ctx context.Context) error) {
	if !n.enabled() {
		return
	}
	detached := context.WithoutCancel(ctx)
	go func() {
		mirrorCtx, cancel := context.WithTimeout(detached, 15*time.Second)
		defer cancel()
		if err := call(mirrorCtx); err != nil {
			logger.WithContext(mirrorCtx).Errorw("[TicketTopic] "+what+" mirror failed",
				logger.Field("error", err.Error()), logger.Field("ticket_id", ticketID))
		}
	}()
}

func (n ticketTopicNotifier) TicketCreated(ctx context.Context, t *ticket.Ticket) {
	n.mirror(ctx, t.Id, "create", func(ctx context.Context) error {
		return n.srv.Notification.NotifyTicketCreated(ctx, t)
	})
}

func (n ticketTopicNotifier) TicketReplied(ctx context.Context, ticketID int64, from, content string) {
	n.mirror(ctx, ticketID, "reply", func(ctx context.Context) error {
		return n.srv.Notification.NotifyTicketReplied(ctx, ticketID, from, content)
	})
}

func (n ticketTopicNotifier) TicketStatusChanged(ctx context.Context, ticketID int64, status uint8) {
	n.mirror(ctx, ticketID, "status", func(ctx context.Context) error {
		return n.srv.Notification.NotifyTicketStatusChanged(ctx, ticketID, status)
	})
}

// marketingQueue adapts the asynq client to the support module's
// MarketingQueue port, keeping queue task types out of the module.
type marketingQueue struct {
	client *asynqx.Client
}

func (q marketingQueue) EnqueueBatchEmail(ctx context.Context, taskID int64, processAt time.Time) (string, error) {
	queueTaskID := fmt.Sprintf("marketing-email-%d-initial", taskID)
	t := asynq.NewTask(queuetypes.ScheduledBatchSendEmail, []byte(strconv.FormatInt(taskID, 10)))
	if err := q.enqueueIdempotent(ctx, t, queueTaskID, asynq.ProcessAt(processAt)); err != nil {
		return "", err
	}
	return queueTaskID, nil
}

func (q marketingQueue) EnqueueQuota(ctx context.Context, taskID int64) error {
	t := asynq.NewTask(queuetypes.ForthwithQuotaTask, []byte(strconv.FormatInt(taskID, 10)))
	return q.enqueueIdempotent(ctx, t, fmt.Sprintf("marketing-quota-%d", taskID))
}

// enqueueIdempotent retries once with the same task ID on a detached bounded
// context. This resolves the common "Redis accepted the write but the client
// lost the response" case as an ID conflict instead of falsely abandoning a
// durable database task.
func (q marketingQueue) enqueueIdempotent(ctx context.Context, t *asynq.Task, taskID string, opts ...asynq.Option) error {
	options := append(append([]asynq.Option{}, opts...), asynq.TaskID(taskID))
	_, err := q.client.EnqueueContext(ctx, t, options...)
	if err == nil || errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}

	retryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, retryErr := q.client.EnqueueContext(retryCtx, t, options...)
	if retryErr == nil || errors.Is(retryErr, asynq.ErrTaskIDConflict) {
		return nil
	}
	return errors.Join(err, retryErr)
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
