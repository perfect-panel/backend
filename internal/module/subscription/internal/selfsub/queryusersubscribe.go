package selfsub

import (
	"context"
	"encoding/json"
	"time"

	"github.com/perfect-panel/backend/internal/infra/mapping"
	"github.com/perfect-panel/backend/internal/infra/protocolkey"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/period"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// QueryUserSubscribe lists the owner's subscriptions with their next
// calendar traffic reset.
func (s *Service) QueryUserSubscribe(ctx context.Context) (*dto.QueryUserSubscribeListResponse, error) {
	log := logger.WithContext(ctx)
	u, ok := user.FromContext(ctx)
	if !ok {
		log.Error("current user is not found in context")
		return nil, xerr.NewErrCode(xerr.InvalidAccess)
	}
	data, err := s.deps.UserSubs.QueryUserSubscribe(ctx, u.Id, usersub.OwnerVisibleStatuses.Values()...)
	if err != nil {
		log.Errorw("[QueryUserSubscribe] Query subscriptions failed", logger.Field("error", err.Error()), logger.Field("user_id", u.Id))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "query subscriptions of user %d", u.Id)
	}

	resp := &dto.QueryUserSubscribeListResponse{
		List:  make([]dto.UserSubscribe, 0, len(data)),
		Total: int64(len(data)),
	}
	cal, now := period.App(), timeutil.Now()
	for _, item := range data {
		var sub dto.UserSubscribe
		if err := mapping.Copy(&sub, item); err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "map subscription %d", item.Id)
		}

		// The plan's discount tiers let the client offer a renewal of more
		// than one period.
		if item.Subscribe != nil && item.Subscribe.Discount != "" {
			var discounts []dto.SubscribeDiscount
			if err := json.Unmarshal([]byte(item.Subscribe.Discount), &discounts); err == nil {
				sub.Subscribe.Discount = discounts
			}
		}

		sub.Short, _ = protocolkey.FixedUniqueString(item.Token, 8, "")
		sub.ResetTime = nextResetTime(cal, &sub, now)
		resp.List = append(resp.List, sub)
	}
	return resp, nil
}

// nextResetTime is the subscription's next calendar traffic reset in Unix
// milliseconds, or 0 when its plan has none. The reset days are the ones the
// calendar reset applies, read in the calendar's zone. A row without a start
// time counts from its expiry.
func nextResetTime(cal period.Calendar, sub *dto.UserSubscribe, now time.Time) int64 {
	base := time.UnixMilli(sub.ExpireTime)
	if sub.StartTime > 0 {
		base = time.UnixMilli(sub.StartTime)
	}
	next, ok := cal.NextReset(period.Cycle(sub.Subscribe.ResetCycle), base, now)
	if !ok {
		return 0
	}
	return next.UnixMilli()
}
