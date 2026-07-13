package common

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/authmethod"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/phone"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type CheckVerificationCodeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Check verification code
func NewCheckVerificationCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CheckVerificationCodeLogic {
	return &CheckVerificationCodeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CheckVerificationCodeLogic) CheckVerificationCode(req *types.CheckVerificationCodeRequest) (resp *types.CheckVerificationCodeRespone, err error) {
	resp = &types.CheckVerificationCodeRespone{}
	if req.Method == authmethod.Email {
		email := authmethod.CanonicalEmail(req.Account)
		cacheKey := fmt.Sprintf("%s:%s:%s", config.AuthCodeCacheKey, constant.ParseVerifyType(req.Type), email)
		value, err := l.svcCtx.Redis.Get(l.ctx, cacheKey).Result()
		if err != nil {
			return resp, nil
		}
		var payload CacheKeyPayload
		if err := json.Unmarshal([]byte(value), &payload); err != nil {
			return resp, nil
		}
		if payload.Code != req.Code {
			return resp, nil
		}
		resp.Status = true
	}
	if req.Method == authmethod.Mobile {
		if !phone.CheckPhone(req.Account) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.TelephoneError), "Invalid phone number")
		}
		cacheKey := fmt.Sprintf("%s:%s:+%s", config.AuthCodeTelephoneCacheKey, constant.ParseVerifyType(req.Type), req.Account)
		value, err := l.svcCtx.Redis.Get(l.ctx, cacheKey).Result()
		if err != nil {
			return resp, nil
		}
		var payload CacheKeyPayload
		if err := json.Unmarshal([]byte(value), &payload); err != nil {
			return resp, nil
		}
		if payload.Code != req.Code {
			return resp, nil
		}
		resp.Status = true
	}
	return resp, nil
}
