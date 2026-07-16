package registerpolicy

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/authmethod"
	"github.com/perfect-panel/server/pkg/limit"
	"github.com/perfect-panel/server/pkg/turnstile"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

const (
	MethodEmail  = authmethod.Email
	MethodMobile = authmethod.Mobile
	MethodDevice = authmethod.Device
)

// EnsureMethodEnabled rejects direct calls to authentication methods disabled
// by the administrator. OAuth methods are loaded from the auth_method table.
func EnsureMethodEnabled(ctx context.Context, svcCtx *svc.ServiceContext, method string) error {
	switch method {
	case MethodEmail:
		if svcCtx.Config.Email.Enable {
			return nil
		}
	case MethodMobile:
		if svcCtx.Config.Mobile.Enable {
			return nil
		}
	case MethodDevice:
		if svcCtx.Config.Device.Enable {
			return nil
		}
	default:
		configured, err := svcCtx.Store.Auth().FindOneByMethod(ctx, method)
		if err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.GetAuthenticatorError), "load auth method %q: %v", method, err)
		}
		if configured.Enabled != nil && *configured.Enabled {
			return nil
		}
	}
	return errors.Wrapf(xerr.NewErrCode(xerr.GetAuthenticatorError), "auth method %q is disabled", method)
}

// EnsureRegistrationOpen applies policies shared by every new-account path.
func EnsureRegistrationOpen(ctx context.Context, svcCtx *svc.ServiceContext, method string) error {
	if svcCtx.Config.Register.StopRegister {
		return errors.Wrap(xerr.NewErrCode(xerr.StopRegister), "registration is disabled")
	}
	return EnsureMethodEnabled(ctx, svcCtx, method)
}

// VerifyHuman enforces the configured registration Turnstile challenge.
func VerifyHuman(ctx context.Context, svcCtx *svc.ServiceContext, token, ip string) error {
	if !svcCtx.Config.Verify.RegisterVerify {
		return nil
	}
	if strings.TrimSpace(token) == "" || strings.TrimSpace(svcCtx.Config.Verify.TurnstileSecret) == "" {
		return errors.Wrap(xerr.NewErrCode(xerr.TooManyRequests), "registration verification failed")
	}
	verifier := turnstile.New(turnstile.Config{
		Secret:  svcCtx.Config.Verify.TurnstileSecret,
		Timeout: 3 * time.Second,
	})
	ok, err := verifier.Verify(ctx, token, ip)
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.TooManyRequests), "registration verification failed: %v", err)
	}
	if !ok {
		return errors.Wrap(xerr.NewErrCode(xerr.TooManyRequests), "registration verification failed")
	}
	return nil
}

// TakeIPPermit atomically reserves one registration from the configured IP
// quota. The duration is configured in minutes.
func TakeIPPermit(ctx context.Context, svcCtx *svc.ServiceContext, ip string) error {
	cfg := svcCtx.Config.Register
	if !cfg.EnableIpRegisterLimit {
		return nil
	}
	if svcCtx.Redis == nil || cfg.IpRegisterLimit <= 0 || cfg.IpRegisterLimitDuration <= 0 {
		return errors.Wrap(xerr.NewErrCode(xerr.ERROR), "invalid IP registration limit configuration")
	}
	parsedIP := net.ParseIP(strings.TrimSpace(ip))
	if parsedIP == nil {
		return errors.Wrap(xerr.NewErrCode(xerr.InvalidParams), "invalid client IP")
	}

	maxInt := int64(^uint(0) >> 1)
	if cfg.IpRegisterLimit > maxInt || cfg.IpRegisterLimitDuration > maxInt/60 {
		return errors.Wrap(xerr.NewErrCode(xerr.ERROR), "IP registration limit configuration is too large")
	}
	limiter := limit.NewPeriodLimit(
		int(cfg.IpRegisterLimitDuration*60),
		int(cfg.IpRegisterLimit),
		svcCtx.Redis,
		config.RegisterIPLimitKeyPrefix,
	)
	permit, err := limiter.TakeCtx(ctx, parsedIP.String())
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "check IP registration limit: %v", err)
	}
	if !limiter.ParsePermitState(permit) {
		return errors.Wrapf(xerr.NewErrCode(xerr.TooManyRequests), "registration limit exceeded for IP %s", parsedIP.String())
	}
	return nil
}
