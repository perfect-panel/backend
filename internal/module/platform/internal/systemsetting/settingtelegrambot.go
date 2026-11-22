package systemsetting

import (
	"context"

	"github.com/perfect-panel/backend/pkg/logger"
)

// SettingTelegramBot reloads the Telegram bot from its stored settings. The
// reload re-registers the webhook and the command menu, so it is recorded
// like a settings change; a trail that cannot be written is logged, the
// reload being what the administrator asked for.
func (s *Service) SettingTelegramBot(ctx context.Context) error {
	if err := s.deps.Store.InSettingsTx(ctx, func(store SettingsStore) error {
		return recordSettingsAction(ctx, store, "settings.reload", "telegram", "")
	}); err != nil {
		logger.WithContext(ctx).Errorw("[SettingTelegramBot] record admin action failed", logger.Field("error", err.Error()))
	}
	return s.deps.reinit("telegram")
}
