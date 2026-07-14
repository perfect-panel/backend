package authMethod

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/auth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/email"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/sms"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateAuthMethodConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update auth method config
func NewUpdateAuthMethodConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateAuthMethodConfigLogic {
	return &UpdateAuthMethodConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateAuthMethodConfigLogic) UpdateAuthMethodConfig(req *dto.UpdateAuthMethodConfigRequest) (resp *dto.AuthMethodConfig, err error) {
	method, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, req.Method)
	if err != nil {
		l.Errorw("find one by method failed", logger.Field("method", req.Method), logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find one by method failed: %v", err.Error())
	}

	tool.DeepCopy(method, req)
	if req.Config != nil {
		_, exist := req.Config.(map[string]interface{})
		if !exist {
			req.Config = initializePlatformConfig(req.Method).(string)
		}
		if req.Method == "email" {
			configs, _ := json.Marshal(req.Config)
			emailConfig := new(auth.EmailAuthConfig)
			emailConfig.Unmarshal(string(configs))
			req.Config = emailConfig
		}

		if req.Method == "mobile" {
			configs, _ := json.Marshal(req.Config)
			mobileConfig := new(auth.MobileAuthConfig)
			mobileConfig.Unmarshal(string(configs))
			req.Config = mobileConfig
		}

		bytes, err := json.Marshal(req.Config)
		if err != nil {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "marshal config failed: %v", err.Error())
		}
		method.Config = string(bytes)
	} else {
		// initialize platform config
		method.Config = initializePlatformConfig(req.Method).(string)
	}
	err = l.svcCtx.Store.Auth().Update(l.ctx, method)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "update auth method failed: %v", err.Error())
	}

	resp = new(dto.AuthMethodConfig)
	tool.DeepCopy(resp, method)
	if method.Config != "" {
		if err := json.Unmarshal([]byte(method.Config), &resp.Config); err != nil {
			l.Errorw("unmarshal apple config failed", logger.Field("config", method.Config), logger.Field("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal apple config failed: %v", err.Error())
		}
	}
	// update global config
	defer l.UpdateGlobal(method.Method)
	return
}

func (l *UpdateAuthMethodConfigLogic) UpdateGlobal(method string) {
	if method == "email" {
		initialize.Email(l.svcCtx)
	}
	if method == "mobile" {
		initialize.Mobile(l.svcCtx)
	}
	if method == "device" {
		initialize.Device(l.svcCtx)
	}
}

func validatePlatformConfig(platform string, cfg map[string]interface{}) (interface{}, error) {
	var bytes []byte
	var err error
	var config interface{}
	bytes, err = json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	switch platform {
	case email.SMTP.String():
		config = new(auth.SMTPConfig)
	case sms.Abosend.String():
		config = new(auth.AbosendConfig)
	case sms.AlibabaCloud.String():
		config = new(auth.AlibabaCloudConfig)
	case sms.Smsbao.String():
		config = new(auth.SmsbaoConfig)
	case sms.Twilio.String():
		config = new(auth.TwilioConfig)
	default:
		return nil, errors.New("invalid platform")
	}
	err = json.Unmarshal(bytes, config)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func initializePlatformConfig(platform string) interface{} {
	var result interface{}
	switch platform {
	case "email":
		result = new(auth.EmailAuthConfig).Marshal()
	case "mobile":
		result = new(auth.MobileAuthConfig).Marshal()
	case "apple":
		result = new(auth.AppleAuthConfig).Marshal()
	case "google":
		result = new(auth.GoogleAuthConfig).Marshal()
	case "github":
		result = new(auth.GithubAuthConfig).Marshal()
	case "facebook":
		result = new(auth.FacebookAuthConfig).Marshal()
	case "telegram":
		result = new(auth.TelegramAuthConfig).Marshal()
	case "device":
		result = new(auth.DeviceConfig).Marshal()
	}
	return result
}
