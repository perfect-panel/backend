package adminuser

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateUserNotifySetting sets which notifications an account receives.
func (s *Service) UpdateUserNotifySetting(ctx context.Context, req *dto.UpdateUserNotifySettingRequest) error {
	log := logger.WithContext(ctx)
	userInfo, err := s.deps.Users.FindOne(ctx, req.UserId)
	if err != nil {
		log.Errorw("[UpdateUserNotifySetting] Find User Error:", logger.Field("err", err.Error()), logger.Field("userId", req.UserId))
		return xerr.Errorf(xerr.DatabaseQueryError, "Find User Error")
	}
	err = s.deps.Users.UpdateColumns(ctx, userInfo.Id, map[string]any{
		"enable_balance_notify":   req.EnableBalanceNotify,
		"enable_login_notify":     req.EnableLoginNotify,
		"enable_subscribe_notify": req.EnableSubscribeNotify,
		"enable_trade_notify":     req.EnableTradeNotify,
	})
	if err != nil {
		log.Errorw("[UpdateUserNotifySetting] Update User Error:", logger.Field("err", err.Error()), logger.Field("userId", req.UserId))
		return xerr.Errorf(xerr.DatabaseUpdateError, "Update User Error")
	}
	return nil
}
