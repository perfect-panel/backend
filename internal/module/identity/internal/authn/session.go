package authn

import (
	"context"
	"errors"

	"github.com/perfect-panel/backend/internal/auth/password"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// verifyPassword compares a password with the account's stored hash. It is
// a variable so tests can interleave work with the check.
var verifyPassword = password.MultiPasswordVerify

// checkPassword compares plain with the account's password, counting the
// attempt against the account's password guess limit: the attempt is
// reserved before the comparison, so concurrent guesses cannot exceed the
// limit, and a correct password clears the attempts.
func (s *Service) checkPassword(ctx context.Context, userInfo *user.User, plain string) error {
	if err := account.ReservePasswordAttempt(ctx, s.deps.Redis, userInfo.Id); err != nil {
		return err
	}
	if !verifyPassword(userInfo.Algo, userInfo.Salt, plain, userInfo.Password) {
		return xerr.Errorf(xerr.UserPasswordError, "wrong password")
	}
	account.ClearPasswordAttempts(ctx, s.deps.Redis, userInfo.Id)
	return nil
}

// signIn ends a successful sign-in: it binds the requested device, if any,
// and issues the session. epoch is the account's session epoch the flow read
// before it checked the credential, so a revocation that overtook the check
// refuses the session. The session's login type comes from the request
// context, where the device transport marks device sign-ins.
func (s *Service) signIn(ctx context.Context, userID int64, epoch, deviceIdentifier string) (*dto.LoginResponse, error) {
	device, err := s.bindLoginDevice(ctx, deviceIdentifier, userID)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.InvalidAccess, "bind device")
	}
	token, err := account.IssueSession(ctx, s.deps.Redis, s.deps.Config().sessions(), account.Login{UserID: userID, Epoch: epoch, Device: device})
	if err != nil {
		return nil, err
	}
	return &dto.LoginResponse{Token: token}, nil
}

// bindLoginDevice binds the device a sign-in names to the account; a sign-in
// naming none binds nothing.
func (s *Service) bindLoginDevice(ctx context.Context, identifier string, userID int64) (*user.Device, error) {
	if identifier == "" {
		return nil, nil
	}
	device, err := s.BindDeviceToUser(ctx, identifier, userID)
	if err != nil {
		return nil, err
	}
	if device == nil || device.Id <= 0 || device.UserId != userID || !device.Enabled {
		return nil, xerr.Errorf(xerr.InvalidAccess, "invalid device binding")
	}
	return device, nil
}

// findAccount returns the account an identity of authType signs in to.
func (s *Service) findAccount(ctx context.Context, authType, identifier string) (*user.User, error) {
	method, err := s.deps.Store.UserAuth().FindUserAuthMethodByOpenID(ctx, authType, identifier)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, xerr.Errorf(xerr.UserNotExist, "no account has this %s identity", authType)
		}
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find %s identity", authType)
	}
	userInfo, err := s.deps.Store.User().FindOne(ctx, method.UserId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, xerr.Errorf(xerr.UserNotExist, "user %d of the %s identity does not exist", method.UserId, authType)
		}
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find user %d", method.UserId)
	}
	return userInfo, nil
}
