// Package registerpolicy enforces the administrator-configured account
// policies shared by every authentication and registration path.
package registerpolicy

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/perfect-panel/backend/internal/auth/challenge"
	"github.com/perfect-panel/backend/internal/auth/identifier"
	"github.com/perfect-panel/backend/internal/auth/ratelimit"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

// The built-in sign-in methods; every other method is an OAuth provider
// configured in the auth_method table.
const (
	MethodEmail  = identifier.Email
	MethodMobile = identifier.Mobile
	MethodDevice = identifier.Device
)

// Policy is the account policy every identity flow applies: which sign-in
// methods are enabled, whether registration is open, the human check and the
// per-IP registration quota. The client address is the request's, from the
// request metadata.
type Policy interface {
	// EnsureMethodEnabled rejects a sign-in method the administrator
	// disabled.
	EnsureMethodEnabled(ctx context.Context, method string) error
	// EnsureRegistrationOpen rejects a new account when registration is
	// stopped or its method disabled.
	EnsureRegistrationOpen(ctx context.Context, method string) error
	// VerifyHuman enforces the Turnstile challenge configured for purpose.
	VerifyHuman(ctx context.Context, purpose Purpose, token string) error
	// TakeIPPermit reserves one registration from the client's IP quota.
	TakeIPPermit(ctx context.Context) error
}

// Purpose selects which configured Turnstile switch guards a request.
type Purpose int

const (
	// Register guards new accounts.
	Register Purpose = iota
	// Login guards password and code sign-ins.
	Login
	// Reset guards password resets.
	Reset
)

// Snapshot is the per-request view of the runtime-mutable policy settings.
type Snapshot struct {
	EmailEnabled  bool
	MobileEnabled bool
	DeviceEnabled bool

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
}

// TurnstileVerifier checks a Turnstile response token.
type TurnstileVerifier func(ctx context.Context, secret, token, remoteIP string) (bool, error)

// Deps declares the policy's collaborators; the identity facade provides
// them.
type Deps struct {
	Auths repository.AuthRepo
	Redis *redis.Client
	// Config snapshots the runtime-mutable policy settings per call.
	Config func() Snapshot
	// VerifyTurnstile overrides the Cloudflare client; nil selects it.
	VerifyTurnstile TurnstileVerifier
}

// ServicePolicy implements Policy.
type ServicePolicy struct {
	deps Deps
}

var _ Policy = ServicePolicy{}

// New builds the policy; without a VerifyTurnstile override it asks
// Cloudflare.
func New(deps Deps) ServicePolicy {
	if deps.VerifyTurnstile == nil {
		deps.VerifyTurnstile = verifyTurnstile
	}
	return ServicePolicy{deps: deps}
}

func verifyTurnstile(ctx context.Context, secret, token, remoteIP string) (bool, error) {
	return challenge.New(challenge.Config{Secret: secret, Timeout: 3 * time.Second}).Verify(ctx, token, remoteIP)
}

// EnsureMethodEnabled rejects direct calls to authentication methods disabled
// by the administrator. OAuth methods are loaded from the auth_method table.
func (p ServicePolicy) EnsureMethodEnabled(ctx context.Context, method string) error {
	cfg := p.deps.Config()
	switch method {
	case MethodEmail:
		if cfg.EmailEnabled {
			return nil
		}
	case MethodMobile:
		if cfg.MobileEnabled {
			return nil
		}
	case MethodDevice:
		if cfg.DeviceEnabled {
			return nil
		}
	default:
		configured, err := p.deps.Auths.FindOneByMethod(ctx, method)
		if err != nil {
			return xerr.Wrapf(err, xerr.GetAuthenticatorError, "load auth method %q", method)
		}
		if configured.Enabled != nil && *configured.Enabled {
			return nil
		}
	}
	return xerr.Errorf(xerr.GetAuthenticatorError, "auth method %q is disabled", method)
}

// EnsureRegistrationOpen applies policies shared by every new-account path.
func (p ServicePolicy) EnsureRegistrationOpen(ctx context.Context, method string) error {
	if p.deps.Config().StopRegister {
		return xerr.Errorf(xerr.StopRegister, "registration is disabled")
	}
	return p.EnsureMethodEnabled(ctx, method)
}

// VerifyHuman enforces the Turnstile challenge configured for purpose. A
// missing token or secret fails without asking Cloudflare.
func (p ServicePolicy) VerifyHuman(ctx context.Context, purpose Purpose, token string) error {
	cfg := p.deps.Config()
	enabled := cfg.RegisterVerify
	switch purpose {
	case Login:
		enabled = cfg.LoginVerify
	case Reset:
		enabled = cfg.ResetPasswordVerify
	}
	if !enabled {
		return nil
	}
	refused := xerr.NewErrCode(xerr.TooManyRequests)
	if strings.TrimSpace(token) == "" || strings.TrimSpace(cfg.TurnstileSecret) == "" {
		return fmt.Errorf("human verification failed: %w", refused)
	}
	meta, _ := requestmeta.From(ctx)
	ok, err := p.deps.VerifyTurnstile(ctx, cfg.TurnstileSecret, token, meta.ClientIP)
	if err != nil {
		// The refusal comes first, so its code is the one the client gets.
		return fmt.Errorf("human verification failed: %w: %w", refused, err)
	}
	if !ok {
		return fmt.Errorf("human verification failed: %w", refused)
	}
	return nil
}

// TakeIPPermit atomically reserves one registration from the configured IP
// quota of the client. The duration is configured in minutes.
func (p ServicePolicy) TakeIPPermit(ctx context.Context) error {
	cfg := p.deps.Config()
	if !cfg.EnableIpRegisterLimit {
		return nil
	}
	if p.deps.Redis == nil || cfg.IpRegisterLimit <= 0 || cfg.IpRegisterLimitDuration <= 0 {
		return xerr.Errorf(xerr.ERROR, "invalid IP registration limit configuration")
	}
	meta, _ := requestmeta.From(ctx)
	parsedIP := net.ParseIP(strings.TrimSpace(meta.ClientIP))
	if parsedIP == nil {
		return xerr.Errorf(xerr.InvalidParams, "invalid client IP")
	}

	maxInt := int64(^uint(0) >> 1)
	if cfg.IpRegisterLimit > maxInt || cfg.IpRegisterLimitDuration > maxInt/60 {
		return xerr.Errorf(xerr.ERROR, "IP registration limit configuration is too large")
	}
	limiter := ratelimit.NewPeriodLimit(
		int(cfg.IpRegisterLimitDuration*60),
		int(cfg.IpRegisterLimit),
		p.deps.Redis,
		config.RegisterIPLimitKeyPrefix,
	)
	permit, err := limiter.Take(ctx, parsedIP.String())
	if err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "check IP registration limit")
	}
	if !limiter.ParsePermitState(permit) {
		return xerr.Errorf(xerr.TooManyRequests, "registration limit exceeded for IP %s", parsedIP.String())
	}
	return nil
}
