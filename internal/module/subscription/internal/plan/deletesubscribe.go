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

// DeleteSubscribe deletes a plan no current user subscription holds: none
// that is pending, active or exhausted inside its term (usersub
// CurrentStatuses), since such a subscription may still be reset, renewed or
// delivered from the plan. Ended subscriptions (expired, refunded, stopped)
// keep pointing at the deleted plan; delivery and the owner's node list treat
// a missing plan as a subscription without nodes. The check and the delete
// share one transaction.
func (s *Service) DeleteSubscribe(ctx context.Context, req *dto.DeleteSubscribeRequest) error {
	// phase tells a failed check from a failed delete for the error code.
	phase := "check"
	err := s.deps.Store.InSubscriptionTx(ctx, func(store repository.SubscriptionStore) error {
		total, err := store.UserSubscription().CountUserSubscribesBySubscribeIdAndStatus(ctx, req.Id, usersub.CurrentStatuses.Values()...)
		if err != nil {
			return err
		}
		if total != 0 {
			return errorIsExistActiveUser
		}
		phase = "delete"
		return store.Subscribe().Delete(ctx, req.Id)
	})
	if err != nil {
		if errors.Is(err, errorIsExistActiveUser) {
			return xerr.Errorf(xerr.SubscribeIsUsedError, "subscribe is used")
		}
		log := logger.WithContext(ctx)
		if phase == "delete" {
			log.Error("[DeleteSubscribeLogic] delete subscribe failed: ", logger.Field("error", err.Error()))
			return xerr.Wrapf(err, xerr.DatabaseDeletedError, "delete subscribe failed: %v", err.Error())
		}
		log.Error("[DeleteSubscribeLogic] check subscribe failed: ", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "check subscribe failed: %v", err.Error())
	}
	return nil
}
