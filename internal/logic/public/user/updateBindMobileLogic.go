package user

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/logic/auth/registerpolicy"
	"github.com/perfect-panel/server/internal/logic/common"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/phone"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"gorm.io/gorm"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type UpdateBindMobileLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update Bind Mobile
func NewUpdateBindMobileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateBindMobileLogic {
	return &UpdateBindMobileLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateBindMobileLogic) UpdateBindMobile(req *dto.UpdateBindMobileRequest) error {
	if err := registerpolicy.EnsureMethodEnabled(l.ctx, l.svcCtx, registerpolicy.MethodMobile); err != nil {
		return err
	}
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	// verify mobile
	phoneNumber, err := phone.FormatToE164(req.AreaCode, req.Mobile)
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.TelephoneError), "Invalid phone number")
	}
	cacheKey := fmt.Sprintf("%s:%s:%s", config.AuthCodeTelephoneCacheKey, constant.Register, phoneNumber)
	if err := common.ValidateVerificationCode(l.ctx, l.svcCtx.Redis, cacheKey, req.Code, false); err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
	}

	m, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, "mobile", phoneNumber)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindUserAuthMethodByOpenID error")
	}
	if m.Id > 0 {
		return errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "mobile already bind")
	}

	method, err := l.svcCtx.Store.User().FindUserAuthMethodByUserId(l.ctx, "mobile", u.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindUserAuthMethodByOpenID error")
	}
	if err := common.ValidateVerificationCode(l.ctx, l.svcCtx.Redis, cacheKey, req.Code, true); err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		method = &user.AuthMethods{
			UserId:         u.Id,
			AuthType:       "mobile",
			AuthIdentifier: phoneNumber,
			Verified:       true,
		}
		if err := l.svcCtx.Store.User().InsertUserAuthMethods(l.ctx, method); err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "InsertUserAuthMethods error")
		}
	} else {
		method.Verified = true
		method.AuthIdentifier = phoneNumber
		if err := l.svcCtx.Store.User().UpdateUserAuthMethods(l.ctx, method); err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "UpdateUserAuthMethods error")
		}
	}
	return nil
}
