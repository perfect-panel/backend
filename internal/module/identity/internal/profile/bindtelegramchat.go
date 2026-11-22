package profile

import (
	"context"
	"errors"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// BindTelegramChat records chatID as the user's verified Telegram binding.
// It completes BindTelegram: the bot redeems the deep link's token, checks
// that neither the chat nor the account is bound yet and asks identity to
// store the binding. An account holds one Telegram binding: the write runs
// in a transaction that locks the account row and re-checks the binding, so
// two redemptions racing each other (a leaked link opened twice) cannot both
// insert; the table is unique on the chat, not on the account.
func (s *Service) BindTelegramChat(ctx context.Context, userID int64, chatID string) error {
	now := timeutil.Now()
	err := s.deps.Store.InIdentityTx(ctx, func(tx repository.IdentityStore) error {
		if _, err := tx.User().FindOneForUpdate(ctx, userID); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "find user %d", userID)
		}
		_, err := tx.UserAuth().FindUserAuthMethodByUserId(ctx, "telegram", userID)
		switch {
		case err == nil:
			return xerr.Errorf(xerr.UserExist, "user %d already has a telegram binding", userID)
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "find the telegram binding of user %d", userID)
		}
		if err := tx.UserAuth().InsertUserAuthMethods(ctx, &user.AuthMethods{
			UserId:         userID,
			AuthType:       "telegram",
			AuthIdentifier: chatID,
			Verified:       true,
			CreatedAt:      now,
			UpdatedAt:      now,
		}); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseInsertError, "bind telegram chat of user %d", userID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// The binding is stored; a stale cache entry only delays its visibility.
	if err := s.deps.UserCache.ClearUserCache(ctx, &user.User{Id: userID}); err != nil {
		logger.WithContext(ctx).Errorw("[Telegram] refresh user cache after bind failed",
			logger.Field("error", err.Error()), logger.Field("user_id", userID))
	}
	return nil
}
