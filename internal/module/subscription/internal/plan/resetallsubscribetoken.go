package plan

import (
	"context"
	"uuid"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// ResetAllSubscribeToken rotates the token and node credential of every
// subscription in its term, all or none: one transaction of batched column
// updates, each naming a few hundred rows, instead of one full-row save per
// subscription. The new credentials are random (crypto/rand tokens, version
// 4 UUIDs) like those of every other rotation.
func (s *Service) ResetAllSubscribeToken(ctx context.Context) (*dto.ResetAllSubscribeTokenResponse, error) {
	log := logger.WithContext(ctx)
	var planIDs []int64
	rotated := 0
	err := s.deps.Store.InSubscriptionTx(ctx, func(store repository.SubscriptionStore) error {
		list, err := store.UserSubscription().FindUserSubscribesByStatus(ctx, usersub.InTermStatuses.Values()...)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "list subscriptions in term")
		}
		rotations := make([]repository.SubscriptionCredentialRotation, 0, len(list))
		seen := make(map[int64]struct{})
		for _, sub := range list {
			rotations = append(rotations, repository.SubscriptionCredentialRotation{
				Previous: sub, Token: usersub.NewToken(), UUID: uuid.NewV4().String(),
			})
			if _, ok := seen[sub.SubscribeId]; !ok {
				seen[sub.SubscribeId] = struct{}{}
				planIDs = append(planIDs, sub.SubscribeId)
			}
		}
		if err := store.UserSubscription().RotateSubscribeCredentials(ctx, rotations); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "rotate subscription credentials")
		}
		rotated = len(rotations)
		return nil
	})
	if err != nil {
		log.Errorw("[ResetAllSubscribeToken] Rotate credentials failed", logger.Field("error", err.Error()))
		return &dto.ResetAllSubscribeTokenResponse{Success: false}, err
	}
	// The node user lists carry the rotated UUIDs.
	if err := s.deps.Plans.ClearCache(ctx, planIDs...); err != nil {
		log.Errorw("[ResetAllSubscribeToken] Clear plan caches failed", logger.Field("error", err.Error()), logger.Field("subscribe_ids", planIDs))
	}
	log.Infow("[ResetAllSubscribeToken] Rotated credentials", logger.Field("count", rotated))
	return &dto.ResetAllSubscribeTokenResponse{Success: true}, nil
}
