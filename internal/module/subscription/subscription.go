// Package subscription is the facade of the subscription module: plan and
// group management plus the public storefront listings (subscription
// delivery joins as migration proceeds). See docs/adr-001-modular-monolith.md.
package subscription

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/module/subscription/internal/plan"
	"github.com/perfect-panel/server/internal/module/subscription/internal/storefront"
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
}

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
}

func New(deps Deps) Service {
	return &service{
		plans: plan.NewService(plan.Deps{
			Plans:             deps.Plans,
			UserSubs:          deps.UserSubs,
			Store:             deps.Store,
			NotifyPlanChanged: deps.NotifyPlanChanged,
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
