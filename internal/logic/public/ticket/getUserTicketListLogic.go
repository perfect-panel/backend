package ticket

import (
	"context"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/pkg/logger"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetUserTicketListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get ticket list
func NewGetUserTicketListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserTicketListLogic {
	return &GetUserTicketListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserTicketListLogic) GetUserTicketList(req *dto.GetUserTicketListRequest) (resp *dto.GetUserTicketListResponse, err error) {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	l.Logger.Debugf("Current user: %v", u.Id)
	total, list, err := l.svcCtx.Store.Ticket().QueryTicketList(l.ctx, req.Page, req.Size, u.Id, req.Status, req.Search)
	if err != nil {
		l.Errorw("[GetUserTicketListLogic] Database Error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "QueryTicketList error: %v", err)
	}
	resp = &dto.GetUserTicketListResponse{
		Total: total,
		List:  make([]dto.Ticket, 0),
	}
	tool.DeepCopy(&resp.List, list)
	return
}
