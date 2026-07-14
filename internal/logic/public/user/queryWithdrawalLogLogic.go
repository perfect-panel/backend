package user

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type QueryWithdrawalLogLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryWithdrawalLogLogic Query Withdrawal Log
func NewQueryWithdrawalLogLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryWithdrawalLogLogic {
	return &QueryWithdrawalLogLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryWithdrawalLogLogic) QueryWithdrawalLog(req *dto.QueryWithdrawalLogListRequest) (resp *dto.QueryWithdrawalLogListResponse, err error) {
	// todo: add your logic here and delete this line

	return
}
