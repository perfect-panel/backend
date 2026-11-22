package plan

import (
	"context"
	"errors"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

var errorIsExistActiveUser = errors.New("subscription ID belongs to a current user subscription")

// BatchDeleteSubscribe deletes the plans, all or none: one plan with a
// current user subscription (see DeleteSubscribe) keeps every plan of the
// batch.
func (s *Service) BatchDeleteSubscribe(ctx context.Context, req *dto.BatchDeleteSubscribeRequest) error {
	log := logger.WithContext(ctx)
	err := s.deps.Store.InSubscriptionTx(ctx, func(store repository.SubscriptionStore) error {
		for _, id := range req.Ids {
			count, err := store.UserSubscription().CountUserSubscribesBySubscribeIdAndStatus(ctx, id, usersub.CurrentStatuses.Values()...)
			if err != nil {
				log.Error("[BatchDeleteSubscribe] Query Subscribe Error: ", logger.Field("error", err.Error()))
				return err
			}
			if count > 0 {
				return errorIsExistActiveUser
			}
			if err := store.Subscribe().Delete(ctx, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errorIsExistActiveUser) {
			return xerr.Errorf(xerr.SubscribeIsUsedError, "subscription ID belongs to an active user subscription")
		}
		log.Error("[BatchDeleteSubscribe] Transaction Error: ", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseDeletedError, "delete subscribe failed: %v", err.Error())
	}
	return nil
}
