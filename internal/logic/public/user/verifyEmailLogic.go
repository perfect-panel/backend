package user

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/logic/auth/registerpolicy"
	"github.com/perfect-panel/server/internal/logic/common"
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

func (l *VerifyEmailLogic) VerifyEmail(req *dto.VerifyEmailRequest) error {
	if err := registerpolicy.EnsureMethodEnabled(l.ctx, l.svcCtx, registerpolicy.MethodEmail); err != nil {
		return err
	}
	email, err := authmethod.ValidateEmail(req.Email, l.svcCtx.Config.Email.DomainSuffixList, l.svcCtx.Config.Email.EnableDomainSuffix)
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "invalid email: %v", err)
	}
	cacheKey := fmt.Sprintf("%s:%s:%s", config.AuthCodeCacheKey, constant.Security, email)
	if err := common.ValidateVerificationCode(l.ctx, l.svcCtx.Redis, cacheKey, req.Code, false); err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
	}

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
	if err := common.ValidateVerificationCode(l.ctx, l.svcCtx.Redis, cacheKey, req.Code, true); err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
	}
	method.Verified = true
	err = l.svcCtx.Store.User().UpdateUserAuthMethods(l.ctx, method)
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "UpdateUserAuthMethods error")
	}
	return nil
}
