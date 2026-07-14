package ticket

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetTicketListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get ticket list
func NewGetTicketListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTicketListLogic {
	return &GetTicketListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetTicketListLogic) GetTicketList(req *dto.GetTicketListRequest) (resp *dto.GetTicketListResponse, err error) {
	total, list, err := l.svcCtx.Store.Ticket().QueryTicketList(l.ctx, int(req.Page), int(req.Size), req.UserId, req.Status, req.Search)
	if err != nil {
		l.Errorw("[GetTicketList] Query Database Error: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "QueryTicketList error: %v", err)
	}
	resp = &dto.GetTicketListResponse{
		Total: total,
		List:  make([]dto.Ticket, 0),
	}
	tool.DeepCopy(&resp.List, list)
	return
}
