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

// UpdateSubscribe replaces an existing plan's configuration after validating
// it, then tells the connected devices that a plan changed. A switch the
// request leaves out keeps its stored value: show and sell cannot be NULL,
// and writing NULL into the others would change what they mean.
func (s *Service) UpdateSubscribe(ctx context.Context, req *dto.UpdateSubscribeRequest) error {
	if err := validateSubscribeInput(req.UnitTime, req.UnitPrice, req.Replacement, req.Inventory, req.Traffic, req.SpeedLimit, req.DeviceLimit, req.Quota, req.DeductionRatio, req.ResetCycle, req.Discount); err != nil {
		return err
	}
	log := logger.WithContext(ctx)
	stored, err := s.deps.Plans.FindOne(ctx, req.Id)
	if err != nil {
		log.Error("[UpdateSubscribe] Database query error", logger.Field("error", err.Error()), logger.Field("subscribe_id", req.Id))
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "get subscribe error: %v", err.Error())
	}
	discount := ""
	if len(req.Discount) > 0 {
		val, _ := json.Marshal(req.Discount)
		discount = string(val)
	}
	// When NodeTags is set, clear Nodes to avoid AND-combined query returning wrong results (#94)
	nodes := slicesx.Int64SliceToString(req.Nodes.Int64s())
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
		NodeTags:          slicesx.StringSliceToString(req.NodeTags),
		Show:              keepIfOmitted(req.Show, stored.Show),
		Sell:              keepIfOmitted(req.Sell, stored.Sell),
		Sort:              req.Sort,
		DeductionRatio:    req.DeductionRatio,
		AllowDeduction:    keepIfOmitted(req.AllowDeduction, stored.AllowDeduction),
		ResetCycle:        req.ResetCycle,
		RenewalReset:      keepIfOmitted(req.RenewalReset, stored.RenewalReset),
		ShowOriginalPrice: req.ShowOriginalPrice,
	}
	if err := s.deps.Plans.Update(ctx, sub); err != nil {
		log.Error("[UpdateSubscribe] update subscribe failed", logger.Field("error", err.Error()), logger.Field("subscribe", sub))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update subscribe error: %v", err.Error())
	}
	s.deps.notifyPlanChanged()
	return nil
}

// keepIfOmitted is the requested switch, or the stored one when the request
// left it out.
func keepIfOmitted(requested, stored *bool) *bool {
	if requested == nil {
		return stored
	}
	return requested
}
