package payment

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/payment"
)

type GetPaymentPlatformLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get supported payment platform
func NewGetPaymentPlatformLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetPaymentPlatformLogic {
	return &GetPaymentPlatformLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetPaymentPlatformLogic) GetPaymentPlatform() (resp *dto.PlatformResponse, err error) {
	resp = &dto.PlatformResponse{
		List: payment.GetSupportedPlatforms(),
	}
	return
}
