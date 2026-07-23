package oauth

import (
	"context"

	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/logger"
)

func clearTrialSubscribeCache(ctx context.Context, userCache repository.UserCacheRepo, plans repository.SubscribeRepo, trialSub *user.Subscribe) {
	if trialSub == nil {
		return
	}
	if err := userCache.ClearSubscribeCache(ctx, trialSub); err != nil {
		logger.WithContext(ctx).Errorw("ClearSubscribeCache failed",
			logger.Field("error", err.Error()),
			logger.Field("user_subscribe_id", trialSub.Id),
		)
	}
	if err := plans.ClearCache(ctx, trialSub.SubscribeId); err != nil {
		logger.WithContext(ctx).Errorw("Clear subscribe cache failed",
			logger.Field("error", err.Error()),
			logger.Field("subscribe_id", trialSub.SubscribeId),
		)
	}
}
