package user

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/authmethod"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type VerifyEmailLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Verify Email
func NewVerifyEmailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *VerifyEmailLogic {
	return &VerifyEmailLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

type CacheKeyPayload struct {
	Code   string `json:"code"`
	LastAt int64  `json:"lastAt"`
}

func (l *VerifyEmailLogic) VerifyEmail(req *dto.VerifyEmailRequest) error {
	email := authmethod.CanonicalEmail(req.Email)
	cacheKey := fmt.Sprintf("%s:%s:%s", config.AuthCodeCacheKey, constant.Security, email)
	value, err := l.svcCtx.Redis.Get(l.ctx, cacheKey).Result()
	if err != nil {
		l.Errorw("Redis Error", logger.Field("error", err.Error()), logger.Field("cacheKey", cacheKey))
		return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
	}

	var payload CacheKeyPayload
	err = json.Unmarshal([]byte(value), &payload)
	if err != nil {
		l.Errorw("Redis Error", logger.Field("error", err.Error()), logger.Field("cacheKey", cacheKey))
		return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
	}
	if payload.Code != req.Code {
		return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
	}
	l.svcCtx.Redis.Del(l.ctx, cacheKey)

	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	method, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, authmethod.Email, email)
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindUserAuthMethodByOpenID error")
	}
	if method.UserId != u.Id {
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "invalid access")
	}
	method.Verified = true
	err = l.svcCtx.Store.User().UpdateUserAuthMethods(l.ctx, method)
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "UpdateUserAuthMethods error")
	}
	return nil
}
