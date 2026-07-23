package marketing

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type QueryQuotaTaskPreCountLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryQuotaTaskPreCountLogic Query quota task pre-count
func NewQueryQuotaTaskPreCountLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryQuotaTaskPreCountLogic {
	return &QueryQuotaTaskPreCountLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryQuotaTaskPreCountLogic) QueryQuotaTaskPreCount(req *dto.QueryQuotaTaskPreCountRequest) (resp *dto.QueryQuotaTaskPreCountResponse, err error) {
	count, err := l.svcCtx.Store.UserSubscription().CountSubscribesByFilter(l.ctx, &user.SubscribeFilter{
		Subscribers: req.Subscribers,
		IsActive:    req.IsActive,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
	})
	if err != nil {
		l.Errorf("[QueryQuotaTaskPreCount] count error: %v", err.Error())
		return nil, err
	}

	return &dto.QueryQuotaTaskPreCountResponse{
		Count: count,
	}, nil
}
