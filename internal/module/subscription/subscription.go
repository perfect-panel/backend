// Package subscription is the facade of the subscription module: plan and
// group management plus the public storefront listings (subscription
// delivery joins as migration proceeds). See docs/adr-001-modular-monolith.md.
package subscription

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/module/subscription/internal/delivery"
	"github.com/perfect-panel/server/internal/module/subscription/internal/plan"
	"github.com/perfect-panel/server/internal/module/subscription/internal/selfsub"
	"github.com/perfect-panel/server/internal/module/subscription/internal/storefront"
	"github.com/perfect-panel/server/internal/module/subscription/internal/usersub"
	"github.com/perfect-panel/server/internal/repository"
)

// Service is the only surface other code may depend on; the implementation
// lives under internal/ where the compiler seals it off.
type Service interface {
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
}

// RequestMeta re-exports the delivery subdomain's transport details.
type RequestMeta = delivery.RequestMeta

// DeliveryConfig re-exports the delivery subdomain's runtime snapshot.
type DeliveryConfig = delivery.Config

// SubscriptionTransactor re-exports the plan subdomain's transaction port.
type SubscriptionTransactor = plan.SubscriptionTransactor

// Deps declares everything the module needs; the composition root
// (internal/svc) provides them.
type Deps struct {
	Plans    repository.SubscribeRepo
	UserSubs repository.UserSubscriptionRepo
	Nodes    repository.NodeRepo
	Store    SubscriptionTransactor
	// NotifyPlanChanged broadcasts a plan update to connected devices.
	NotifyPlanChanged func()
	// Host is the site host list (first line is used for node fallbacks).
	Host string
	// IsTrialPlan reports whether the plan is the configured trial plan.
	IsTrialPlan func(planID int64) bool

	// Delivery dependencies.
	Clients repository.ClientRepo
	Users   repository.UserRepo
	Logs    repository.LogRepo
	// DeliveryConfig reads the runtime-mutable delivery configuration.
	DeliveryConfig func() DeliveryConfig

	// User-subscription administration dependencies.
	Devices repository.UserDeviceRepo
	Cache   repository.UserCacheRepo
	Traffic repository.TrafficRepo
	// FullStore is the transitional full-store dependency for the admin and
	// self-service subscription transactions.
	FullStore repository.Store
	Orders    repository.OrderRepo
	Inbox     repository.InboxRepo
	// SingleModel forbids holding more than one blocking subscription.
	SingleModel bool
}

func New(deps Deps) Service {
	return &service{
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
			Users:          deps.Users,
			Nodes:          deps.Nodes,
			Logs:           deps.Logs,
			ConfigSnapshot: deps.DeliveryConfig,
		}),
		selfSubs: selfsub.NewService(selfsub.Deps{
			UserSubs:    deps.UserSubs,
			Plans:       deps.Plans,
			Users:       deps.Users,
			Orders:      deps.Orders,
			Cache:       deps.Cache,
			Logs:        deps.Logs,
			Inbox:       deps.Inbox,
			Store:       deps.FullStore,
			SingleModel: deps.SingleModel,
		}),
		userSubs: usersub.NewService(usersub.Deps{
			Plans:       deps.Plans,
			UserSubs:    deps.UserSubs,
			Users:       deps.Users,
			Devices:     deps.Devices,
			Cache:       deps.Cache,
			Traffic:     deps.Traffic,
			Logs:        deps.Logs,
			Store:       deps.FullStore,
			SingleModel: deps.SingleModel,
		}),
		storefront: storefront.NewService(storefront.Deps{
			Plans:       deps.Plans,
			UserSubs:    deps.UserSubs,
			Nodes:       deps.Nodes,
			Host:        deps.Host,
			IsTrialPlan: deps.IsTrialPlan,
		}),
	}
}

type service struct {
	plans      *plan.Service
	storefront *storefront.Service
	delivery   *delivery.Service
	userSubs   *usersub.Service
	selfSubs   *selfsub.Service
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
