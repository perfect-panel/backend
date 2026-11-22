// Package subscription is the facade of the subscription module: plans and
// groups, the storefront, user subscriptions and their self-service,
// subscription delivery and client applications, order fulfillment and
// provider entitlements, trials, quota tasks, the lifecycle sweeps and
// calendar traffic resets, plan inventory and traffic usage accounting. See
// docs/design/adr-001-modular-monolith.md.
package subscription

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/application"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/delivery"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/fulfillment"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/nodeaccess"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/plan"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/quotatask"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/repo"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/selfsub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/storefront"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/sweep"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/trafficreset"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/trial"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
)

// Service is the only surface other code may depend on; the implementation
// lives under internal/ where the compiler seals it off.
type Service interface {
	// CheckSubscriptions runs the lifecycle sweeps (traffic exceeded,
	// expired), committing status flips in subscription transactions with
	// notification and cache invalidation as post-commit side effects.
	CheckSubscriptions(ctx context.Context) error
	// RemindExpiringSubscriptions warns owners whose subscription expires
	// soon. It is a daily pass, not part of the minute-by-minute sweep: the
	// notice is once per expiry and reaching users at a civil hour matters.
	RemindExpiringSubscriptions(ctx context.Context) error
	// ResetCalendarTraffic clears the traffic of the subscriptions owed their
	// plan's calendar reset (1st of the month, monthly, yearly): those whose
	// reset falls on today and those a missed run left unreset since their
	// last reset day, each at most once per day however often a failed run
	// is repeated.
	ResetCalendarTraffic(ctx context.Context) error
	// ProcessQuotaTask executes an admin-scheduled quota grant (time
	// extension / gift credit) for the task's subscription scope.
	ProcessQuotaTask(ctx context.Context, taskID int64) error
	// FulfillPaidOrder applies a paid order's business effect (new
	// subscription, renewal or traffic reset) exactly once and returns the
	// notification context.
	FulfillPaidOrder(ctx context.Context, orderNo string) (*FulfillmentOutcome, error)
	ReconcileEntitlement(ctx context.Context, cmd dto.ReconcileEntitlementCommand) (*dto.EntitlementResult, error)
	// GrantTrial consumes the identity.user_registered event: applies the
	// registration trial exactly once (a disabled policy still consumes the
	// event).
	GrantTrial(ctx context.Context, userID int64) error

	// The client-application management for subscription delivery.
	CreateSubscribeApplication(ctx context.Context, req *dto.CreateSubscribeApplicationRequest) (*dto.SubscribeApplication, error)
	UpdateSubscribeApplication(ctx context.Context, req *dto.UpdateSubscribeApplicationRequest) (*dto.SubscribeApplication, error)
	DeleteSubscribeApplication(ctx context.Context, req *dto.DeleteSubscribeApplicationRequest) error
	GetSubscribeApplicationList(ctx context.Context, req *dto.GetSubscribeApplicationListRequest) (*dto.GetSubscribeApplicationListResponse, error)
	PreviewSubscribeTemplate(ctx context.Context, req *dto.PreviewSubscribeTemplateRequest) (*dto.PreviewSubscribeTemplateResponse, error)

	CreateSubscribe(ctx context.Context, req *dto.CreateSubscribeRequest) error
	UpdateSubscribe(ctx context.Context, req *dto.UpdateSubscribeRequest) error
	DeleteSubscribe(ctx context.Context, req *dto.DeleteSubscribeRequest) error
	BatchDeleteSubscribe(ctx context.Context, req *dto.BatchDeleteSubscribeRequest) error
	GetSubscribeList(ctx context.Context, req *dto.GetSubscribeListRequest) (*dto.GetSubscribeListResponse, error)
	GetSubscribeDetails(ctx context.Context, req *dto.GetSubscribeDetailsRequest) (*dto.Subscribe, error)
	SubscribeSort(ctx context.Context, req *dto.SubscribeSortRequest) error
	ResetAllSubscribeToken(ctx context.Context) (*dto.ResetAllSubscribeTokenResponse, error)
	CreateSubscribeGroup(ctx context.Context, req *dto.CreateSubscribeGroupRequest) error
	UpdateSubscribeGroup(ctx context.Context, req *dto.UpdateSubscribeGroupRequest) error
	DeleteSubscribeGroup(ctx context.Context, req *dto.DeleteSubscribeGroupRequest) error
	BatchDeleteSubscribeGroup(ctx context.Context, req *dto.BatchDeleteSubscribeGroupRequest) error
	GetSubscribeGroupList(ctx context.Context) (*dto.GetSubscribeGroupListResponse, error)

	QuerySubscribeList(ctx context.Context, req *dto.QuerySubscribeListRequest) (*dto.QuerySubscribeListResponse, error)
	QuerySubscribeGroupList(ctx context.Context) (*dto.QuerySubscribeGroupListResponse, error)
	QueryUserSubscribeNodeList(ctx context.Context) (*dto.QueryUserSubscribeNodeListResponse, error)

	// Deliver renders the client configuration for a subscription token.
	Deliver(ctx context.Context, meta RequestMeta, req *dto.SubscribeRequest) (*dto.SubscribeResponse, error)
	// IsUserAgentAllowed gates delivery by the configured user-agent allowlist.
	IsUserAgentAllowed(ctx context.Context, userAgent string) bool

	// User self-service subscription management.
	QueryUserSubscribe(ctx context.Context) (*dto.QueryUserSubscribeListResponse, error)
	// ResetOwnSubscribeToken is the self-service variant; ownership of the
	// subscription is enforced against the request context.
	ResetOwnSubscribeToken(ctx context.Context, req *dto.ResetUserSubscribeTokenRequest) error
	GetSubscribeLog(ctx context.Context, req *dto.GetSubscribeLogRequest) (*dto.GetSubscribeLogResponse, error)
	UpdateUserSubscribeNote(ctx context.Context, req *dto.UpdateUserSubscribeNoteRequest) error
	PreUnsubscribe(ctx context.Context, req *dto.PreUnsubscribeRequest) (*dto.PreUnsubscribeResponse, error)
	// Unsubscribe cancels in a subscription transaction and settles the
	// refund in a billing transaction, resumable via the idempotent inbox.
	Unsubscribe(ctx context.Context, req *dto.UnsubscribeRequest) error

	// Admin-side user subscription management.
	CreateUserSubscribe(ctx context.Context, req *dto.CreateUserSubscribeRequest) error
	DeleteUserSubscribe(ctx context.Context, req *dto.DeleteUserSubscribeRequest) error
	UpdateUserSubscribe(ctx context.Context, req *dto.UpdateUserSubscribeRequest) error
	GetUserSubscribe(ctx context.Context, req *dto.GetUserSubscribeListRequest) (*dto.GetUserSubscribeListResponse, error)
	GetUserSubscribeById(ctx context.Context, req *dto.GetUserSubscribeByIdRequest) (*dto.UserSubscribeDetail, error)
	GetUserSubscribeDevices(ctx context.Context, req *dto.GetUserSubscribeDevicesRequest) (*dto.GetUserSubscribeDevicesResponse, error)
	GetUserSubscribeLogs(ctx context.Context, req *dto.GetUserSubscribeLogsRequest) (*dto.GetUserSubscribeLogsResponse, error)
	GetUserSubscribeResetTrafficLogs(ctx context.Context, req *dto.GetUserSubscribeResetTrafficLogsRequest) (*dto.GetUserSubscribeResetTrafficLogsResponse, error)
	GetUserSubscribeTrafficLogs(ctx context.Context, req *dto.GetUserSubscribeTrafficLogsRequest) (*dto.GetUserSubscribeTrafficLogsResponse, error)
	ResetUserSubscribeToken(ctx context.Context, req *dto.ResetUserSubscribeTokenRequest) error
	ResetUserSubscribeTraffic(ctx context.Context, req *dto.ResetUserSubscribeTrafficRequest) error
	ToggleUserSubscribeStatus(ctx context.Context, req *dto.ToggleUserSubscribeStatusRequest) error
	// ChangeUserSubscribeStatus stops or resumes a subscription the caller
	// saw in status from, refusing when the status changed meanwhile.
	ChangeUserSubscribeStatus(ctx context.Context, id int64, from, to uint8) error

	// Access and Reads serve the other modules' reads and cache
	// invalidation (see access.go and reads.go).
	Access
	Reads
}

// TrialGrantConsumer is the trial grant's consumer identity on the event bus
// and in its inbox markers. It is persisted: renaming it would grant
// committed trials again.
const TrialGrantConsumer = trial.Consumer

// RequestMeta re-exports the delivery subdomain's transport details.
type RequestMeta = delivery.RequestMeta

// DeliveryConfig re-exports the delivery subdomain's runtime snapshot.
type DeliveryConfig = delivery.Config

// DeliveryLimiter re-exports the delivery subdomain's per-address fetch
// limiter port.
type DeliveryLimiter = delivery.FetchLimiter

// NewDeliveryLimiter returns the per-address fetch limiter of subscription
// delivery over rds: delivery.FetchRateQuota fetches per
// delivery.FetchRateWindow and address.
func NewDeliveryLimiter(rds *redis.Client) DeliveryLimiter {
	return delivery.NewFetchLimiter(rds)
}

// SubscriptionTransactor re-exports the plan subdomain's transaction port.
type SubscriptionTransactor = plan.SubscriptionTransactor

// Deps declares everything the module needs; the composition root
// (internal/app) provides them.
type Deps struct {
	Plans    repository.SubscribeRepo
	UserSubs repository.UserSubscriptionRepo
	Nodes    NodeReader
	Store    SubscriptionTransactor
	// NotifyPlanChanged broadcasts a plan update to connected devices.
	NotifyPlanChanged func()
	// IsTrialPlan reports whether the plan is the configured trial plan.
	IsTrialPlan func(planID int64) bool

	// Delivery dependencies.
	Clients repository.ClientRepo
	Logs    repository.LogRepo
	// DeliveryConfig reads the runtime-mutable delivery configuration.
	DeliveryConfig func() DeliveryConfig
	// DeliveryLimiter bounds the subscription fetches per client address
	// (NewDeliveryLimiter); nil admits every fetch.
	DeliveryLimiter DeliveryLimiter

	// Accounts is the identity port of delivery, administration, lifecycle
	// and quota use cases: owners, their devices and email bindings, and
	// their cached projections.
	Accounts Accounts

	// User-subscription administration dependencies.
	Traffic TrafficLogReader
	// Operations composes only the persistence capabilities of this module's
	// administration, lifecycle and fulfillment use cases.
	Operations Store
	// Orders, Refunds and QuotaGifts are the billing module's side of
	// fulfillment, cancellation and quota tasks: the orders they read, the
	// cancellation's refund stage and the quota task's gift stage.
	Orders     OrderReader
	Refunds    RefundSettler
	QuotaGifts QuotaGiftLedger
	Inbox      repository.InboxRepo
	// SingleModel forbids holding more than one blocking subscription;
	// runtime-mutable, read per request.
	SingleModel func() bool
	// TrialPolicy snapshots the runtime-mutable registration-trial settings
	// per call.
	TrialPolicy func() TrialPolicy

	// LifecycleNotify is the lifecycle sweep's owner notification channel.
	LifecycleNotify sweep.Notifier
}

// OrderReader is the billing read port of fulfillment and the refund quote:
// the paid order by id or number, and the order with its renewals.
type OrderReader interface {
	fulfillment.OrderReader
	selfsub.OrderReader
}

// RefundSettler and QuotaGiftLedger re-export the billing ports of the
// cancellation's refund stage and the quota task's gift stage; the billing
// facade provides both.
type (
	RefundSettler   = selfsub.RefundSettler
	QuotaGiftLedger = quotatask.GiftLedger
)

// NewRepoBuilder exports the module-owned repository implementations for
// store assembly (ADR-001 step-6 preparation).
func NewRepoBuilder() repository.SubscriptionBuilder {
	return func(c repository.ModuleConn, nodes repository.NodeCacheKeyBridge) repository.SubscriptionRepos {
		conn := c.Conn()
		subs := repo.NewUserSubscriptionRepo(conn)
		return repository.SubscriptionRepos{
			Entitlements: repo.NewEntitlementRepo(c.DB),
			Plans:        repo.NewSubscribeRepo(conn, nodes),
			UserSubs:     subs,
			Traffic:      subs,
			Clients:      repo.NewClientRepo(conn),
			CacheBridge:  subs,
			ScopeBridge:  subs,
		}
	}
}

// New assembles the module from its dependencies.
func New(deps Deps) Service {
	return &service{
		reads: reads{plans: deps.Plans, userSubs: deps.UserSubs},
		trials: trial.NewService(trial.Deps{
			Plans:       deps.Plans,
			Cache:       deps.UserSubs,
			Store:       deps.Operations,
			TrialPolicy: deps.TrialPolicy,
		}),
		fulfil: fulfillment.NewService(fulfillment.Deps{
			Orders:      deps.Orders,
			Store:       deps.Operations,
			UserSubs:    deps.UserSubs,
			Plans:       deps.Plans,
			Cache:       deps.UserSubs,
			SingleModel: deps.SingleModel,
		}),
		quota: quotatask.NewService(quotatask.Deps{
			Accounts: deps.Accounts,
			Store:    deps.Operations,
			Gifts:    deps.QuotaGifts,
		}),
		trafficReset: trafficreset.NewService(trafficreset.Deps{
			Store: deps.Operations,
			Plans: deps.Plans,
		}),
		sweeper: sweep.NewService(sweep.Deps{
			UserSubs: deps.UserSubs,
			Plans:    deps.Plans,
			Cache:    deps.UserSubs,
			Store:    deps.Operations,
			Emails:   deps.Accounts,
			Owners:   deps.Accounts,
			Notify:   deps.LifecycleNotify,
		}),
		apps: application.NewService(application.Deps{
			Clients: deps.Clients,
			Nodes:   deps.Nodes,
		}),
		plans: plan.NewService(plan.Deps{
			Plans:             deps.Plans,
			UserSubs:          deps.UserSubs,
			Store:             deps.Store,
			NotifyPlanChanged: deps.NotifyPlanChanged,
		}),
		delivery: delivery.NewService(delivery.Deps{
			Clients:        deps.Clients,
			Plans:          deps.Plans,
			UserSubs:       deps.UserSubs,
			Users:          deps.Accounts,
			Nodes:          deps.Nodes,
			Logs:           deps.Logs,
			ConfigSnapshot: deps.DeliveryConfig,
			Limiter:        deps.DeliveryLimiter,
		}),
		selfSubs: selfsub.NewService(selfsub.Deps{
			UserSubs:    deps.UserSubs,
			Plans:       deps.Plans,
			Orders:      deps.Orders,
			Refunds:     deps.Refunds,
			Cache:       deps.UserSubs,
			Logs:        deps.Logs,
			Inbox:       deps.Inbox,
			Store:       deps.Operations,
			SingleModel: deps.SingleModel,
		}),
		userSubs: usersub.NewService(usersub.Deps{
			Plans:       deps.Plans,
			UserSubs:    deps.UserSubs,
			Users:       deps.Accounts,
			Devices:     deps.Accounts,
			Cache:       deps.UserSubs,
			Traffic:     deps.Traffic,
			Logs:        deps.Logs,
			Store:       deps.Operations,
			SingleModel: deps.SingleModel,
		}),
		storefront: storefront.NewService(storefront.Deps{
			Plans:       deps.Plans,
			UserSubs:    deps.UserSubs,
			Nodes:       deps.Nodes,
			IsTrialPlan: deps.IsTrialPlan,
		}),
		nodeAccess: nodeaccess.NewService(nodeaccess.Deps{
			Plans:         deps.Plans,
			Subscriptions: deps.UserSubs,
		}),
	}
}

type service struct {
	reads
	trials       *trial.Service
	fulfil       *fulfillment.Service
	quota        *quotatask.Service
	trafficReset *trafficreset.Service
	sweeper      *sweep.Service
	apps         *application.Service
	plans        *plan.Service
	storefront   *storefront.Service
	delivery     *delivery.Service
	userSubs     *usersub.Service
	selfSubs     *selfsub.Service
	nodeAccess   *nodeaccess.Service
}

func (s *service) CreateSubscribe(ctx context.Context, req *dto.CreateSubscribeRequest) error {
	return s.plans.CreateSubscribe(ctx, req)
}

func (s *service) UpdateSubscribe(ctx context.Context, req *dto.UpdateSubscribeRequest) error {
	return s.plans.UpdateSubscribe(ctx, req)
}

func (s *service) DeleteSubscribe(ctx context.Context, req *dto.DeleteSubscribeRequest) error {
	return s.plans.DeleteSubscribe(ctx, req)
}

func (s *service) BatchDeleteSubscribe(ctx context.Context, req *dto.BatchDeleteSubscribeRequest) error {
	return s.plans.BatchDeleteSubscribe(ctx, req)
}

func (s *service) GetSubscribeList(ctx context.Context, req *dto.GetSubscribeListRequest) (*dto.GetSubscribeListResponse, error) {
	return s.plans.GetSubscribeList(ctx, req)
}

func (s *service) GetSubscribeDetails(ctx context.Context, req *dto.GetSubscribeDetailsRequest) (*dto.Subscribe, error) {
	return s.plans.GetSubscribeDetails(ctx, req)
}

func (s *service) SubscribeSort(ctx context.Context, req *dto.SubscribeSortRequest) error {
	return s.plans.SubscribeSort(ctx, req)
}

func (s *service) ResetAllSubscribeToken(ctx context.Context) (*dto.ResetAllSubscribeTokenResponse, error) {
	return s.plans.ResetAllSubscribeToken(ctx)
}

func (s *service) CreateSubscribeGroup(ctx context.Context, req *dto.CreateSubscribeGroupRequest) error {
	return s.plans.CreateSubscribeGroup(ctx, req)
}

func (s *service) UpdateSubscribeGroup(ctx context.Context, req *dto.UpdateSubscribeGroupRequest) error {
	return s.plans.UpdateSubscribeGroup(ctx, req)
}

func (s *service) DeleteSubscribeGroup(ctx context.Context, req *dto.DeleteSubscribeGroupRequest) error {
	return s.plans.DeleteSubscribeGroup(ctx, req)
}

func (s *service) BatchDeleteSubscribeGroup(ctx context.Context, req *dto.BatchDeleteSubscribeGroupRequest) error {
	return s.plans.BatchDeleteSubscribeGroup(ctx, req)
}

func (s *service) GetSubscribeGroupList(ctx context.Context) (*dto.GetSubscribeGroupListResponse, error) {
	return s.plans.GetSubscribeGroupList(ctx)
}

func (s *service) QuerySubscribeList(ctx context.Context, req *dto.QuerySubscribeListRequest) (*dto.QuerySubscribeListResponse, error) {
	return s.storefront.QuerySubscribeList(ctx, req)
}

func (s *service) QuerySubscribeGroupList(ctx context.Context) (*dto.QuerySubscribeGroupListResponse, error) {
	return s.storefront.QuerySubscribeGroupList(ctx)
}

func (s *service) QueryUserSubscribeNodeList(ctx context.Context) (*dto.QueryUserSubscribeNodeListResponse, error) {
	return s.storefront.QueryUserSubscribeNodeList(ctx)
}

func (s *service) Deliver(ctx context.Context, meta RequestMeta, req *dto.SubscribeRequest) (*dto.SubscribeResponse, error) {
	return s.delivery.Deliver(ctx, meta, req)
}

func (s *service) IsUserAgentAllowed(ctx context.Context, userAgent string) bool {
	return s.delivery.IsUserAgentAllowed(ctx, userAgent)
}

func (s *service) CreateUserSubscribe(ctx context.Context, req *dto.CreateUserSubscribeRequest) error {
	return s.userSubs.CreateUserSubscribe(ctx, req)
}

func (s *service) DeleteUserSubscribe(ctx context.Context, req *dto.DeleteUserSubscribeRequest) error {
	return s.userSubs.DeleteUserSubscribe(ctx, req)
}

func (s *service) UpdateUserSubscribe(ctx context.Context, req *dto.UpdateUserSubscribeRequest) error {
	return s.userSubs.UpdateUserSubscribe(ctx, req)
}

func (s *service) GetUserSubscribe(ctx context.Context, req *dto.GetUserSubscribeListRequest) (*dto.GetUserSubscribeListResponse, error) {
	return s.userSubs.GetUserSubscribe(ctx, req)
}

func (s *service) GetUserSubscribeById(ctx context.Context, req *dto.GetUserSubscribeByIdRequest) (*dto.UserSubscribeDetail, error) {
	return s.userSubs.GetUserSubscribeById(ctx, req)
}

func (s *service) GetUserSubscribeDevices(ctx context.Context, req *dto.GetUserSubscribeDevicesRequest) (*dto.GetUserSubscribeDevicesResponse, error) {
	return s.userSubs.GetUserSubscribeDevices(ctx, req)
}

func (s *service) GetUserSubscribeLogs(ctx context.Context, req *dto.GetUserSubscribeLogsRequest) (*dto.GetUserSubscribeLogsResponse, error) {
	return s.userSubs.GetUserSubscribeLogs(ctx, req)
}

func (s *service) GetUserSubscribeResetTrafficLogs(ctx context.Context, req *dto.GetUserSubscribeResetTrafficLogsRequest) (*dto.GetUserSubscribeResetTrafficLogsResponse, error) {
	return s.userSubs.GetUserSubscribeResetTrafficLogs(ctx, req)
}

func (s *service) GetUserSubscribeTrafficLogs(ctx context.Context, req *dto.GetUserSubscribeTrafficLogsRequest) (*dto.GetUserSubscribeTrafficLogsResponse, error) {
	return s.userSubs.GetUserSubscribeTrafficLogs(ctx, req)
}

func (s *service) ResetUserSubscribeToken(ctx context.Context, req *dto.ResetUserSubscribeTokenRequest) error {
	return s.userSubs.ResetUserSubscribeToken(ctx, req)
}

func (s *service) ResetUserSubscribeTraffic(ctx context.Context, req *dto.ResetUserSubscribeTrafficRequest) error {
	return s.userSubs.ResetUserSubscribeTraffic(ctx, req)
}

func (s *service) ToggleUserSubscribeStatus(ctx context.Context, req *dto.ToggleUserSubscribeStatusRequest) error {
	return s.userSubs.ToggleUserSubscribeStatus(ctx, req)
}

func (s *service) ChangeUserSubscribeStatus(ctx context.Context, id int64, from, to uint8) error {
	return s.userSubs.ChangeUserSubscribeStatus(ctx, id, from, to)
}

func (s *service) QueryUserSubscribe(ctx context.Context) (*dto.QueryUserSubscribeListResponse, error) {
	return s.selfSubs.QueryUserSubscribe(ctx)
}

func (s *service) ResetOwnSubscribeToken(ctx context.Context, req *dto.ResetUserSubscribeTokenRequest) error {
	return s.selfSubs.ResetUserSubscribeToken(ctx, req)
}

func (s *service) GetSubscribeLog(ctx context.Context, req *dto.GetSubscribeLogRequest) (*dto.GetSubscribeLogResponse, error) {
	return s.selfSubs.GetSubscribeLog(ctx, req)
}

func (s *service) UpdateUserSubscribeNote(ctx context.Context, req *dto.UpdateUserSubscribeNoteRequest) error {
	return s.selfSubs.UpdateUserSubscribeNote(ctx, req)
}

func (s *service) PreUnsubscribe(ctx context.Context, req *dto.PreUnsubscribeRequest) (*dto.PreUnsubscribeResponse, error) {
	return s.selfSubs.PreUnsubscribe(ctx, req)
}

func (s *service) Unsubscribe(ctx context.Context, req *dto.UnsubscribeRequest) error {
	return s.selfSubs.Unsubscribe(ctx, req)
}

func (s *service) CreateSubscribeApplication(ctx context.Context, req *dto.CreateSubscribeApplicationRequest) (*dto.SubscribeApplication, error) {
	return s.apps.CreateSubscribeApplication(ctx, req)
}

func (s *service) UpdateSubscribeApplication(ctx context.Context, req *dto.UpdateSubscribeApplicationRequest) (*dto.SubscribeApplication, error) {
	return s.apps.UpdateSubscribeApplication(ctx, req)
}

func (s *service) DeleteSubscribeApplication(ctx context.Context, req *dto.DeleteSubscribeApplicationRequest) error {
	return s.apps.DeleteSubscribeApplication(ctx, req)
}

func (s *service) GetSubscribeApplicationList(ctx context.Context, req *dto.GetSubscribeApplicationListRequest) (*dto.GetSubscribeApplicationListResponse, error) {
	return s.apps.GetSubscribeApplicationList(ctx, req)
}

func (s *service) PreviewSubscribeTemplate(ctx context.Context, req *dto.PreviewSubscribeTemplateRequest) (*dto.PreviewSubscribeTemplateResponse, error) {
	return s.apps.PreviewSubscribeTemplate(ctx, req)
}

func (s *service) CheckSubscriptions(ctx context.Context) error {
	return s.sweeper.CheckSubscriptions(ctx)
}

func (s *service) RemindExpiringSubscriptions(ctx context.Context) error {
	return s.sweeper.RemindExpiringSubscribes(ctx)
}

func (s *service) ResetCalendarTraffic(ctx context.Context) error {
	return s.trafficReset.ResetDue(ctx)
}

func (s *service) ProcessQuotaTask(ctx context.Context, taskID int64) error {
	return s.quota.ProcessQuotaTask(ctx, taskID)
}

// ErrQuotaTaskUnretryable re-exports the quota subdomain's skip-retry
// sentinel for the queue shell.
var ErrQuotaTaskUnretryable = quotatask.ErrUnretryable

// FulfillmentOutcome re-exports the fulfillment subdomain's notification
// context, and the NotifyKind* constants label which notice applies.
type FulfillmentOutcome = fulfillment.Outcome

// The notices a fulfillment outcome can call for: a new subscription, a
// renewal, or a traffic reset.
const (
	NotifyKindPurchase     = fulfillment.NotifyPurchase
	NotifyKindRenewal      = fulfillment.NotifyRenewal
	NotifyKindResetTraffic = fulfillment.NotifyResetTraffic
)

func (s *service) FulfillPaidOrder(ctx context.Context, orderNo string) (*FulfillmentOutcome, error) {
	return s.fulfil.FulfillPaidOrder(ctx, orderNo)
}

func (s *service) ReconcileEntitlement(ctx context.Context, cmd dto.ReconcileEntitlementCommand) (*dto.EntitlementResult, error) {
	return s.fulfil.ReconcileEntitlement(ctx, cmd)
}

// TrialPolicy re-exports the trial subdomain's policy snapshot for the
// composition root.
type TrialPolicy = trial.Policy

func (s *service) GrantTrial(ctx context.Context, userID int64) error {
	return s.trials.GrantTrial(ctx, userID)
}

// Store is the persistence capability required by this package. It excludes
// unrelated repositories and application-wide transactions.
type Store interface {
	usersub.Store
	selfsub.Store
	sweep.Store
	trial.Store
	fulfillment.Store
	quotatask.Store
	trafficreset.Store
}
