package user

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type PreUnsubscribeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewPreUnsubscribeLogic Pre Unsubscribe
func NewPreUnsubscribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PreUnsubscribeLogic {
	return &PreUnsubscribeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PreUnsubscribeLogic) PreUnsubscribe(req *dto.PreUnsubscribeRequest) (resp *dto.PreUnsubscribeResponse, err error) {
	remainingAmount, err := CalculateRemainingAmount(l.ctx, l.svcCtx, req.Id)
	if err != nil {
		l.Errorw("[PreUnsubscribeLogic] Calculate Remaining Amount Error:", logger.Field("err", err.Error()))
		return nil, err
	}
	return &dto.PreUnsubscribeResponse{
		DeductionAmount: remainingAmount,
	}, nil
}
