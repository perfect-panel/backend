package profile

import (
	"context"
	"strconv"

	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UnbindTelegram removes the calling account's Telegram binding and tells
// the chat, best effort.
func (s *Service) UnbindTelegram(ctx context.Context) error {
	u, err := currentUser(ctx)
	if err != nil {
		return err
	}
	log := logger.WithContext(ctx)
	method, err := s.deps.UserAuth.FindUserAuthMethodByPlatform(ctx, u.Id, "telegram")
	if err != nil {
		log.Errorw("UnbindTelegram FindUserAuthMethodByPlatform Error", logger.Field("id", u.Id), logger.Field("error", err.Error()))
		return xerr.Errorf(xerr.DatabaseQueryError, "Find User Auth Method By Platform Failed")
	}

	userTelegramChatId, err := strconv.ParseInt(method.AuthIdentifier, 10, 64)
	if err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "parse the telegram id of user %d", u.Id)
	}

	if userTelegramChatId == 0 {
		return xerr.Errorf(xerr.TelegramNotBound, "Unbind Telegram")
	}

	if err := s.deps.UserAuth.DeleteUserAuthMethods(ctx, u.Id, "telegram"); err != nil {
		log.Errorw("UnbindTelegram DeleteUserAuthMethods Error", logger.Field("id", u.Id), logger.Field("error", err.Error()))
		return xerr.Errorf(xerr.DatabaseDeletedError, "Delete User Auth Methods Failed")
	}
	// The unbind notice is best-effort: the composition root renders and
	// sends it through the runtime-configured bot.
	if err := s.deps.NotifyUnbind(ctx, u.Id, userTelegramChatId); err != nil {
		log.Errorw("UnbindTelegram Send Error", logger.Field("id", u.Id), logger.Field("error", err.Error()))
	}
	return nil
}
