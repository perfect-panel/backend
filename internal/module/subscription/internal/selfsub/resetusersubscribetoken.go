package selfsub

import (
	"context"
	"uuid"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// errNotOwner rejects an operation on another user's subscription.
var errNotOwner = xerr.NewErrCode(xerr.InvalidAccess)

// ResetUserSubscribeToken rotates the owner's subscription token and node
// credential. The row is read under lock, so the previous credentials whose
// cache entries the commit invalidates are the stored ones.
func (s *Service) ResetUserSubscribeToken(ctx context.Context, req *dto.ResetUserSubscribeTokenRequest) error {
	log := logger.WithContext(ctx)
	u, ok := user.FromContext(ctx)
	if !ok {
		log.Error("current user is not found in context")
		return xerr.NewErrCode(xerr.InvalidAccess)
	}
	var rotated *usersub.Subscribe
	err := s.deps.Store.InSubscriptionTx(ctx, func(store repository.SubscriptionStore) error {
		sub, err := store.UserSubscription().FindOneSubscribeForUpdate(ctx, req.UserSubscribeId)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "find subscription %d", req.UserSubscribeId)
		}
		if sub.UserId != u.Id {
			return errNotOwner
		}
		rotation := repository.SubscriptionCredentialRotation{Previous: sub, Token: usersub.NewToken(), UUID: uuid.NewV4().String()}
		if err := store.UserSubscription().RotateSubscribeCredentials(ctx, []repository.SubscriptionCredentialRotation{rotation}); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "rotate credentials of subscription %d", req.UserSubscribeId)
		}
		rotated = sub
		return nil
	})
	if err != nil {
		log.Errorw("[ResetUserSubscribeToken] Rotate failed", logger.Field("error", err.Error()), logger.Field("user_subscribe_id", req.UserSubscribeId))
		return err
	}
	// The node user lists carry the UUID.
	if err := s.deps.Plans.ClearCache(ctx, rotated.SubscribeId); err != nil {
		log.Errorw("[ResetUserSubscribeToken] Clear plan cache failed", logger.Field("error", err.Error()), logger.Field("subscribe_id", rotated.SubscribeId))
		return xerr.Wrapf(err, xerr.ERROR, "clear plan cache")
	}
	return nil
}
