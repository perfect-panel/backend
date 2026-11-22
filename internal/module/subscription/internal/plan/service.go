// Package plan implements the subscription plan management subdomain of the
// subscription module: plan and group CRUD, ordering and token resets. Only
// the module facade may reach it.
package plan

import (
	"context"

	"github.com/perfect-panel/backend/internal/repository"
)

// SubscriptionTransactor mirrors the store's subscription-scoped transaction.
type SubscriptionTransactor interface {
	InSubscriptionTx(ctx context.Context, fn func(repository.SubscriptionStore) error) error
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Plans    repository.SubscribeRepo
	UserSubs repository.UserSubscriptionRepo
	Store    SubscriptionTransactor
	// NotifyPlanChanged broadcasts a plan update to connected devices.
	NotifyPlanChanged func()
}

func (d Deps) notifyPlanChanged() {
	if d.NotifyPlanChanged != nil {
		d.NotifyPlanChanged()
	}
}

// Service is the plan-management entry point used by the subscription
// facade.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}
