package authn

import (
	"context"

	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/logger"
)

// passwordRehasher stores hash as the password of the account id, provided
// its stored hash is still currentHash, and reports whether it did.
type passwordRehasher interface {
	UpgradePasswordHash(ctx context.Context, id int64, currentHash, hash, algo, salt string) (bool, error)
}

// upgradePasswordAfterLogin rehashes a password stored with a legacy
// algorithm once its owner proved it. The rehash is best-effort: a failure
// is logged and the sign-in goes on.
func upgradePasswordAfterLogin(ctx context.Context, users passwordRehasher, userInfo *user.User, plainPassword string) {
	if userInfo == nil || userInfo.Id == 0 || plainPassword == "" {
		return
	}
	if !password.PasswordNeedsRehash(userInfo.Algo, userInfo.Password) {
		return
	}

	nextHash := password.EncodePassWord(plainPassword)
	updated, err := users.UpgradePasswordHash(ctx, userInfo.Id, userInfo.Password, nextHash, password.PasswordAlgoArgon2id, "")
	if err != nil {
		logger.WithContext(ctx).Errorw("failed to upgrade password hash",
			logger.Field("user_id", userInfo.Id),
			logger.Field("error", err.Error()),
		)
		return
	}
	if !updated {
		return
	}
	userInfo.Password = nextHash
	userInfo.Algo = password.PasswordAlgoArgon2id
	userInfo.Salt = ""
}
