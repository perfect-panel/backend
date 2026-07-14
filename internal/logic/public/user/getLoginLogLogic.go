package user

import (
	"context"

	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetLoginLogLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get Login Log
func NewGetLoginLogLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetLoginLogLogic {
	return &GetLoginLogLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetLoginLogLogic) GetLoginLog(req *dto.GetLoginLogRequest) (resp *dto.GetLoginLogResponse, err error) {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	data, total, err := l.svcCtx.Store.Log().FilterSystemLog(l.ctx, &log.FilterParams{
		Page:     req.Page,
		Size:     req.Size,
		Type:     log.TypeLogin.Uint8(),
		ObjectID: u.Id,
	})
	if err != nil {
		l.Errorw("find login log failed:", logger.Field("error", err.Error()), logger.Field("user_id", u.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find login log failed: %v", err.Error())
	}
	list := make([]dto.UserLoginLog, 0)

	for _, datum := range data {
		var content log.Login
		if err = content.Unmarshal([]byte(datum.Content)); err != nil {
			l.Errorf("[GetUserLoginLogs] unmarshal login log content failed: %v", err.Error())
			continue
		}
		list = append(list, dto.UserLoginLog{
			Id:        datum.Id,
			UserId:    datum.ObjectID,
			LoginIP:   content.LoginIP,
			UserAgent: content.UserAgent,
			Success:   content.Success,
			Timestamp: datum.CreatedAt.UnixMilli(),
		})
	}

	return &dto.GetLoginLogResponse{
		Total: total,
		List:  list,
	}, nil
}
