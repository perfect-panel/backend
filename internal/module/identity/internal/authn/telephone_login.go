package authn

import (
	"context"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authn/registerpolicy"
	"github.com/perfect-panel/backend/internal/module/identity/internal/verification"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// TelephoneLogin signs in with a phone number and either its password or a
// security code sent to the number.
func (s *Service) TelephoneLogin(ctx context.Context, req *dto.TelephoneLoginRequest) (resp *dto.LoginResponse, err error) {
	if err := s.policy.VerifyHuman(ctx, registerpolicy.Login, req.CfToken); err != nil {
		return nil, err
	}
	if err := s.policy.EnsureMethodEnabled(ctx, identifier.Mobile); err != nil {
		return nil, err
	}
	phoneNumber, err := identifier.FormatToE164(req.TelephoneAreaCode, req.Telephone)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.TelephoneError, "invalid phone number")
	}
	attempt := account.NewAttempt(s.deps.Store.Log(), identifier.Mobile)
	defer func() {
		if err = attempt.Finish(ctx, err); err != nil {
			resp = nil
		}
	}()

	if req.Password == "" && req.TelephoneCode == "" {
		return nil, xerr.NewErrCodeMsg(xerr.InvalidParams, "password and telephone code is empty")
	}
	userInfo, err := s.findAccount(ctx, identifier.Mobile, phoneNumber)
	if err != nil {
		return nil, err
	}
	attempt.Identify(userInfo.Id)

	// The epoch is read before the credential check, so a reset that lands
	// while the credential is checked ends this sign-in too.
	epoch, err := account.ReadEpoch(ctx, s.deps.Redis, userInfo.Id)
	if err != nil {
		return nil, err
	}
	if req.TelephoneCode == "" {
		if err := s.checkPassword(ctx, userInfo, req.Password); err != nil {
			return nil, err
		}
	} else {
		key := verification.MobileCodeKey(auth.Security, phoneNumber)
		if err := verification.ValidateVerificationCode(ctx, s.deps.Redis, key, req.TelephoneCode, true); err != nil {
			return nil, xerr.Wrapf(err, xerr.VerifyCodeError, "check sign-in code")
		}
	}
	// The account state is only revealed to the owner of the credential, as
	// the email sign-in does: whether a number's account is disabled or
	// deleted is not for anyone who knows the number.
	if err := account.EnsureActive(userInfo); err != nil {
		return nil, err
	}
	if req.TelephoneCode == "" {
		upgradePasswordAfterLogin(ctx, s.deps.Store.User(), userInfo, req.Password)
	}

	return s.signIn(ctx, userInfo.Id, epoch, req.Identifier)
}
