package ticket

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/ticket"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type CreateTicketFollowLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create ticket follow
func NewCreateTicketFollowLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateTicketFollowLogic {
	return &CreateTicketFollowLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateTicketFollowLogic) CreateTicketFollow(req *dto.CreateTicketFollowRequest) (err error) {
	// find ticket
	_, err = l.svcCtx.Store.Ticket().FindOne(l.ctx, req.TicketId)
	if err != nil {
		l.Errorw("[CreateTicketFollow] FindOne error", logger.Field("error", err.Error()), logger.Field("ticketId", req.TicketId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find ticket failed: %v", err.Error())
	}
	err = l.svcCtx.Store.Ticket().InsertTicketFollow(l.ctx, &ticket.Follow{
		TicketId: req.TicketId,
		From:     req.From,
		Type:     req.Type,
		Content:  req.Content,
	})
	if err != nil {
		l.Errorw("[CreateTicketFollow] Database insert error", logger.Field("error", err.Error()), logger.Field("request", req))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create ticket follow failed: %v", err.Error())
	}
	err = l.svcCtx.Store.Ticket().UpdateTicketStatus(l.ctx, req.TicketId, 0, ticket.Waiting)
	if err != nil {
		l.Errorw("[CreateTicketFollow] Database update error", logger.Field("error", err.Error()), logger.Field("status", ticket.Waiting))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update ticket status failed: %v", err.Error())
	}
	return
}
