package selfsub

import (
	"context"
	"errors"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/deduction"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/period"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// remainingAmount is the refund for cancelling the subscription now: the
// unused share of what its order paid. A subscription without an order
// refunds nothing. A provider-managed subscription, one whose plan is gone
// or allows no deduction (outside single-subscription mode), and one that is
// not active are refused.
func (s *Service) remainingAmount(ctx context.Context, userSubscribeId int64) (int64, error) {
	userSubscribe, err := s.deps.UserSubs.FindOneUserSubscribe(ctx, userSubscribeId)
	if err != nil {
		logger.WithContext(ctx).Error("[CalculateRemainingAmount] FindOneUserSubscribe", logger.Field("err", err.Error()), logger.Field("id", userSubscribeId))
		return 0, xerr.Errorf(xerr.DatabaseQueryError, "FindOneUserSubscribe failed, id: %d", userSubscribeId)
	}
	if userSubscribe.EntitlementSource != "" {
		return 0, usersub.ErrProviderManaged
	}
	if userSubscribe.OrderId == 0 {
		return 0, nil
	}
	plan := userSubscribe.Subscribe
	if plan == nil {
		// The plan was deleted, and its refund rules with it.
		return 0, xerr.Errorf(xerr.SubscribeNotAvailable, "plan %d of subscription %d no longer exists", userSubscribe.SubscribeId, userSubscribeId)
	}
	// An unset flag (a row from before the column had a default) allows no
	// deduction, the safe reading.
	if (plan.AllowDeduction == nil || !*plan.AllowDeduction) && !s.deps.SingleModel() {
		return 0, xerr.Errorf(xerr.SubscribeNotAvailable, "plan %d does not allow deductions", plan.Id)
	}

	if userSubscribe.Status != usersub.SubscribeStatusActive {
		return 0, errors.New("the subscription package is not in use")
	}
	orderDetails, err := s.deps.Orders.FindOneDetails(ctx, userSubscribe.OrderId)
	if err != nil {
		logger.WithContext(ctx).Error("[CalculateRemainingAmount] FindOneDetails", logger.Field("err", err.Error()), logger.Field("id", userSubscribe.OrderId))
		return 0, xerr.Errorf(xerr.DatabaseQueryError, "FindOneDetails failed, id: %d", userSubscribe.OrderId)
	}
	remainingAmount, err := deduction.CalculateRemainingAmount(
		deduction.Subscribe{
			StartTime:      userSubscribe.StartTime,
			ExpireTime:     userSubscribe.ExpireTime,
			Traffic:        userSubscribe.Traffic,
			Download:       userSubscribe.Download,
			Upload:         userSubscribe.Upload,
			UnitTime:       period.Unit(plan.UnitTime),
			ResetCycle:     period.Cycle(plan.ResetCycle),
			DeductionRatio: plan.DeductionRatio,
		},
		deduction.Order{Amount: orderDetails.RefundBasis()},
	)
	if err != nil {
		return 0, xerr.Wrapf(err, xerr.ERROR, "calculate the refund of subscription %d: %v", userSubscribeId, err)
	}
	return remainingAmount, nil
}
