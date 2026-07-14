package ticket

import (
	"context"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetUserTicketDetailsLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get ticket detail
func NewGetUserTicketDetailsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserTicketDetailsLogic {
	return &GetUserTicketDetailsLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserTicketDetailsLogic) GetUserTicketDetails(req *dto.GetUserTicketDetailRequest) (resp *dto.Ticket, err error) {

	data, err := l.svcCtx.Store.Ticket().QueryTicketDetail(l.ctx, req.Id)
	if err != nil {
		l.Errorw("[GetUserTicketDetailsLogic] Database Error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get ticket detail failed: %v", err.Error())
	}
	// check access
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	if data.UserId != u.Id {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "invalid access")
	}
	resp = &dto.Ticket{}
	tool.DeepCopy(resp, data)
	return
}
