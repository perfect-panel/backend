package user

import (
	"context"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type UpdateUserPasswordLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update User Password
func NewUpdateUserPasswordLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserPasswordLogic {
	return &UpdateUserPasswordLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateUserPasswordLogic) UpdateUserPassword(req *dto.UpdateUserPasswordRequest) error {
	userInfo := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	//update the password
	userInfo.Password = tool.EncodePassWord(req.Password)
	// Reset algo to default, otherwise a migrated user (e.g. algo=md5salt/sha256salt)
	// would be locked out: the new hash is pbkdf2 but verification would still use the
	// old algorithm. Mirrors resetPasswordLogic and updateUserBasicInfoLogic.
	userInfo.Algo = "default"
	if err := l.svcCtx.Store.User().Update(l.ctx, userInfo); err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "Update user password error")
	}
	return nil
}
