package marketing

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/task"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
)

type GetPreSendEmailCountLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewGetPreSendEmailCountLogic Get pre-send email count
func NewGetPreSendEmailCountLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetPreSendEmailCountLogic {
	return &GetPreSendEmailCountLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetPreSendEmailCountLogic) GetPreSendEmailCount(req *dto.GetPreSendEmailCountRequest) (resp *dto.GetPreSendEmailCountResponse, err error) {
	scope := task.ParseScopeType(req.Scope)
	count, err := l.svcCtx.Store.User().CountEmailRecipients(l.ctx, &user.EmailRecipientFilter{
		Scope:             scope.Int8(),
		RegisterStartTime: req.RegisterStartTime,
		RegisterEndTime:   req.RegisterEndTime,
	})
	if err != nil {
		l.Errorf("[GetPreSendEmailCount] Count error: %v", err)
		return nil, xerr.NewErrMsg("Failed to count emails")
	}

	return &dto.GetPreSendEmailCountResponse{
		Count: count,
	}, nil
}
