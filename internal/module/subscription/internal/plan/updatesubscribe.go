package plan

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/subscribe"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateSubscribeLogic struct {
	logger.Logger
	ctx  context.Context
	deps Deps
}

// Update subscribe
func newUpdateSubscribeLogic(ctx context.Context, deps Deps) *UpdateSubscribeLogic {
	return &UpdateSubscribeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *UpdateSubscribeLogic) UpdateSubscribe(req *dto.UpdateSubscribeRequest) error {
	if err := validateSubscribeInput(req.UnitTime, req.UnitPrice, req.Replacement, req.Inventory, req.Traffic, req.SpeedLimit, req.DeviceLimit, req.Quota, req.DeductionRatio, req.ResetCycle, req.Discount); err != nil {
		return err
	}
	// Query the database to get the subscribe information
	_, err := l.deps.Plans.FindOne(l.ctx, req.Id)
	if err != nil {
		l.Logger.Error("[UpdateSubscribe] Database query error", logger.Field("error", err.Error()), logger.Field("subscribe_id", req.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get subscribe error: %v", err.Error())
	}
	discount := ""
	if len(req.Discount) > 0 {
		val, _ := json.Marshal(req.Discount)
		discount = string(val)
	}
	// When NodeTags is set, clear Nodes to avoid AND-combined query returning wrong results (#94)
	nodes := tool.Int64SliceToString(req.Nodes.Int64s())
	if len(req.NodeTags) > 0 {
		nodes = ""
	}
	sub := &subscribe.Subscribe{
		Id:                req.Id,
		Name:              req.Name,
		Language:          req.Language,
		Description:       req.Description,
		UnitPrice:         req.UnitPrice,
		UnitTime:          req.UnitTime,
		Discount:          discount,
		Replacement:       req.Replacement,
		Inventory:         req.Inventory,
		Traffic:           req.Traffic,
		SpeedLimit:        req.SpeedLimit,
		DeviceLimit:       req.DeviceLimit,
		Quota:             req.Quota,
		Nodes:             nodes,
		NodeTags:          tool.StringSliceToString(req.NodeTags),
		Show:              req.Show,
		Sell:              req.Sell,
		Sort:              req.Sort,
		DeductionRatio:    req.DeductionRatio,
		AllowDeduction:    req.AllowDeduction,
		ResetCycle:        req.ResetCycle,
		RenewalReset:      req.RenewalReset,
		ShowOriginalPrice: req.ShowOriginalPrice,
	}
	err = l.deps.Plans.Update(l.ctx, sub)
	if err != nil {
		l.Logger.Error("[UpdateSubscribe] update subscribe failed", logger.Field("error", err.Error()), logger.Field("subscribe", sub))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update subscribe error: %v", err.Error())
	}
	l.deps.notifyPlanChanged()
	return nil
}
