package publicinfo

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/infra/mapping"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetGlobalConfig assembles the public site configuration from the running
// configuration, the stored currency and verification code settings, the
// enabled login methods and the web ads switch. The login methods are best
// effort: when they cannot be read, none is listed.
func (s *Service) GetGlobalConfig(ctx context.Context) (*dto.GetGlobalConfigResponse, error) {
	log := logger.WithContext(ctx)
	running := s.deps.Config()
	resp := new(dto.GetGlobalConfigResponse)

	currencyCfg, err := s.deps.Settings.GetCurrencyConfig(ctx)
	if err != nil {
		log.Errorw("[GetGlobalConfig] GetCurrencyConfig error", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "GetCurrencyConfig error: %v", err.Error())
	}
	verifyCodeCfg, err := s.deps.Settings.GetVerifyCodeConfig(ctx)
	if err != nil {
		log.Errorw("[GetGlobalConfig] GetVerifyCodeConfig error", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "GetVerifyCodeConfig error: %v", err.Error())
	}

	for _, part := range []struct{ dst, src any }{
		{&resp.Site, running.Site},
		{&resp.Subscribe, running.Subscribe},
		{&resp.Auth.Email, running.Email},
		{&resp.Auth.Mobile, running.Mobile},
		{&resp.Auth.Register, running.Register},
		{&resp.Invite, running.Invite},
	} {
		if err := mapping.Copy(part.dst, part.src); err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "copy the public configuration")
		}
	}
	config.SystemConfigSliceReflectToStruct(currencyCfg, &resp.Currency)
	config.SystemConfigSliceReflectToStruct(verifyCodeCfg, &resp.VerifyCode)

	resp.Verify = dto.VeifyConfig{
		TurnstileSiteKey:          running.Verify.TurnstileSiteKey,
		EnableLoginVerify:         running.Verify.LoginVerify,
		EnableRegisterVerify:      running.Verify.RegisterVerify,
		EnableResetPasswordVerify: running.Verify.ResetPasswordVerify,
	}

	authMethods, err := s.deps.Accounts.ListAuthMethods(ctx)
	if err != nil {
		log.Errorw("[GetGlobalConfig] FindAll error", logger.Field("error", err.Error()))
	}
	var methods []string
	for _, method := range authMethods {
		if method.Enabled {
			methods = append(methods, method.Method)
			if method.Method == "device" {
				_ = json.Unmarshal([]byte(method.Config), &resp.Auth.Device)
				resp.Auth.Device.Enable = true
			}
		}
	}
	resp.OAuthMethods = methods

	webAds, err := s.deps.Settings.FindOneByKey(ctx, "WebAD")
	if err != nil {
		log.Errorw("[GetGlobalConfig] FindOneByKey error", logger.Field("error", err.Error()), logger.Field("key", "WebAD"))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "FindOneByKey error: %v", err.Error())
	}
	resp.WebAd = webAds.Value == "true"
	return resp, nil
}
