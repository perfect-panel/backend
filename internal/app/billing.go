package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/infra/rediskeys"
	"github.com/perfect-panel/server/internal/infra/taskqueue"
	"github.com/perfect-panel/server/internal/module/billing"
	"github.com/perfect-panel/server/internal/module/identity"
	"github.com/perfect-panel/server/internal/module/identity/entity/user"
	"github.com/perfect-panel/server/internal/module/subscription"
	"github.com/perfect-panel/server/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/server/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/redis/go-redis/v9"
)

// newBillingModule wires the billing module against the shared store and the
// asynq client.
func newBillingModule(c config.Config, store repository.Store, queue *taskqueue.Client, rds *redis.Client, rate *billing.CurrencyRateCache, srv *Application) billing.Service {
	return billing.New(billing.Deps{
		PaidOrders: billing.PaidOrderDependencies{
			GuestAccounts:        identity.NewGuestAccounts(store),
			Subscriptions:        paidOrderSubscription{srv},
			LegacyGuestCache:     rds,
			Notifications:        paidOrderNotifications{srv},
			NotificationsEnabled: func() bool { return srv.Runtime.Config().Telegram.EnableNotify },
		},
		Orders:       store.Order(),
		OrderEvents:  store.OrderEvent(),
		Payments:     store.Payment(),
		Coupons:      store.Coupon(),
		Withdrawals:  store.UserWithdrawal(),
		Plans:        billingPlans{srv},
		UserSubs:     billingSubscriptions{srv},
		Store:        store,
		Inventory:    subscription.NewInventory(store),
		Tx:           store,
		Queue:        activationQueue{client: queue},
		Redis:        rds,
		RedisPrefix:  rediskeys.New(c.Redis.KeyPrefix),
		SingleModel:  func() bool { return srv.Runtime.Config().Subscribe.SingleModel },
		CurrencyUnit: func() string { return srv.Runtime.Config().Currency.Unit },

		Logs:        store.Log(),
		UserCache:   identityUserCache{srv},
		Affiliates:  billingAccounts{srv},
		AuthMethods: billingAccounts{srv},

		UserProfiles: billingAccounts{srv},
		InvitePolicy: func() (uint8, bool) {
			current := srv.Runtime.Config().Invite
			return uint8(current.ReferralPercentage), current.OnlyFirstPurchase
		},

		PortalPlans:        billingPlans{srv},
		GuestAccounts:      billingAccounts{srv},
		Sessions:           rds,
		GuestCheckoutCache: rds,
		ExchangeRate:       rate,
		Portal: billing.PortalConfig{
			SiteName:          func() string { return srv.Runtime.Config().Site.SiteName },
			CurrencyUnit:      func() string { return srv.Runtime.Config().Currency.Unit },
			CurrencyAccessKey: func() string { return srv.Runtime.Config().Currency.AccessKey },
			SiteHost:          func() string { return srv.Runtime.Config().Site.Host },
			// Guest purchases create an account, so they follow the
			// registration Turnstile setting.
			GuestVerification: func() billing.GuestVerification {
				current := srv.Runtime.Config().Verify
				return billing.GuestVerification{Enabled: current.RegisterVerify, Secret: current.TurnstileSecret}
			},
			// A guest purchase creates an account, so it follows the
			// registration gates too: closed registration, disabled
			// methods and the email domain allowlist.
			Registration: func() billing.RegistrationPolicy {
				current := srv.Runtime.Config()
				return billing.RegistrationPolicy{
					StopRegister:            current.Register.StopRegister,
					EmailEnabled:            current.Email.Enable,
					MobileEnabled:           current.Mobile.Enable,
					EmailDomainSuffixList:   current.Email.DomainSuffixList,
					EmailEnableDomainSuffix: current.Email.EnableDomainSuffix,
				}
			},
			JwtSecret: c.JwtAuth.AccessSecret,
			JwtExpire: c.JwtAuth.AccessExpire,
		},
	})
}

// The subscription, identity and notification modules are constructed after
// billing. These adapters resolve their facades when a workflow executes,
// after assembly.
type paidOrderSubscription struct{ srv *Application }

func (p paidOrderSubscription) FulfillPaidOrder(ctx context.Context, orderNo string) (*subscription.FulfillmentOutcome, error) {
	return p.srv.Subscription.FulfillPaidOrder(ctx, orderNo)
}

type paidOrderNotifications struct{ srv *Application }

func (p paidOrderNotifications) NotifyTelegramUser(ctx context.Context, id int64, text string) error {
	return p.srv.Notification.NotifyTelegramUser(ctx, id, text)
}

func (p paidOrderNotifications) NotifyAdminsTelegram(ctx context.Context, text string) error {
	return p.srv.Notification.NotifyAdminsTelegram(ctx, text)
}

// billingPlans serves billing's plan reads (checkout, order details and the
// storefront portal) from the subscription facade.
type billingPlans struct{ srv *Application }

func (p billingPlans) FindOne(ctx context.Context, id int64) (*subscribe.Subscribe, error) {
	return p.srv.Subscription.PlanByID(ctx, id)
}

func (p billingPlans) FilterList(ctx context.Context, params *subscribe.FilterParams) (int64, []*subscribe.Subscribe, error) {
	return p.srv.Subscription.FilterPlans(ctx, params)
}

// billingSubscriptions serves the checkout's user-subscription reads from
// the subscription facade.
type billingSubscriptions struct{ srv *Application }

func (s billingSubscriptions) HasBlockingSubscription(ctx context.Context, userID int64) (bool, error) {
	return s.srv.Subscription.HasBlockingSubscription(ctx, userID)
}

func (s billingSubscriptions) CountQuotaConsumingSubscriptions(ctx context.Context, userID, subscribeID int64) (int64, error) {
	return s.srv.Subscription.CountQuotaConsumingSubscriptions(ctx, userID, subscribeID)
}

func (s billingSubscriptions) FindOneUserSubscribe(ctx context.Context, id int64) (*usersub.SubscribeDetails, error) {
	return s.srv.Subscription.SubscriptionDetailsByID(ctx, id)
}

func (s billingSubscriptions) FindOneSubscribe(ctx context.Context, id int64) (*usersub.Subscribe, error) {
	return s.srv.Subscription.SubscriptionByID(ctx, id)
}

// billingAccounts serves billing's account reads (the referral settings and
// referrer of a commission, the affiliate list with its masked identifiers,
// the guest checkout's identifier check) from the identity facade.
type billingAccounts struct{ srv *Application }

// The alias check is optional for a reader; the production reader must have
// it, or guest purchases would silently skip registration's mailbox rule.
var _ billing.EmailAliasReader = billingAccounts{}

func (a billingAccounts) FindOne(ctx context.Context, id int64) (*user.User, error) {
	return a.srv.Identity.FindUser(ctx, id)
}

func (a billingAccounts) CountAffiliates(ctx context.Context, refererID int64) (int64, error) {
	return a.srv.Identity.CountAffiliates(ctx, refererID)
}

func (a billingAccounts) QueryAffiliateList(ctx context.Context, refererID int64, page, size int) ([]*user.User, int64, error) {
	return a.srv.Identity.ListAffiliates(ctx, refererID, page, size)
}

func (a billingAccounts) FindUserAuthMethods(ctx context.Context, userID int64) ([]*user.AuthMethods, error) {
	return a.srv.Identity.ListUserAuthMethods(ctx, userID)
}

func (a billingAccounts) FindUserAuthMethodByOpenID(ctx context.Context, method, openID string) (*user.AuthMethods, error) {
	return a.srv.Identity.FindAuthMethodByIdentifier(ctx, method, openID)
}

// FindEmailAlias lets guest purchases apply registration's mailbox-alias
// rule (portal.EmailAliasReader).
func (a billingAccounts) FindEmailAlias(ctx context.Context, email string) (*user.AuthMethods, error) {
	return a.srv.Identity.FindEmailAlias(ctx, email)
}

// identityUserCache drops users' cached projections through the identity
// facade, which is constructed after billing and resolved per call.
type identityUserCache struct{ srv *Application }

func (c identityUserCache) ClearUserCache(ctx context.Context, userIDs ...int64) error {
	return c.srv.Identity.ClearUserCache(ctx, userIDs...)
}

// activationQueue adapts the asynq client to the billing module's activation
// port. A task-id conflict means a delivery already exists for the order,
// which is success, not an error.
type activationQueue struct {
	client *taskqueue.Client
}

func (q activationQueue) EnqueueActivation(ctx context.Context, orderNo string) error {
	payload, err := json.Marshal(taskqueue.ForthwithActivateOrderPayload{OrderNo: orderNo})
	if err != nil {
		return err
	}
	task := asynq.NewTask(taskqueue.ForthwithActivateOrder, payload)
	_, err = q.client.EnqueueContext(ctx, task, asynq.TaskID(taskqueue.ActivationTaskID(orderNo)))
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}

// EnqueueDeferredClose schedules the pending order's expiry close after the
// payment window elapses.
func (q activationQueue) EnqueueDeferredClose(ctx context.Context, orderNo string) error {
	payload, err := json.Marshal(taskqueue.DeferCloseOrderPayload{OrderNo: orderNo})
	if err != nil {
		return err
	}
	task := asynq.NewTask(taskqueue.DeferCloseOrder, payload)
	_, err = q.client.EnqueueContext(ctx, task, asynq.MaxRetry(3), asynq.ProcessIn(billing.CloseOrderTimeMinutes*time.Minute))
	return err
}
