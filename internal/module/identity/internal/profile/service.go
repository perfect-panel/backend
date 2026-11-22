package profile

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Service is the profile subdomain entry point used by the identity facade.
type Service struct {
	deps Deps
}

// NewService builds the subdomain over the dependencies the facade forwards.
func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// currentUser returns the signed-in account the auth middleware put in ctx;
// a request without one is refused and logged.
func currentUser(ctx context.Context) (*user.User, error) {
	u, ok := user.FromContext(ctx)
	if !ok {
		logger.WithContext(ctx).Error("current user is not found in context")
		return nil, xerr.Errorf(xerr.InvalidAccess, "Invalid Access")
	}
	return u, nil
}
