// Package storefront implements the public subscription listings of the
// subscription module. Only the module facade may reach it.
package storefront

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/repository"
)

type Deps struct {
	Plans    repository.SubscribeRepo
	UserSubs repository.UserSubscriptionRepo
	Nodes    repository.NodeRepo
	// Host is the site host list (first line is used for node fallbacks).
	Host string
	// IsTrialPlan reports whether the plan is the currently configured trial
	// plan; the registration config is runtime-mutable.
	IsTrialPlan func(planID int64) bool
}

func (d Deps) isTrialPlan(planID int64) bool {
	return d.IsTrialPlan != nil && d.IsTrialPlan(planID)
}

type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

func (s *Service) QuerySubscribeList(ctx context.Context, req *dto.QuerySubscribeListRequest) (*dto.QuerySubscribeListResponse, error) {
	return newQuerySubscribeListLogic(ctx, s.deps).QuerySubscribeList(req)
}

func (s *Service) QuerySubscribeGroupList(ctx context.Context) (*dto.QuerySubscribeGroupListResponse, error) {
	return newQuerySubscribeGroupListLogic(ctx, s.deps).QuerySubscribeGroupList()
}

func (s *Service) QueryUserSubscribeNodeList(ctx context.Context) (*dto.QueryUserSubscribeNodeListResponse, error) {
	return newQueryUserSubscribeNodeListLogic(ctx, s.deps).QueryUserSubscribeNodeList()
}
