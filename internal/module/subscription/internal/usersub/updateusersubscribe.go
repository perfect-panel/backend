package usersub

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// adminEditedColumns are the columns an administrator's subscription edit
// sets; the owner's note, the credentials and the dates the edit does not
// show keep their stored values.
var adminEditedColumns = []string{"subscribe_id", "expire_time", "traffic", "download", "upload", "status", "finished_at"}

// UpdateUserSubscribe applies an administrator's edit of plan, term and
// traffic. The status follows the new term: expired or active again.
func (s *Service) UpdateUserSubscribe(ctx context.Context, req *dto.UpdateUserSubscribeRequest) error {
	log := logger.WithContext(ctx)
	current, err := s.deps.UserSubs.FindOneSubscribe(ctx, req.UserSubscribeId)
	if err != nil {
		log.Errorw("[UpdateUserSubscribe] Find subscription failed", logger.Field("error", err.Error()), logger.Field("user_subscribe_id", req.UserSubscribeId))
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find subscription %d", req.UserSubscribeId)
	}
	if current.EntitlementSource != "" {
		return usersub.ErrProviderManaged
	}
	edited := *current
	edited.SubscribeId = req.SubscribeId
	edited.ExpireTime = usersub.ExpiryFromMilli(req.ExpiredAt)
	edited.Traffic, edited.Download, edited.Upload = req.Traffic, req.Download, req.Upload
	edited.Status = usersub.SubscribeStatusActive
	if edited.ExpiredAt(timeutil.Now()) {
		edited.Status = usersub.SubscribeStatusExpired
	}
	edited.FinishedAt = nil
	if err := s.deps.UserSubs.UpdateSubscribeColumns(ctx, &edited, adminEditedColumns...); err != nil {
		log.Errorw("[UpdateUserSubscribe] Update subscription failed", logger.Field("error", err.Error()), logger.Field("user_subscribe_id", req.UserSubscribeId))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update subscription %d", req.UserSubscribeId)
	}
	// The subscription may have moved between plans: both plans' node user
	// lists change.
	return s.clearPlanCaches(ctx, current.SubscribeId, edited.SubscribeId)
}

// clearPlanCaches drops the cached node user lists of the given plans after a
// committed change; the subscription's own entries go with its write.
func (s *Service) clearPlanCaches(ctx context.Context, planIDs ...int64) error {
	if err := s.deps.Plans.ClearCache(ctx, planIDs...); err != nil {
		logger.WithContext(ctx).Errorw("[UserSubscribe] Clear plan cache failed", logger.Field("error", err.Error()), logger.Field("subscribe_ids", planIDs))
		return xerr.Wrapf(err, xerr.ERROR, "clear plan cache")
	}
	return nil
}
