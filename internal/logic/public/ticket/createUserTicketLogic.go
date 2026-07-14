package ticket

import (
	"context"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/entity/ticket"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type CreateUserTicketLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create ticket
func NewCreateUserTicketLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateUserTicketLogic {
	return &CreateUserTicketLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateUserTicketLogic) CreateUserTicket(req *dto.CreateUserTicketRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	err := l.svcCtx.Store.Ticket().Insert(l.ctx, &ticket.Ticket{
		Title:       req.Title,
		Description: req.Description,
		UserId:      u.Id,
		Status:      ticket.Pending,
	})
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert ticket error: %v", err.Error())
	}
	return nil
}
