package ticket

import (
	"context"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/ticket"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type CreateUserTicketFollowLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create ticket follow
func NewCreateUserTicketFollowLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateUserTicketFollowLogic {
	return &CreateUserTicketFollowLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateUserTicketFollowLogic) CreateUserTicketFollow(req *dto.CreateUserTicketFollowRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	// query ticket
	t, err := l.svcCtx.Store.Ticket().FindOne(l.ctx, req.TicketId)
	if err != nil {
		l.Errorw("[CreateUserTicketFollow] Database query error", logger.Field("error", err.Error()), logger.Field("request", req))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query ticket failed: %v", err.Error())
	}
	// check access
	if u.Id != t.UserId {
		l.Errorw("[CreateUserTicketFollow] Invalid access", logger.Field("user_id", u.Id), logger.Field("ticket_user_id", t.UserId))
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "invalid access")
	}
	// insert follow
	err = l.svcCtx.Store.Ticket().InsertTicketFollow(l.ctx, &ticket.Follow{
		TicketId: req.TicketId,
		From:     req.From,
		Type:     req.Type,
		Content:  req.Content,
	})
	if err != nil {
		l.Errorw("[CreateUserTicketFollow] Database insert error", logger.Field("error", err.Error()), logger.Field("request", req))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create ticket follow failed: %v", err.Error())
	}
	err = l.svcCtx.Store.Ticket().UpdateTicketStatus(l.ctx, req.TicketId, u.Id, ticket.Pending)
	if err != nil {
		l.Errorw("[CreateUserTicketFollow] Database update error", logger.Field("error", err.Error()), logger.Field("status", ticket.Pending))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update ticket status failed: %v", err.Error())
	}
	return nil
}
