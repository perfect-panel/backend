package plan

import (
	"context"
	"encoding/json"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/slicesx"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// CreateSubscribe stores a new plan after validating its configuration; it
// is sorted after the existing plans.
func (s *Service) CreateSubscribe(ctx context.Context, req *dto.CreateSubscribeRequest) error {
	if err := validateSubscribeInput(req.UnitTime, req.UnitPrice, req.Replacement, req.Inventory, req.Traffic, req.SpeedLimit, req.DeviceLimit, req.Quota, req.DeductionRatio, req.ResetCycle, req.Discount); err != nil {
		return err
	}
	discount := ""
	if len(req.Discount) > 0 {
		val, _ := json.Marshal(req.Discount)
		discount = string(val)
	}
	sub := &subscribe.Subscribe{
		Id:                0,
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
		Nodes:             slicesx.Int64SliceToString(req.Nodes.Int64s()),
		NodeTags:          storedNodeTags(req.NodeTags),
		Show:              req.Show,
		Sell:              req.Sell,
		Sort:              0,
		DeductionRatio:    req.DeductionRatio,
		AllowDeduction:    req.AllowDeduction,
		ResetCycle:        req.ResetCycle,
		RenewalReset:      req.RenewalReset,
		ShowOriginalPrice: req.ShowOriginalPrice,
	}
	err := s.deps.Plans.Insert(ctx, sub)
	if err != nil {
		logger.WithContext(ctx).Error("[CreateSubscribeLogic] create subscribe error: ", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "create subscribe error: %v", err.Error())
	}

	return nil
}
