package bootstrap

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/infra/mapping"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

var errAuthMethodMissing = errors.New("no such auth method")

// findAuthMethod reads the stored configuration of one authentication method
// from the identity module, which owns it.
func findAuthMethod(ctx context.Context, deps *Dependencies, method string) (*auth.Auth, error) {
	found, err := deps.LoginMethods.FindLoginMethod(ctx, method)
	if err == nil && found == nil {
		err = errAuthMethodMissing
	}
	if err != nil {
		return nil, wrapf(err, xerr.DatabaseQueryError, "read the %s auth method", method)
	}
	return found, nil
}

// authMethodEnabled treats a method without a stored flag as disabled
// instead of dereferencing nil.
func authMethodEnabled(method *auth.Auth) bool {
	return method.Enabled != nil && *method.Enabled
}

// Email loads the email sign-in and sending settings: the provider, its
// configuration, and the notification templates and subjects.
func Email(ctx context.Context, deps *Dependencies) error {
	logger.Debug("Email config initialization")
	method, err := findAuthMethod(ctx, deps, "email")
	if err != nil {
		return err
	}
	var cfg config.EmailConfig
	var emailConfig = new(auth.EmailAuthConfig)
	if err := emailConfig.Unmarshal(method.Config); err != nil {
		// The stored config falls back to the defaults on purpose; the
		// load goes on, but the operator has to see the rejected value.
		logger.WithContext(ctx).Errorw("[Email] stored auth method config is invalid, using the defaults",
			logger.Field("method", "email"), logger.Field("error", err.Error()))
	}
	if err := mapping.Copy(&cfg, emailConfig); err != nil {
		return wrapf(err, xerr.ERROR, "apply the email auth method config")
	}
	cfg.Enable = authMethodEnabled(method)
	value, err := json.Marshal(emailConfig.PlatformConfig)
	if err != nil {
		return wrapf(err, xerr.ERROR, "encode the email platform config")
	}
	cfg.PlatformConfig = string(value)
	deps.updateRuntime(func(current *config.Runtime) { current.Email = cfg })
	return nil
}

// Mobile loads the phone sign-in and SMS settings: the provider, its
// configuration and the area-code whitelist.
func Mobile(ctx context.Context, deps *Dependencies) error {
	logger.Debug("Mobile config initialization")
	method, err := findAuthMethod(ctx, deps, "mobile")
	if err != nil {
		return err
	}
	var cfg config.MobileConfig
	var mobileConfig auth.MobileAuthConfig
	if err := mobileConfig.Unmarshal(method.Config); err != nil {
		// The stored config falls back to the defaults on purpose; the
		// load goes on, but the operator has to see the rejected value.
		logger.WithContext(ctx).Errorw("[Mobile] stored auth method config is invalid, using the defaults",
			logger.Field("method", "mobile"), logger.Field("error", err.Error()))
	}
	if err := mapping.Copy(&cfg, mobileConfig); err != nil {
		return wrapf(err, xerr.ERROR, "apply the mobile auth method config")
	}
	cfg.Enable = authMethodEnabled(method)
	value, err := json.Marshal(mobileConfig.PlatformConfig)
	if err != nil {
		return wrapf(err, xerr.ERROR, "encode the mobile platform config")
	}
	cfg.PlatformConfig = string(value)
	deps.updateRuntime(func(current *config.Runtime) { current.Mobile = cfg })
	return nil
}

// Device loads the device login settings. A stored configuration that does
// not decode fails the load: applying the zero value instead would silently
// switch device request signing off.
func Device(ctx context.Context, deps *Dependencies) error {
	logger.Debug("device config initialization")
	method, err := findAuthMethod(ctx, deps, "device")
	if err != nil {
		return err
	}
	var cfg config.DeviceConfig
	var deviceConfig auth.DeviceConfig
	if err := deviceConfig.Unmarshal(method.Config); err != nil {
		return wrapf(err, xerr.ERROR, "decode the device auth method config")
	}
	if err := mapping.Copy(&cfg, deviceConfig); err != nil {
		return wrapf(err, xerr.ERROR, "apply the device auth method config")
	}
	cfg.Enable = authMethodEnabled(method)
	deps.updateRuntime(func(current *config.Runtime) { current.Device = cfg })
	return nil
}
