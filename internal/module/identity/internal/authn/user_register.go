package authn

import (
	"context"
	"errors"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	"github.com/perfect-panel/backend/internal/auth/password"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authn/registerpolicy"
	"github.com/perfect-panel/backend/internal/module/identity/internal/verification"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// UserRegister creates an account that signs in with an email address and
// password, and signs it in.
func (s *Service) UserRegister(ctx context.Context, req *dto.UserRegisterRequest) (resp *dto.LoginResponse, err error) {
	cfg := s.deps.Config()
	email, err := identifier.ValidateEmail(req.Email, cfg.EmailDomainSuffixList, cfg.EmailEnableDomainSuffix)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.InvalidParams, "invalid email")
	}
	if err := s.policy.EnsureRegistrationOpen(ctx, identifier.Email); err != nil {
		return nil, err
	}
	if err := s.policy.VerifyHuman(ctx, registerpolicy.Register, req.CfToken); err != nil {
		return nil, err
	}
	referer, err := s.resolveReferer(ctx, req.Invite)
	if err != nil {
		return nil, err
	}
	codeKey := verification.EmailCodeKey(auth.Register, email)
	if cfg.EmailVerifyEnabled {
		if err := verification.ValidateVerificationCode(ctx, s.deps.Redis, codeKey, req.Code, false); err != nil {
			return nil, xerr.Wrapf(err, xerr.VerifyCodeError, "check registration code")
		}
	}
	existing, err := s.deps.Store.User().FindOneByEmail(ctx, email)
	switch {
	case err == nil && existing.DeletedAt.Valid:
		return nil, xerr.Errorf(xerr.UserDisabled, "the email belongs to a deleted account")
	case err == nil:
		return nil, xerr.Errorf(xerr.UserExist, "the email is registered")
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find user by email")
	}
	// One inbox must not open many accounts (and trials): "a.b+x@gmail.com"
	// reaches the mailbox of an existing "ab@gmail.com".
	if _, err := s.deps.Store.UserAuth().FindEmailAlias(ctx, email); err == nil {
		return nil, xerr.Errorf(xerr.UserExist, "the email reaches the mailbox of an existing account")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find email aliases")
	}
	if err := s.policy.TakeIPPermit(ctx); err != nil {
		return nil, err
	}
	if cfg.EmailVerifyEnabled {
		if err := verification.ValidateVerificationCode(ctx, s.deps.Redis, codeKey, req.Code, true); err != nil {
			return nil, xerr.Wrapf(err, xerr.VerifyCodeError, "check registration code")
		}
	}

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
			{AuthType: identifier.Email, AuthIdentifier: email, Verified: cfg.EmailVerifyEnabled},
		},
	}, identifier.Email); err != nil {
		return nil, err
	}
	return s.signInRegistered(ctx, newUser.Id, identifier.Email, req.Identifier)
}

// signInRegistered signs a newly registered account in, auditing the sign-in.
func (s *Service) signInRegistered(ctx context.Context, userID int64, method, deviceIdentifier string) (resp *dto.LoginResponse, err error) {
	attempt := account.NewAttempt(s.deps.Store.Log(), method)
	attempt.Identify(userID)
	defer func() {
		if err = attempt.Finish(ctx, err); err != nil {
			resp = nil
		}
	}()
	epoch, err := account.ReadEpoch(ctx, s.deps.Redis, userID)
	if err != nil {
		return nil, err
	}
	return s.signIn(ctx, userID, epoch, deviceIdentifier)
}

// resolveReferer returns the account whose invite code a registration
// names, or nil for none; an invite is required when invites are forced.
func (s *Service) resolveReferer(ctx context.Context, invite string) (*user.User, error) {
	if invite == "" {
		if s.deps.Config().InviteForced {
			return nil, xerr.Errorf(xerr.InviteCodeError, "invite code is required")
		}
		return nil, nil
	}
	referer, err := s.deps.Store.User().FindOneByReferCode(ctx, invite)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.InviteCodeError, "find invite code")
	}
	return referer, nil
}
