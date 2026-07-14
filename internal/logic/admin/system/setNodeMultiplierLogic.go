package system

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type SetNodeMultiplierLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Set Node Multiplier
func NewSetNodeMultiplierLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SetNodeMultiplierLogic {
	return &SetNodeMultiplierLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SetNodeMultiplierLogic) SetNodeMultiplier(req *dto.SetNodeMultiplierRequest) error {
	data, err := json.Marshal(req.Periods)
	if err != nil {
		l.Logger.Error("Marshal Node Multiplier Config Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Marshal Node Multiplier Config Error: %s", err.Error())
	}
	if err = l.svcCtx.Store.System().UpdateNodeMultiplierConfig(l.ctx, string(data)); err != nil {
		l.Logger.Error("Update Node Multiplier Config Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Update Node Multiplier Config Error: %s", err.Error())
	}
	// update Node Multiplier
	initialize.Node(l.svcCtx)

	return nil
}
