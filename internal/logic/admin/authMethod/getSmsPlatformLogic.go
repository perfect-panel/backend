package authMethod

import (
	"context"

	"github.com/perfect-panel/server/pkg/sms"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type GetSmsPlatformLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get sms support platform
func NewGetSmsPlatformLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSmsPlatformLogic {
	return &GetSmsPlatformLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetSmsPlatformLogic) GetSmsPlatform() (resp *dto.PlatformResponse, err error) {
	return &dto.PlatformResponse{
		List: sms.GetSupportedPlatforms(),
	}, nil
}
