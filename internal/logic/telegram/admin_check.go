package telegram

import (
	"context"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

// adminAuth resolves a Telegram message sender to an admin user.
// Returns the admin user record, or a rejection message if unauthorized.
func adminAuth(ctx context.Context, svcCtx *svc.ServiceContext, msg *tgbotapi.Message) (admin *user.User, rejectMsg string) {
	chatID := strconv.FormatInt(msg.Chat.ID, 10)

	auth, err := svcCtx.Store.User().FindUserAuthMethodByOpenID(ctx, "telegram", chatID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logger.WithContext(ctx).Infow("admin auth: Telegram not bound", logger.Field("chat_id", msg.Chat.ID))
			return nil, "您的 Telegram 尚未绑定账号。\n请登录 Web 后台 → 个人设置 → 绑定 Telegram。"
		}
		logger.WithContext(ctx).Errorw("admin auth: query auth method failed", logger.Field("error", err.Error()))
		return nil, "系统错误，请稍后再试。"
	}

	u, err := svcCtx.Store.User().FindOne(ctx, auth.UserId)
	if err != nil {
		logger.WithContext(ctx).Errorw("admin auth: query user failed", logger.Field("error", err.Error()), logger.Field("user_id", auth.UserId))
		return nil, "系统错误，请稍后再试。"
	}

	if u.IsAdmin == nil || !*u.IsAdmin {
		logger.WithContext(ctx).Infow("admin auth: user is not admin", logger.Field("user_id", u.Id))
		return nil, "您没有管理权限。"
	}

	return u, ""
}

// resolveTelegramUser resolves a Telegram chat ID to a user, regardless of admin status.
// Used when sending messages to users via their bound Telegram.
func resolveTelegramUser(ctx context.Context, svcCtx *svc.ServiceContext, chatID int64) (*user.User, bool) {
	auth, err := svcCtx.Store.User().FindUserAuthMethodByOpenID(ctx, "telegram", strconv.FormatInt(chatID, 10))
	if err != nil {
		return nil, false
	}
	u, err := svcCtx.Store.User().FindOne(ctx, auth.UserId)
	if err != nil {
		return nil, false
	}
	return u, true
}
