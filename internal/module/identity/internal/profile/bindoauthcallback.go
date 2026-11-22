package profile

import (
	"context"
	"errors"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthstate"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// BindOAuthCallback completes binding the req.Method identity the provider
// vouches for to the calling account. An account holds at most one identity
// per method: binding the identity it already holds succeeds again, which a
// client retrying a timed-out callback relies on, and another account's
// identity or a second one of the method is refused.
func (s *Service) BindOAuthCallback(ctx context.Context, req *dto.BindOAuthCallbackRequest) error {
	if err := s.deps.Policy.EnsureMethodEnabled(ctx, req.Method); err != nil {
		return err
	}
	current, ok := user.FromContext(ctx)
	if !ok {
		return xerr.Errorf(xerr.InvalidAccess, "no signed-in user")
	}
	fields, ok := req.Callback.(map[string]any)
	if !ok {
		return xerr.Errorf(xerr.InvalidParams, "OAuth callback must be an object")
	}
	identity, err := s.deps.OAuth.Identify(ctx, oauthstate.BindScope(current.Id), req.Method, fields, "")
	if err != nil {
		return err
	}

	holder, err := s.deps.UserAuth.FindUserAuthMethodByOpenID(ctx, req.Method, identity.Subject)
	switch {
	case err == nil && holder.UserId == current.Id:
		// The caller just proved the identity with the provider, which is
		// what verified means; an administrator's binding from before that
		// flag was set signs in again once its owner proves it here.
		if holder.Verified {
			return nil
		}
		holder.Verified = true
		if err := s.deps.UserAuth.UpdateUserAuthMethods(ctx, holder); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "verify %s identity", req.Method)
		}
		return s.clearCache(ctx, current)
	case err == nil:
		return xerr.Errorf(xerr.UserExist, "the %s identity belongs to another account", req.Method)
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find %s identity", req.Method)
	}
	if _, err := s.deps.UserAuth.FindUserAuthMethodByPlatform(ctx, current.Id, req.Method); err == nil {
		return xerr.Errorf(xerr.UserExist, "the account already has a %s identity", req.Method)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find the account's %s identity", req.Method)
	}

	if err := s.deps.UserAuth.InsertUserAuthMethods(ctx, &user.AuthMethods{
		UserId:         current.Id,
		AuthType:       req.Method,
		AuthIdentifier: identity.Subject,
		Verified:       true,
	}); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "bind %s identity", req.Method)
	}
	return s.clearCache(ctx, current)
}

// clearCache drops the account's cached view, which lists its identities.
func (s *Service) clearCache(ctx context.Context, current *user.User) error {
	if err := s.deps.UserCache.ClearUserCache(ctx, current); err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "clear user cache")
	}
	return nil
}
