package repository

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/entitlement"
)

// EntitlementRepo is used only inside the subscription transaction.
type EntitlementRepo interface {
	PlanIDs(context.Context, string) ([]int64, error)
	InsertRevision(context.Context, *entitlement.Revision) error
	FindStateForUpdate(context.Context, string) (*entitlement.State, error)
	InsertState(context.Context, *entitlement.State) error
	UpdateState(context.Context, *entitlement.State) error
	FindPeriod(context.Context, string) (*entitlement.Period, error)
	InsertPeriod(context.Context, *entitlement.Period) error
	UpdatePeriod(context.Context, *entitlement.Period) error
}
