package user

import (
	"context"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type BindTelegramLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Bind Telegram
func NewBindTelegramLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BindTelegramLogic {
	return &BindTelegramLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *BindTelegramLogic) BindTelegram() (resp *dto.BindTelegramResponse, err error) {
	session, ok := l.ctx.Value(constant.CtxKeySessionID).(string)
	if !ok || session == "" {
		l.Errorw("bind telegram failed: session id missing from context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	if l.svcCtx.Config.Telegram.BotName == "" {
		l.Errorw("bind telegram failed: telegram bot is not initialized")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "telegram bot is not configured")
	}
	return &dto.BindTelegramResponse{
		Url:       fmt.Sprintf("https://t.me/%s?start=%s", l.svcCtx.Config.Telegram.BotName, session),
		ExpiredAt: timeutil.Now().Add(300 * time.Second).UnixMilli(),
	}, nil
}
