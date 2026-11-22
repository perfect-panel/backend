package profile

import (
	"context"
	"errors"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/auth/usersession"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/verification"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// UpdateUserPassword sets the calling account's password and ends its
// sessions. A session alone must not be enough to take the account over for
// good: changing an existing password proves the current one, and setting
// the first password of an account that has an email or phone number bound
// proves that address with a security code sent to it, so a stolen session
// cannot bootstrap a password and then use it to move the account. An
// account with neither a password nor a bound address (OAuth or device only)
// has nothing else to prove and sets its password with the session.
//
// The change is audited in the account's login history and the account is
// told about it, with the third-party sign-in methods still bound to it: a
// binding made during a compromise keeps signing in until its owner removes
// it, so the response names them for the client to show.
func (s *Service) UpdateUserPassword(ctx context.Context, req *dto.UpdateUserPasswordRequest) (*dto.UpdateUserPasswordResponse, error) {
	userInfo, ok := user.FromContext(ctx)
	if !ok {
		return nil, xerr.Errorf(xerr.InvalidAccess, "Invalid Access")
	}
	var proofKey string
	if userInfo.Password != "" {
		// The current password, under the same guess limit as sign-in.
		if err := s.checkPassword(ctx, userInfo, req.OldPassword); err != nil {
			return nil, err
		}
	} else {
		var err error
		if proofKey, err = s.proveBoundAddress(ctx, userInfo.Id, req.CurrentCode); err != nil {
			return nil, err
		}
	}
	if proofKey != "" {
		if err := verification.ValidateVerificationCode(ctx, s.deps.Redis, proofKey, req.CurrentCode, true); err != nil {
			return nil, xerr.Wrapf(err, xerr.VerifyCodeError, "check the bound address code")
		}
	}
	// The new hash always uses the current algorithm; a migrated user would
	// otherwise keep verifying it with the old legacy algorithm.
	if err := s.deps.Users.UpdateColumns(ctx, userInfo.Id, password.UserColumns(req.Password)); err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseUpdateError, "update password of user %d", userInfo.Id)
	}
	// Every session from before the change ends, including a stolen one.
	if err := usersession.Revoke(ctx, s.deps.Redis, userInfo.Id); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "revoke sessions of user %d", userInfo.Id)
	}
	if err := account.RecordCredentialChange(ctx, s.deps.Logs, userInfo.Id, account.PasswordChange); err != nil {
		return nil, err
	}
	bindings, err := s.thirdPartyBindings(ctx, userInfo.Id)
	if err != nil {
		return nil, err
	}
	account.NotifyPasswordChanged(ctx, s.deps.NotifyPasswordChanged, userInfo.Id, bindings)
	return &dto.UpdateUserPasswordResponse{ThirdPartyBindings: bindings}, nil
}

// thirdPartyBindings lists the types of the third-party sign-in methods
// bound to the account; none without the bindings wired (a test double).
func (s *Service) thirdPartyBindings(ctx context.Context, userID int64) ([]string, error) {
	if s.deps.UserAuth == nil {
		return []string{}, nil
	}
	methods, err := s.deps.UserAuth.FindUserAuthMethods(ctx, userID)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "list the bindings of user %d", userID)
	}
	return account.ThirdPartyBindings(methods), nil
}

// proveBoundAddress checks code against the security codes sent to the
// account's bound email and phone number and returns the key of the code it
// matches, for the caller to spend once the change goes through. An account
// with neither bound has nothing to prove and gets no key. Wrong guesses
// count against each code's own guess limit, which deletes the code.
func (s *Service) proveBoundAddress(ctx context.Context, userID int64, code string) (string, error) {
	keys, err := s.boundAddressKeys(ctx, userID)
	if err != nil || len(keys) == 0 {
		return "", err
	}
	if code == "" {
		return "", xerr.Errorf(xerr.InvalidParams, "the security code sent to the bound email or phone number is required to set the first password")
	}
	var lastErr error
	for _, key := range keys {
		if lastErr = verification.ValidateVerificationCode(ctx, s.deps.Redis, key, code, false); lastErr == nil {
			return key, nil
		}
	}
	return "", xerr.Wrapf(lastErr, xerr.VerifyCodeError, "check the bound address code")
}

// boundAddressKeys returns the keys of the security codes sent to the
// account's bound email and phone number, in that order; none for an
// account with neither.
func (s *Service) boundAddressKeys(ctx context.Context, userID int64) ([]string, error) {
	var keys []string
	for _, authType := range []string{identifier.Email, identifier.Mobile} {
		method, err := s.deps.UserAuth.FindUserAuthMethodByUserId(ctx, authType, userID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find %s identity", authType)
		}
		keys = append(keys, securityCodeKey(authType, method.AuthIdentifier))
	}
	return keys, nil
}
