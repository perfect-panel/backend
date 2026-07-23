package user

import (
	"context"

	"github.com/perfect-panel/server/internal/logic/auth/registerpolicy"
	"github.com/perfect-panel/server/pkg/authmethod"
	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"gorm.io/gorm"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type UpdateBindEmailLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUpdateBindEmailLogic Update Bind Email
func NewUpdateBindEmailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateBindEmailLogic {
	return &UpdateBindEmailLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateBindEmailLogic) UpdateBindEmail(req *dto.UpdateBindEmailRequest) error {
	if err := registerpolicy.EnsureMethodEnabled(l.ctx, l.svcCtx, registerpolicy.MethodEmail); err != nil {
		return err
	}
	email, err := authmethod.ValidateEmail(req.Email, l.svcCtx.Config.Email.DomainSuffixList, l.svcCtx.Config.Email.EnableDomainSuffix)
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "invalid email: %v", err)
	}
	req.Email = email
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	method, err := l.svcCtx.Store.UserAuth().FindUserAuthMethodByUserId(l.ctx, "email", u.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindUserAuthMethodByOpenID error")
	}
	m, err := l.svcCtx.Store.UserAuth().FindUserAuthMethodByOpenID(l.ctx, "email", req.Email)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindUserAuthMethodByOpenID error")
	}
	// email already bind
	if m.Id > 0 {
		return errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "email already bind")
	}
	if method.Id == 0 {
		method = &user.AuthMethods{
			UserId:         u.Id,
			AuthType:       "email",
			AuthIdentifier: req.Email,
			Verified:       false,
		}
		if err := l.svcCtx.Store.UserAuth().InsertUserAuthMethods(l.ctx, method); err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "InsertUserAuthMethods error")
		}
	} else {
		method.Verified = false
		method.AuthIdentifier = req.Email
		if err := l.svcCtx.Store.UserAuth().UpdateUserAuthMethods(l.ctx, method); err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "UpdateUserAuthMethods error")
		}
	}
	return nil
}
