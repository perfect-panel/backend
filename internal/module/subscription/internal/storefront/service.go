// Package storefront implements the public subscription listings of the
// subscription module. Only the module facade may reach it.
package storefront

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/repository"
)

// NodeLister is the network read port (the network facade) listing the
// enabled nodes, with their servers, a plan selects.
type NodeLister interface {
	ListEnabledNodesByScope(ctx context.Context, nodeIDs []int64, tags []string) ([]*node.Node, error)
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Plans    repository.SubscribeRepo
	UserSubs repository.UserSubscriptionRepo
	Nodes    NodeLister
	// IsTrialPlan reports whether the plan is the currently configured trial
	// plan; the registration config is runtime-mutable.
	IsTrialPlan func(planID int64) bool
}

func (d Deps) isTrialPlan(planID int64) bool {
	return d.IsTrialPlan != nil && d.IsTrialPlan(planID)
}

// Service is the storefront entry point used by the subscription facade.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}
