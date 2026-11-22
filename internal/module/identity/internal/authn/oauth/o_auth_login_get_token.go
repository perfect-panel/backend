package oauth

import (
	"context"
	"errors"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authn/registerpolicy"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthprovider"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthstate"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// OAuthLoginGetToken completes a sign-in through req.Method: the callback
// signs in the account holding the identity the provider vouches for, or
// registers one when none does.
func (s *Service) OAuthLoginGetToken(ctx context.Context, req *dto.OAuthLoginGetTokenRequest) (resp *dto.LoginResponse, err error) {
	if err := s.deps.Policy.EnsureMethodEnabled(ctx, req.Method); err != nil {
		return nil, err
	}
	fields, ok := req.Callback.(map[string]any)
	if !ok {
		return nil, xerr.Errorf(xerr.InvalidParams, "OAuth callback must be an object")
	}
	attempt := account.NewAttempt(s.deps.Store.Log(), req.Method)
	defer func() {
		if err = attempt.Finish(ctx, err); err != nil {
			resp = nil
		}
	}()

	identity, err := s.deps.Flow.Identify(ctx, oauthstate.LoginScope(), req.Method, fields, req.Nonce)
	if err != nil {
		return nil, err
	}
	userInfo, err := s.findOrRegister(ctx, req, identity)
	if err != nil {
		return nil, err
	}
	attempt.Identify(userInfo.Id)
	if err := account.EnsureActive(userInfo); err != nil {
		return nil, err
	}
	token, err := account.IssueSession(ctx, s.deps.Redis, s.deps.Config().Sessions, account.Login{UserID: userInfo.Id})
	if err != nil {
		return nil, err
	}
	return &dto.LoginResponse{Token: token}, nil
}

// findOrRegister returns the account holding the identity, registering one
// when none does.
func (s *Service) findOrRegister(ctx context.Context, req *dto.OAuthLoginGetTokenRequest, identity *oauthprovider.Identity) (*user.User, error) {
	binding, err := s.deps.Store.UserAuth().FindUserAuthMethodByOpenID(ctx, req.Method, identity.Subject)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.register(ctx, req, identity)
	}
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find %s identity", req.Method)
	}
	// Provider sign-in and binding always store verified identities. An
	// unverified one was asserted by someone else (for example a guest
	// checkout naming this provider id), so it must not open that account to
	// whoever proves the identity now.
	if !binding.Verified {
		logger.WithContext(ctx).Errorw("refusing sign-in through an unverified identity",
			logger.Field("auth_type", req.Method), logger.Field("user_id", binding.UserId))
		return nil, xerr.Errorf(xerr.UserExist, "%s identity is bound to an account that never verified it", req.Method)
	}
	userInfo, err := s.deps.Store.User().FindOne(ctx, binding.UserId)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find user %d", binding.UserId)
	}
	return userInfo, nil
}

// register creates the account of an identity no account holds. A verified
// email the provider reports is bound too, unless another account has it.
func (s *Service) register(ctx context.Context, req *dto.OAuthLoginGetTokenRequest, identity *oauthprovider.Identity) (*user.User, error) {
	method := req.Method
	if err := s.deps.Policy.EnsureRegistrationOpen(ctx, method); err != nil {
		return nil, err
	}
	if err := s.deps.Policy.VerifyHuman(ctx, registerpolicy.Register, req.CfToken); err != nil {
		return nil, err
	}
	cfg := s.deps.Config()
	referer, err := s.resolveReferer(ctx, cfg, req.Invite)
	if err != nil {
		return nil, err
	}
	email := identity.Email
	if email != "" {
		if email, err = identifier.ValidateEmail(email, cfg.EmailDomainSuffixList, cfg.EmailEnableDomainSuffix); err != nil {
			return nil, xerr.Wrapf(err, xerr.InvalidParams, "OAuth email is not allowed")
		}
	}
	if err := s.deps.Policy.TakeIPPermit(ctx); err != nil {
		return nil, err
	}
	identities := []user.AuthMethods{{AuthType: method, AuthIdentifier: identity.Subject, Verified: true}}
	if email != "" {
		existing, err := s.deps.Store.User().FindOneByEmail(ctx, email)
		switch {
		case err == nil && existing.Id != 0:
			return nil, xerr.Errorf(xerr.UserExist, "the %s email is registered", method)
		case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
			return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find user by email")
		}
		identities = append(identities, user.AuthMethods{AuthType: identifier.Email, AuthIdentifier: email, Verified: true})
	}

	newUser := &user.User{Avatar: identity.Avatar, OnlyFirstPurchase: &cfg.OnlyFirstPurchase}
	if referer != nil {
		newUser.RefererId = referer.Id
	}
	if err := account.Register(ctx, s.deps.Store, account.New{User: newUser, Identities: identities}, method); err != nil {
		return nil, err
	}
	return newUser, nil
}

func (s *Service) resolveReferer(ctx context.Context, cfg Config, invite string) (*user.User, error) {
	if invite == "" {
		if cfg.InviteForced {
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
