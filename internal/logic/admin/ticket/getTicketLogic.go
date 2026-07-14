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

type GetTicketLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get ticket detail
func NewGetTicketLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTicketLogic {
	return &GetTicketLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetTicketLogic) GetTicket(req *dto.GetTicketRequest) (resp *dto.Ticket, err error) {
	data, err := l.svcCtx.Store.Ticket().QueryTicketDetail(l.ctx, req.Id)
	if err != nil {
		l.Errorw("[GetTicket] Query Database Error: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get ticket detail failed: %v", err.Error())
	}
	resp = &dto.Ticket{}
	tool.DeepCopy(resp, data)
	return
}
