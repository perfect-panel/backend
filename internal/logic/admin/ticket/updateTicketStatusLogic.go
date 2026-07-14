package ticket

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateTicketStatusLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update ticket status
func NewUpdateTicketStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateTicketStatusLogic {
	return &UpdateTicketStatusLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateTicketStatusLogic) UpdateTicketStatus(req *dto.UpdateTicketStatusRequest) error {

	err := l.svcCtx.Store.Ticket().UpdateTicketStatus(l.ctx, req.Id, 0, *req.Status)
	if err != nil {
		l.Errorw("[UpdateTicketStatus] Update Database Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update ticket error: %v", err.Error())
	}
	return nil
}
