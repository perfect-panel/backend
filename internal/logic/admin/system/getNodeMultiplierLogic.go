package system

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetNodeMultiplierLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get Node Multiplier
func NewGetNodeMultiplierLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetNodeMultiplierLogic {
	return &GetNodeMultiplierLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetNodeMultiplierLogic) GetNodeMultiplier() (resp *dto.GetNodeMultiplierResponse, err error) {
	data, err := l.svcCtx.Store.System().FindNodeMultiplierConfig(l.ctx)
	if err != nil {
		l.Logger.Error("Get Node Multiplier Config Error: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Get Node Multiplier Config Error: %s", err.Error())
	}
	var periods []dto.TimePeriod
	if data.Value != "" {
		if err := json.Unmarshal([]byte(data.Value), &periods); err != nil {
			l.Logger.Error("Unmarshal Node Multiplier Config Error: ", logger.Field("error", err.Error()), logger.Field("value", data.Value))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Unmarshal Node Multiplier Config Error: %s", err.Error())
		}
	}

	return &dto.GetNodeMultiplierResponse{
		Periods: periods,
	}, nil
}
