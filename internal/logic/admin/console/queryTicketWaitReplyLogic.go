package console

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type QueryTicketWaitReplyLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryTicketWaitReplyLogic Query ticket wait reply
func NewQueryTicketWaitReplyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryTicketWaitReplyLogic {
	return &QueryTicketWaitReplyLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryTicketWaitReplyLogic) QueryTicketWaitReply() (resp *dto.TicketWaitRelpyResponse, err error) {
	count, err := l.svcCtx.Store.Ticket().QueryWaitReplyTotal(l.ctx)
	if err != nil {
		l.Errorw("[QueryTicketWaitReply] Query Database Error: ", logger.Field("error", err.Error()))
		return nil, err
	}
	return &dto.TicketWaitRelpyResponse{
		Count: count,
	}, nil
}
