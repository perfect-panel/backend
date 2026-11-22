package authn

import (
	"context"
	"errors"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authn/registerpolicy"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// UserLogin signs in with an email address and password.
func (s *Service) UserLogin(ctx context.Context, req *dto.UserLoginRequest) (resp *dto.LoginResponse, err error) {
	if err := s.policy.VerifyHuman(ctx, registerpolicy.Login, req.CfToken); err != nil {
		return nil, err
	}
	if err := s.policy.EnsureMethodEnabled(ctx, identifier.Email); err != nil {
		return nil, err
	}
	email, err := identifier.ValidateEmail(req.Email, "", false)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.InvalidParams, "invalid email")
	}
	attempt := account.NewAttempt(s.deps.Store.Log(), identifier.Email)
	defer func() {
		if err = attempt.Finish(ctx, err); err != nil {
			resp = nil
		}
	}()

	userInfo, err := s.deps.Store.User().FindOneByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, xerr.Errorf(xerr.UserNotExist, "no account has this email")
		}
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find user by email")
	}
	attempt.Identify(userInfo.Id)
	if userInfo.DeletedAt.Valid {
		return nil, xerr.Errorf(xerr.UserNotExist, "user %d is deleted", userInfo.Id)
	}

	// The epoch is read before the password check, so a reset that lands
	// while the password is checked ends this sign-in too.
	epoch, err := account.ReadEpoch(ctx, s.deps.Redis, userInfo.Id)
	if err != nil {
		return nil, err
	}
	if err := s.checkPassword(ctx, userInfo, req.Password); err != nil {
		return nil, err
	}
	// The account state is only revealed to the owner of the password.
	if err := account.EnsureActive(userInfo); err != nil {
		return nil, err
	}
	upgradePasswordAfterLogin(ctx, s.deps.Store.User(), userInfo, req.Password)

	return s.signIn(ctx, userInfo.Id, epoch, req.Identifier)
}
