package profile

import (
	"context"
	"fmt"
	"time"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/random"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// telegramBindTokenTTL bounds how long a deep link stays usable. The value is
// the expiry advertised to the client, so the two can no longer drift.
const telegramBindTokenTTL = 300 * time.Second

// BindTelegram returns the bot deep link that binds the calling account's
// Telegram chat once the user opens it.
func (s *Service) BindTelegram(ctx context.Context) (*dto.BindTelegramResponse, error) {
	u, ok := user.FromContext(ctx)
	if !ok {
		return nil, xerr.Errorf(xerr.InvalidAccess, "no signed-in user")
	}
	if s.deps.TelegramBotName() == "" {
		logger.WithContext(ctx).Errorw("bind telegram failed: telegram bot is not initialized")
		return nil, xerr.Errorf(xerr.TelegramBotUnavailable, "telegram bot is not configured")
	}

	// The deep link carries a dedicated single-use token rather than the
	// caller's session id: the link travels through Telegram chats and
	// screenshots, and a leaked session id would let its holder bind their
	// own Telegram account — and therefore log in — as this user.
	token := random.KeyNew(32, 1)
	expiredAt := timeutil.Now().Add(telegramBindTokenTTL)
	key := fmt.Sprintf("%s:%s", config.TelegramBindKey, token)
	if err := s.deps.Redis.Set(ctx, key, u.Id, telegramBindTokenTTL).Err(); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "store telegram bind token of user %d", u.Id)
	}

	return &dto.BindTelegramResponse{
		Url:       fmt.Sprintf("https://t.me/%s?start=%s", s.deps.TelegramBotName(), token),
		ExpiredAt: expiredAt.UnixMilli(),
	}, nil
}
