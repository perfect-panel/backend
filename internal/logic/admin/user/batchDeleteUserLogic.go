package user

import (
	"context"
	"os"
	"strings"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type BatchDeleteUserLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logger.Logger
}

func NewBatchDeleteUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchDeleteUserLogic {
	return &BatchDeleteUserLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logger.WithContext(ctx),
	}
}

func (l *BatchDeleteUserLogic) BatchDeleteUser(req *dto.BatchDeleteUserRequest) error {
	isDemo := strings.ToLower(os.Getenv("PPANEL_MODE")) == "demo"

	if tool.Contains(req.Ids, 2) && isDemo {
		return errors.Wrapf(xerr.NewErrCodeMsg(503, "Demo mode does not allow deletion of the admin user"), "BatchDeleteUser failed: cannot delete admin user in demo mode")
	}

	err := l.svcCtx.Store.User().BatchDeleteUser(l.ctx, req.Ids)
	if err != nil {
		l.Logger.Error("[BatchDeleteUserLogic] BatchDeleteUser failed: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "BatchDeleteUser failed: %v", err.Error())
	}
	return nil
}
