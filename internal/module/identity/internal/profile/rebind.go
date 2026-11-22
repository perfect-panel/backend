package profile

import (
	"context"
	"errors"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/auth/usersession"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/verification"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// rebinding is the calling account's request to bind a new address of one
// type, replacing the address of that type it holds, if any.
type rebinding struct {
	// authType is the binding type (identifier.Email or identifier.Mobile);
	// address is the new address in its stored form.
	authType, address string
	// code is the register code sent to the new address, held under codeKey.
	code, codeKey string
	// password and currentCode are the request's proof of the current
	// credential.
	password, currentCode string
}

// securityCodeKey is the key of the security code sent to address, a bound
// email or phone number in its stored form.
func securityCodeKey(authType, address string) string {
	if authType == identifier.Mobile {
		return verification.MobileCodeKey(auth.Security, address)
	}
	return verification.EmailCodeKey(auth.Security, address)
}

// rebind binds the new address to the account. The new address becomes a
// login identifier, so its owner proves control of it with the code sent
// there. A session alone must not be enough to move the account to an
// address its holder controls, so replacing a bound address also proves the
// current credential (proveCurrentCredential) and ends every session of the
// account, the caller's included, as a password change does. A first binding
// replaces nothing and needs neither.
func (s *Service) rebind(ctx context.Context, u *user.User, r rebinding) error {
	if err := verification.ValidateVerificationCode(ctx, s.deps.Redis, r.codeKey, r.code, false); err != nil {
		return xerr.Wrapf(err, xerr.VerifyCodeError, "check verification code")
	}
	holder, err := s.deps.UserAuth.FindUserAuthMethodByOpenID(ctx, r.authType, r.address)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find identity")
	}
	if holder.Id > 0 {
		return xerr.Errorf(xerr.UserExist, "the %s is bound to an account", r.authType)
	}
	current, err := s.deps.UserAuth.FindUserAuthMethodByUserId(ctx, r.authType, u.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find identity")
	}
	replacing := current.Id > 0
	var proofKey string
	if replacing {
		if proofKey, err = s.proveCurrentCredential(ctx, u, r, current.AuthIdentifier); err != nil {
			return err
		}
	}
	if err := verification.ValidateVerificationCode(ctx, s.deps.Redis, r.codeKey, r.code, true); err != nil {
		return xerr.Wrapf(err, xerr.VerifyCodeError, "check verification code")
	}
	if !replacing {
		if err := s.deps.UserAuth.InsertUserAuthMethods(ctx, &user.AuthMethods{
			UserId:         u.Id,
			AuthType:       r.authType,
			AuthIdentifier: r.address,
			Verified:       true,
		}); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseInsertError, "bind identity")
		}
		return nil
	}
	if proofKey != "" {
		if err := verification.ValidateVerificationCode(ctx, s.deps.Redis, proofKey, r.currentCode, true); err != nil {
			return xerr.Wrapf(err, xerr.VerifyCodeError, "check the current %s code", r.authType)
		}
	}
	current.Verified = true
	current.AuthIdentifier = r.address
	if err := s.deps.UserAuth.UpdateUserAuthMethods(ctx, current); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update identity")
	}
	if err := usersession.Revoke(ctx, s.deps.Redis, u.Id); err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "revoke sessions of user %d", u.Id)
	}
	return nil
}

// proveCurrentCredential checks the proof a replacement carries: the current
// password when the account has one, counted against the account's password
// guess limit like a sign-in, or else the security code sent to the address
// being replaced. That code is checked but not spent; the key it is held
// under is returned so rebind spends it once the replacement goes through.
func (s *Service) proveCurrentCredential(ctx context.Context, u *user.User, r rebinding, current string) (string, error) {
	if u.Password != "" {
		if r.password == "" {
			return "", xerr.Errorf(xerr.InvalidParams, "the current password is required to replace the bound %s", r.authType)
		}
		return "", s.checkPassword(ctx, u, r.password)
	}
	if r.currentCode == "" {
		return "", xerr.Errorf(xerr.InvalidParams, "the security code sent to the current %s is required to replace it", r.authType)
	}
	key := securityCodeKey(r.authType, current)
	if err := verification.ValidateVerificationCode(ctx, s.deps.Redis, key, r.currentCode, false); err != nil {
		return "", xerr.Wrapf(err, xerr.VerifyCodeError, "check the current %s code", r.authType)
	}
	return key, nil
}

// checkPassword compares plain with the account's current password. The
// attempt counts against the account's password guess limit, the one sign-in
// uses, and is reserved before the comparison, so a session cannot be used
// to guess the password without limit; a correct password clears the
// attempts.
func (s *Service) checkPassword(ctx context.Context, u *user.User, plain string) error {
	if err := account.ReservePasswordAttempt(ctx, s.deps.Redis, u.Id); err != nil {
		return err
	}
	if !password.MultiPasswordVerify(u.Algo, u.Salt, plain, u.Password) {
		return xerr.Errorf(xerr.UserPasswordError, "current password is incorrect")
	}
	account.ClearPasswordAttempts(ctx, s.deps.Redis, u.Id)
	return nil
}
