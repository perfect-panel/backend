package authn

import (
	"context"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	"github.com/perfect-panel/backend/internal/auth/password"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authn/registerpolicy"
	"github.com/perfect-panel/backend/internal/module/identity/internal/verification"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// TelephoneUserRegister creates an account that signs in with a phone
// number, proven by a registration code sent to it, and signs it in.
func (s *Service) TelephoneUserRegister(ctx context.Context, req *dto.TelephoneRegisterRequest) (resp *dto.LoginResponse, err error) {
	if err := s.policy.EnsureRegistrationOpen(ctx, identifier.Mobile); err != nil {
		return nil, err
	}
	if err := s.policy.VerifyHuman(ctx, registerpolicy.Register, req.CfToken); err != nil {
		return nil, err
	}
	if !identifier.Check(req.TelephoneAreaCode, req.Telephone) {
		return nil, xerr.Errorf(xerr.TelephoneError, "telephone number is not valid")
	}
	phoneNumber, err := identifier.FormatToE164(req.TelephoneAreaCode, req.Telephone)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.TelephoneError, "invalid phone number")
	}
	codeKey := verification.MobileCodeKey(auth.Register, phoneNumber)
	if err := verification.ValidateVerificationCode(ctx, s.deps.Redis, codeKey, req.Code, false); err != nil {
		return nil, xerr.Wrapf(err, xerr.VerifyCodeError, "check registration code")
	}
	exist, err := s.identityExists(ctx, identifier.Mobile, phoneNumber)
	if err != nil {
		return nil, err
	}
	if exist {
		return nil, xerr.Errorf(xerr.UserExist, "telephone already exists")
	}
	referer, err := s.resolveReferer(ctx, req.Invite)
	if err != nil {
		return nil, err
	}
	if err := s.policy.TakeIPPermit(ctx); err != nil {
		return nil, err
	}
	if err := verification.ValidateVerificationCode(ctx, s.deps.Redis, codeKey, req.Code, true); err != nil {
		return nil, xerr.Wrapf(err, xerr.VerifyCodeError, "check registration code")
	}

	cfg := s.deps.Config()
	newUser := &user.User{
		Password:          password.EncodePassWord(req.Password),
		Algo:              password.PasswordAlgoArgon2id,
		OnlyFirstPurchase: &cfg.OnlyFirstPurchase,
	}
	if referer != nil {
		newUser.RefererId = referer.Id
	}
	if err := account.Register(ctx, s.deps.Store, account.New{
		User: newUser,
		Identities: []user.AuthMethods{
			{AuthType: identifier.Mobile, AuthIdentifier: phoneNumber, Verified: true},
		},
	}, identifier.Mobile); err != nil {
		return nil, err
	}
	return s.signInRegistered(ctx, newUser.Id, identifier.Mobile, req.Identifier)
}
