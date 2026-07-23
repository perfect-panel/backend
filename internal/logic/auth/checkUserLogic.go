package auth

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

type CheckUserLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewCheckUserLogic Check user is exist
func NewCheckUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CheckUserLogic {
	return &CheckUserLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CheckUserLogic) CheckUser(req *dto.CheckUserRequest) (resp *dto.CheckUserResponse, err error) {
	authMethod, err := l.svcCtx.Store.UserAuth().FindUserAuthMethodByOpenID(l.ctx, "email", req.Email)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user by email error: %v", err.Error())
	}
	return &dto.CheckUserResponse{
		Exist: authMethod.UserId != 0,
	}, nil
}
