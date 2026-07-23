package auth

import (
	"context"

	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
)

func upgradePasswordAfterLogin(ctx context.Context, svcCtx *svc.ServiceContext, log logger.Logger, userInfo *user.User, plainPassword string) {
	if userInfo == nil || userInfo.Id == 0 || plainPassword == "" {
		return
	}
	if !tool.PasswordNeedsRehash(userInfo.Algo, userInfo.Password) {
		return
	}

	nextHash := tool.EncodePassWord(plainPassword)
	updated, err := svcCtx.Store.User().UpgradePasswordHash(ctx, userInfo.Id, userInfo.Password, nextHash, tool.PasswordAlgoArgon2id, "")
	if err != nil {
		log.Errorw("failed to upgrade password hash",
			logger.Field("user_id", userInfo.Id),
			logger.Field("error", err.Error()),
		)
		return
	}
	if !updated {
		return
	}
	userInfo.Password = nextHash
	userInfo.Algo = tool.PasswordAlgoArgon2id
	userInfo.Salt = ""
}
