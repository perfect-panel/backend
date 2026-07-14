package auth

import (
	"context"

	"github.com/perfect-panel/server/pkg/phone"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"gorm.io/gorm"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type CheckUserTelephoneLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Check user telephone is exist
func NewCheckUserTelephoneLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CheckUserTelephoneLogic {
	return &CheckUserTelephoneLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CheckUserTelephoneLogic) CheckUserTelephone(req *dto.TelephoneCheckUserRequest) (resp *dto.TelephoneCheckUserResponse, err error) {
	phoneNumber, err := phone.FormatToE164(req.TelephoneAreaCode, req.Telephone)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.TelephoneError), "Invalid phone number")
	}
	authMethods, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, "mobile", phoneNumber)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user by email error: %v", err.Error())
	}

	return &dto.TelephoneCheckUserResponse{
		Exist: authMethods.UserId != 0,
	}, nil
}
