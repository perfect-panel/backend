package adminuser

import (
	"context"
	"strings"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// CreateUserAuthMethod binds an identity to an account, or replaces the
// account's identity of that type. Emails and phone numbers are stored
// normalized, and one that cannot be is refused.
func (s *Service) CreateUserAuthMethod(ctx context.Context, req *dto.CreateUserAuthMethodRequest) error {
	if strings.EqualFold(strings.TrimSpace(req.AuthType), "device") {
		return xerr.Errorf(xerr.InvalidParams, "use device management for device identities")
	}
	err := s.deps.Store.InIdentityTx(ctx, func(store repository.IdentityStore) error {
		// An administrator's binding vouches for the identity, so it signs
		// in like one the user bound through the provider.
		return store.UserAuth().UpsertUserAuthMethod(ctx, &user.AuthMethods{
			UserId:         req.UserId,
			AuthType:       req.AuthType,
			AuthIdentifier: req.AuthIdentifier,
			Verified:       true,
		})
	})
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "bind %s identity to user %d", req.AuthType, req.UserId)
	}
	return nil
}
