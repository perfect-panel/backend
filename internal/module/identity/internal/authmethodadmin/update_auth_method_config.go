package authmethodadmin

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/backend/internal/infra/mail"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateAuthMethodConfig stores an administrator's configuration of an
// authentication method. A configuration that is not an object or does not
// decode is refused, not replaced by the defaults; a missing one resets the
// method to them.
func (s *Service) UpdateAuthMethodConfig(ctx context.Context, req *dto.UpdateAuthMethodConfigRequest) (*dto.AuthMethodConfig, error) {
	method, err := s.deps.Auths.FindOneByMethod(ctx, req.Method)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find auth method %q", req.Method)
	}

	// The method names the row; an id the request carries must be that row's,
	// or the save would overwrite another method. A request without a
	// configuration resets the configuration to the defaults and leaves the
	// switch as stored; one without a switch keeps it too.
	if req.Id != 0 && req.Id != method.Id {
		return nil, xerr.Errorf(xerr.InvalidParams, "auth method %d is not %q", req.Id, req.Method)
	}
	if req.Config != nil {
		if _, ok := req.Config.(map[string]any); !ok {
			return nil, xerr.Errorf(xerr.InvalidParams, "the %s config must be an object", req.Method)
		}
		config, err := decodeMethodConfig(req.Method, req.Config)
		if err != nil {
			return nil, err
		}
		bytes, err := json.Marshal(config)
		if err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "marshal %s config", req.Method)
		}
		method.Config = string(bytes)
		if req.Enabled != nil {
			method.Enabled = req.Enabled
		}
	} else {
		method.Config = initializePlatformConfig(req.Method)
	}
	if err := s.deps.Auths.Update(ctx, method); err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseUpdateError, "update auth method %q", req.Method)
	}

	resp, err := methodConfig(method)
	if err != nil {
		return nil, err
	}
	// The email, mobile, device and telegram settings are also held by the
	// runtime configuration, which reloads them.
	switch method.Method {
	case "email", "mobile", "device", "telegram":
		if err := s.deps.Reinitialize(method.Method); err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "the %s settings are saved but could not be applied", method.Method)
		}
	}
	return resp, nil
}

// decodeMethodConfig checks the configuration of the methods whose settings
// the runtime reads; the provider methods keep what the administrator sent.
func decodeMethodConfig(method string, config any) (any, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.InvalidParams, "encode %s config", method)
	}
	switch method {
	case "email":
		emailConfig := new(auth.EmailAuthConfig)
		if err := emailConfig.Unmarshal(string(raw)); err != nil {
			return nil, xerr.Wrapf(err, xerr.InvalidParams, "invalid email config")
		}
		if emailConfig.Platform == mail.Resend.String() || emailConfig.Platform == mail.Cloudflare.String() {
			settings, err := json.Marshal(emailConfig.PlatformConfig)
			if err != nil {
				return nil, xerr.Wrapf(err, xerr.InvalidParams, "encode email provider settings")
			}
			if _, err := mail.NewSender(emailConfig.Platform, string(settings), ""); err != nil {
				return nil, xerr.Wrapf(err, xerr.InvalidParams, "invalid email provider settings")
			}
		}
		return emailConfig, nil
	case "mobile":
		mobileConfig := new(auth.MobileAuthConfig)
		if err := mobileConfig.Unmarshal(string(raw)); err != nil {
			return nil, xerr.Wrapf(err, xerr.InvalidParams, "invalid mobile config")
		}
		return mobileConfig, nil
	case "device":
		deviceConfig := new(auth.DeviceConfig)
		if err := deviceConfig.Unmarshal(string(raw)); err != nil {
			return nil, xerr.Wrapf(err, xerr.InvalidParams, "invalid device config")
		}
		if deviceConfig.OnlyRealDevice && !deviceConfig.EnableSecurity {
			return nil, xerr.Errorf(xerr.InvalidParams, "only_real_device requires enable_security")
		}
		if deviceConfig.EnableSecurity && deviceConfig.SecuritySecret == "" {
			return nil, xerr.Errorf(xerr.InvalidParams, "device security secret is required")
		}
		return deviceConfig, nil
	case "telegram":
		// Startup decodes the stored settings into the same type, so what
		// does not decode here would keep the next start from completing.
		telegramConfig := new(auth.TelegramAuthConfig)
		if err := telegramConfig.Unmarshal(string(raw)); err != nil {
			return nil, xerr.Wrapf(err, xerr.InvalidParams, "invalid telegram config")
		}
		return telegramConfig, nil
	default:
		return config, nil
	}
}

// initializePlatformConfig is the default configuration of a method, as
// stored JSON; unknown methods have none.
func initializePlatformConfig(platform string) string {
	switch platform {
	case "email":
		return new(auth.EmailAuthConfig).Marshal()
	case "mobile":
		return new(auth.MobileAuthConfig).Marshal()
	case "apple":
		return new(auth.AppleAuthConfig).Marshal()
	case "google":
		return new(auth.GoogleAuthConfig).Marshal()
	case "github":
		return new(auth.GithubAuthConfig).Marshal()
	case "facebook":
		return new(auth.FacebookAuthConfig).Marshal()
	case "telegram":
		return new(auth.TelegramAuthConfig).Marshal()
	case "device":
		return new(auth.DeviceConfig).Marshal()
	}
	return ""
}
