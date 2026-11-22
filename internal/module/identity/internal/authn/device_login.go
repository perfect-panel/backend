package authn

import (
	"context"
	"errors"
	"strings"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authn/registerpolicy"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// DeviceLogin signs in with a device identifier. A device seen for the first
// time registers a new account when registration is open.
//
// The identifier is a bearer credential, so guessing it is capped like a
// password: per identifier and per client address, counted before the lookup
// so a burst cannot exceed the limit, and the identifier's count is cleared
// by a successful sign-in. The Turnstile challenge for sign-ins applies when
// it is switched on; a first-time device then needs no second challenge for
// its registration, since a Turnstile token is redeemed once.
func (s *Service) DeviceLogin(ctx context.Context, req *dto.DeviceLoginRequest) (resp *dto.LoginResponse, err error) {
	if req.Identifier == "" || len(req.Identifier) > 255 || strings.TrimSpace(req.Identifier) != req.Identifier {
		return nil, xerr.NewErrCode(xerr.InvalidParams)
	}
	if err := s.policy.EnsureMethodEnabled(ctx, identifier.Device); err != nil {
		return nil, err
	}
	cfg := s.deps.Config()
	if cfg.DeviceOnlyReal {
		if secure, _ := ctx.Value(requestctx.CtxKeyDeviceSecure).(bool); !secure {
			return nil, xerr.Errorf(xerr.InvalidAccess, "verified device transport is required")
		}
	}
	if err := s.policy.VerifyHuman(ctx, registerpolicy.Login, req.CfToken); err != nil {
		return nil, err
	}
	if err := s.reserveDeviceLoginAttempt(ctx, req.Identifier); err != nil {
		return nil, err
	}
	attempt := account.NewAttempt(s.deps.Store.Log(), identifier.Device)
	defer func() {
		if err = attempt.Finish(ctx, err); err != nil {
			resp = nil
		}
	}()

	devices := s.deps.Store.UserDevice()
	deviceInfo, err := devices.FindOneDeviceByIdentifier(ctx, req.Identifier)
	var userInfo *user.User
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		if userInfo, deviceInfo, err = s.registerDevice(ctx, req, cfg.LoginVerify); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find device")
	default:
		if userInfo, err = s.deps.Store.User().FindOne(ctx, deviceInfo.UserId); err != nil {
			return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find user %d of the device", deviceInfo.UserId)
		}
	}
	attempt.Identify(userInfo.Id)
	if err := account.EnsureActive(userInfo); err != nil {
		return nil, err
	}
	// Read authoritative device state rather than trusting a cached binding.
	deviceInfo, err = devices.FindDeviceForAuth(ctx, deviceInfo.Id)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "read device state")
	}
	if !deviceInfo.Enabled || deviceInfo.UserId != userInfo.Id {
		return nil, xerr.Errorf(xerr.InvalidAccess, "device is disabled or its binding changed")
	}
	ip, userAgent := deviceMetadata(ctx)
	touched, err := devices.TouchDevice(ctx, deviceInfo.Id, userInfo.Id, ip, userAgent)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseUpdateError, "refresh device")
	}
	if !touched {
		return nil, xerr.Errorf(xerr.InvalidAccess, "device binding changed")
	}
	token, err := account.IssueSession(ctx, s.deps.Redis, cfg.sessions(), account.Login{UserID: userInfo.Id, LoginType: identifier.Device, Device: deviceInfo})
	if err != nil {
		return nil, err
	}
	account.ClearAttempts(ctx, s.deps.Redis, account.DeviceLoginAttemptKey(req.Identifier))
	return &dto.LoginResponse{Token: token}, nil
}

// reserveDeviceLoginAttempt counts a device sign-in against the identifier's
// and the client address's guess limits, before anything is looked up.
func (s *Service) reserveDeviceLoginAttempt(ctx context.Context, deviceIdentifier string) error {
	ip, _ := deviceMetadata(ctx)
	if err := account.ReserveAttempt(ctx, s.deps.Redis, account.DeviceLoginIPAttemptKey(ip),
		account.MaxDeviceLoginIPAttempts, account.DeviceLoginIPAttemptWindow); err != nil {
		return err
	}
	return account.ReserveAttempt(ctx, s.deps.Redis, account.DeviceLoginAttemptKey(deviceIdentifier),
		account.MaxDeviceLoginAttempts, account.DeviceLoginAttemptWindow)
}

// registerDevice creates the account of a device seen for the first time,
// signed in to by the device alone. humanVerified reports that the sign-in
// already passed the Turnstile challenge, whose token cannot be redeemed a
// second time for the registration.
func (s *Service) registerDevice(ctx context.Context, req *dto.DeviceLoginRequest, humanVerified bool) (*user.User, *user.Device, error) {
	if err := s.policy.EnsureRegistrationOpen(ctx, identifier.Device); err != nil {
		return nil, nil, err
	}
	if !humanVerified {
		if err := s.policy.VerifyHuman(ctx, registerpolicy.Register, req.CfToken); err != nil {
			return nil, nil, err
		}
	}
	referer, err := s.resolveReferer(ctx, req.Invite)
	if err != nil {
		return nil, nil, err
	}
	if err := s.policy.TakeIPPermit(ctx); err != nil {
		return nil, nil, err
	}

	cfg := s.deps.Config()
	ip, userAgent := deviceMetadata(ctx)
	newUser := &user.User{OnlyFirstPurchase: &cfg.OnlyFirstPurchase}
	if referer != nil {
		newUser.RefererId = referer.Id
	}
	device := &user.Device{Ip: ip, UserAgent: userAgent, Identifier: req.Identifier, Enabled: true}
	if err := account.Register(ctx, s.deps.Store, account.New{
		User:       newUser,
		Identities: []user.AuthMethods{{AuthType: identifier.Device, AuthIdentifier: req.Identifier, Verified: true}},
		Device:     device,
	}, identifier.Device); err != nil {
		return nil, nil, err
	}
	// The identifier is a credential and stays out of the log; the device
	// row names it.
	logger.WithContext(ctx).Infow("device registered a new account",
		logger.Field("user_id", newUser.Id), logger.Field("device_id", device.Id))
	return newUser, device, nil
}
