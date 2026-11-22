package usersub

import (
	"context"
	"uuid"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// ResetUserSubscribeToken rotates the subscription URL token and the node
// credential together: rotating only the token would leave anyone who
// already pulled the config connected. The previous credentials read under
// the row lock lose their cache entries with the commit.
func (s *Service) ResetUserSubscribeToken(ctx context.Context, req *dto.ResetUserSubscribeTokenRequest) error {
	var rotated *usersub.Subscribe
	err := s.deps.Store.InSubscriptionTx(ctx, func(store repository.SubscriptionStore) error {
		sub, err := store.UserSubscription().FindOneSubscribeForUpdate(ctx, req.UserSubscribeId)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "find subscription %d", req.UserSubscribeId)
		}
		rotation := repository.SubscriptionCredentialRotation{Previous: sub, Token: usersub.NewToken(), UUID: uuid.NewV4().String()}
		if err := store.UserSubscription().RotateSubscribeCredentials(ctx, []repository.SubscriptionCredentialRotation{rotation}); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "rotate credentials of subscription %d", req.UserSubscribeId)
		}
		rotated = sub
		return nil
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("[ResetUserSubscribeToken] Rotate failed", logger.Field("error", err.Error()), logger.Field("user_subscribe_id", req.UserSubscribeId))
		return err
	}
	// The node user lists carry the UUID.
	return s.clearPlanCaches(ctx, rotated.SubscribeId)
}
