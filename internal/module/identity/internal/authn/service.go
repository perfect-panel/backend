// Package authn implements the authentication subdomain of the identity
// module: account existence checks, password, code and device sign-in,
// registration, password resets and, through its oauth subpackage, OAuth
// sign-in. Every flow reads the client address and user agent from the
// request metadata.
package authn

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authn/oauth"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authn/registerpolicy"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthflow"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
)

// Snapshot is the per-request view of every runtime-mutable setting the
// authentication flows consume.
type Snapshot struct {
	JWTAccessSecret string
	JWTAccessExpire int64

	EmailEnabled            bool
	EmailVerifyEnabled      bool
	EmailDomainSuffixList   string
	EmailEnableDomainSuffix bool
	MobileEnabled           bool
	DeviceEnabled           bool
	DeviceOnlyReal          bool

	InviteForced      bool
	OnlyFirstPurchase bool

	StopRegister bool
	// RegisterVerify, LoginVerify and ResetPasswordVerify switch the
	// Turnstile challenge on for registration, sign-in and password reset.
	RegisterVerify          bool
	LoginVerify             bool
	ResetPasswordVerify     bool
	TurnstileSecret         string
	EnableIpRegisterLimit   bool
	IpRegisterLimit         int64
	IpRegisterLimitDuration int64

	// SiteHost anchors the OAuth redirect allowlist and is the fallback
	// redirect target for the Apple form-post callback.
	SiteHost string
}

func (s Snapshot) sessions() account.SessionConfig {
	return account.SessionConfig{Secret: s.JWTAccessSecret, Lifetime: s.JWTAccessExpire}
}

func (s Snapshot) policy() registerpolicy.Snapshot {
	return registerpolicy.Snapshot{
		EmailEnabled:            s.EmailEnabled,
		MobileEnabled:           s.MobileEnabled,
		DeviceEnabled:           s.DeviceEnabled,
		StopRegister:            s.StopRegister,
		RegisterVerify:          s.RegisterVerify,
		LoginVerify:             s.LoginVerify,
		ResetPasswordVerify:     s.ResetPasswordVerify,
		TurnstileSecret:         s.TurnstileSecret,
		EnableIpRegisterLimit:   s.EnableIpRegisterLimit,
		IpRegisterLimit:         s.IpRegisterLimit,
		IpRegisterLimitDuration: s.IpRegisterLimitDuration,
	}
}

// Deps declares the subdomain's dependencies; the identity facade forwards
// them from the composition root.
type Deps struct {
	Store Store
	Redis *redis.Client
	// Config snapshots the runtime-mutable settings per request.
	Config func() Snapshot
	// OAuth runs the OAuth provider round trips; built from Store and Redis
	// when nil.
	OAuth *oauthflow.Flow
	// VerifyTurnstile overrides the Cloudflare Turnstile client; nil selects
	// it.
	VerifyTurnstile registerpolicy.TurnstileVerifier
	// NotifyPasswordChanged tells the account, best effort, that its
	// password was reset and which third-party sign-in methods stay bound;
	// optional.
	NotifyPasswordChanged func(ctx context.Context, userID int64, bindings []string) error
}

// Service is the authentication subdomain entry point used by the identity
// facade.
type Service struct {
	deps   Deps
	policy registerpolicy.Policy
	oauth  *oauth.Service
}

// NewService builds the subdomain with its account policy and, unless Deps
// carries one, its OAuth round trip.
func NewService(deps Deps) *Service {
	policy := registerpolicy.New(registerpolicy.Deps{
		Auths:           deps.Store.Auth(),
		Redis:           deps.Redis,
		Config:          func() registerpolicy.Snapshot { return deps.Config().policy() },
		VerifyTurnstile: deps.VerifyTurnstile,
	})
	if deps.OAuth == nil {
		deps.OAuth = oauthflow.New(oauthflow.Deps{
			Auths:    deps.Store.Auth(),
			Redis:    deps.Redis,
			SiteHost: func() string { return deps.Config().SiteHost },
		})
	}
	return &Service{
		deps:   deps,
		policy: policy,
		oauth: oauth.NewService(oauth.Deps{
			Store:  deps.Store,
			Redis:  deps.Redis,
			Policy: policy,
			Flow:   deps.OAuth,
			Config: func() oauth.Config {
				cfg := deps.Config()
				return oauth.Config{
					InviteForced:            cfg.InviteForced,
					OnlyFirstPurchase:       cfg.OnlyFirstPurchase,
					EmailDomainSuffixList:   cfg.EmailDomainSuffixList,
					EmailEnableDomainSuffix: cfg.EmailEnableDomainSuffix,
					Sessions:                cfg.sessions(),
					SiteHost:                cfg.SiteHost,
				}
			},
		}),
	}
}

// Policy exposes the account policy to sibling subdomains (the profile and
// verification-code flows gate on the same switches).
func (s *Service) Policy() registerpolicy.Policy { return s.policy }

func (s *Service) OAuthLogin(ctx context.Context, req *dto.OAuthLoginRequest) (*dto.OAuthLoginResponse, error) {
	return s.oauth.OAuthLogin(ctx, req)
}

func (s *Service) OAuthLoginGetToken(ctx context.Context, req *dto.OAuthLoginGetTokenRequest) (*dto.LoginResponse, error) {
	return s.oauth.OAuthLoginGetToken(ctx, req)
}

func (s *Service) AppleLoginCallback(ctx context.Context, req *dto.AppleLoginCallbackRequest) (*oauth.AppleLoginRedirect, error) {
	return s.oauth.AppleLoginCallback(ctx, req)
}

// Store is the persistence capability required by this package. It excludes
// unrelated repositories and application-wide transactions.
type Store interface {
	Auth() repository.AuthRepo
	repository.IdentityTransactor
	Log() repository.LogRepo
	User() repository.UserRepo
	UserAuth() repository.UserAuthRepo
	UserDevice() repository.UserDeviceRepo
}
