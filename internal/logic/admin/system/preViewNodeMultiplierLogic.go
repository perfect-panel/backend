package system

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
)

type PreViewNodeMultiplierLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// PreView Node Multiplier
func NewPreViewNodeMultiplierLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PreViewNodeMultiplierLogic {
	return &PreViewNodeMultiplierLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PreViewNodeMultiplierLogic) PreViewNodeMultiplier() (resp *dto.PreViewNodeMultiplierResponse, err error) {
	now := timeutil.Now()
	ratio := l.svcCtx.NodeMultiplierManager.GetMultiplier(now)
	return &dto.PreViewNodeMultiplierResponse{
		Ratio:       ratio,
		CurrentTime: now.Format("2006-01-02 15:04:05"),
	}, nil
}
