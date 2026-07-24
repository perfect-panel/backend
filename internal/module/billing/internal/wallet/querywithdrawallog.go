package wallet

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/pkg/logger"
)

type QueryWithdrawalLogLogic struct {
	logger.Logger
	ctx    context.Context
	deps Deps
}

// NewQueryWithdrawalLogLogic Query Withdrawal Log
func newQueryWithdrawalLogLogic(ctx context.Context, deps Deps) *QueryWithdrawalLogLogic {
	return &QueryWithdrawalLogLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *QueryWithdrawalLogLogic) QueryWithdrawalLog(req *dto.QueryWithdrawalLogListRequest) (resp *dto.QueryWithdrawalLogListResponse, err error) {
	// todo: add your logic here and delete this line

	return
}
