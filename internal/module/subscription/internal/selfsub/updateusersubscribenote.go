package selfsub

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateUserSubscribeNote stores the owner's note on their subscription. Only
// the note column is written, so a note edit racing the traffic accounting
// or the lifecycle sweep keeps what they wrote.
func (s *Service) UpdateUserSubscribeNote(ctx context.Context, req *dto.UpdateUserSubscribeNoteRequest) error {
	log := logger.WithContext(ctx)
	u, ok := user.FromContext(ctx)
	if !ok {
		log.Error("current user is not found in context")
		return xerr.NewErrCode(xerr.InvalidAccess)
	}
	details, err := s.deps.UserSubs.FindOneUserSubscribe(ctx, req.UserSubscribeId)
	if err != nil {
		log.Errorw("[UpdateUserSubscribeNote] Find subscription failed", logger.Field("error", err.Error()), logger.Field("user_subscribe_id", req.UserSubscribeId))
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find subscription %d", req.UserSubscribeId)
	}
	if details.UserId != u.Id {
		log.Errorw("[UpdateUserSubscribeNote] Subscription belongs to another user", logger.Field("user_subscribe_id", req.UserSubscribeId))
		return errNotOwner
	}
	sub := &usersub.Subscribe{
		Id: details.Id, UserId: details.UserId, Token: details.Token,
		EntitlementSource: details.EntitlementSource, Note: req.Note,
	}
	if err := s.deps.UserSubs.UpdateSubscribeColumns(ctx, sub, "note"); err != nil {
		log.Errorw("[UpdateUserSubscribeNote] Update note failed", logger.Field("error", err.Error()), logger.Field("user_subscribe_id", req.UserSubscribeId))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update note of subscription %d", req.UserSubscribeId)
	}
	return nil
}
