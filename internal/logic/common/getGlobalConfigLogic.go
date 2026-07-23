package common

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/report"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetGlobalConfigLogic struct {
	logger.Logger
	ctx  context.Context
	deps GetGlobalConfigDependencies
}

// Get global config
func NewGetGlobalConfigLogic(ctx context.Context, deps GetGlobalConfigDependencies) *GetGlobalConfigLogic {
	return &GetGlobalConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *GetGlobalConfigLogic) GetGlobalConfig() (resp *dto.GetGlobalConfigResponse, err error) {
	resp = new(dto.GetGlobalConfigResponse)

	currencyCfg, err := l.deps.Store.System().GetCurrencyConfig(l.ctx)
	if err != nil {
		l.Logger.Error("[GetGlobalConfigLogic] GetCurrencyConfig error: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetCurrencyConfig error: %v", err.Error())
	}
	verifyCodeCfg, err := l.deps.Store.System().GetVerifyCodeConfig(l.ctx)
	if err != nil {
		l.Logger.Error("[GetGlobalConfigLogic] GetVerifyCodeConfig error: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetVerifyCodeConfig error: %v", err.Error())
	}

	tool.DeepCopy(&resp.Site, l.deps.Config.Site)
	tool.DeepCopy(&resp.Subscribe, l.deps.Config.Subscribe)
	tool.DeepCopy(&resp.Auth.Email, l.deps.Config.Email)
	tool.DeepCopy(&resp.Auth.Mobile, l.deps.Config.Mobile)
	tool.DeepCopy(&resp.Auth.Register, l.deps.Config.Register)
	tool.DeepCopy(&resp.Verify, l.deps.Config.Verify)
	tool.DeepCopy(&resp.Invite, l.deps.Config.Invite)
	tool.SystemConfigSliceReflectToStruct(currencyCfg, &resp.Currency)
	tool.SystemConfigSliceReflectToStruct(verifyCodeCfg, &resp.VerifyCode)

	if report.IsGatewayMode() {
		resp.Subscribe.SubscribePath = "/sub" + l.deps.Config.Subscribe.SubscribePath
	}

	resp.Verify = dto.VeifyConfig{
		TurnstileSiteKey:          l.deps.Config.Verify.TurnstileSiteKey,
		EnableLoginVerify:         l.deps.Config.Verify.LoginVerify,
		EnableRegisterVerify:      l.deps.Config.Verify.RegisterVerify,
		EnableResetPasswordVerify: l.deps.Config.Verify.ResetPasswordVerify,
	}
	var methods []string

	// auth methods
	authMethods, err := l.deps.Store.Auth().FindAll(l.ctx)
	if err != nil {
		l.Logger.Error("[GetGlobalConfigLogic] FindAll error: ", logger.Field("error", err.Error()))
	}

	for _, method := range authMethods {
		if *method.Enabled {
			methods = append(methods, method.Method)
			if method.Method == "device" {
				_ = json.Unmarshal([]byte(method.Config), &resp.Auth.Device)
				resp.Auth.Device.Enable = true
			}
		}
	}
	resp.OAuthMethods = methods

	webAds, err := l.deps.Store.System().FindOneByKey(l.ctx, "WebAD")
	if err != nil {
		l.Logger.Error("[GetGlobalConfigLogic] FindOneByKey error: ", logger.Field("error", err.Error()), logger.Field("key", "WebAD"))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneByKey error: %v", err.Error())
	}
	// web ads config
	resp.WebAd = webAds.Value == "true"
	return
}
