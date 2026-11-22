package profile

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateUserNotify sets the notification switches the request names for the
// calling account; the others keep their value.
func (s *Service) UpdateUserNotify(ctx context.Context, req *dto.UpdateUserNotifyRequest) error {
	u, err := currentUser(ctx)
	if err != nil {
		return err
	}
	if u.Id == 0 {
		return xerr.Errorf(xerr.InvalidAccess, "no signed-in user")
	}
	columns := map[string]any{}
	for column, value := range map[string]*bool{
		"enable_login_notify":     req.EnableLoginNotify,
		"enable_balance_notify":   req.EnableBalanceNotify,
		"enable_subscribe_notify": req.EnableSubscribeNotify,
		"enable_trade_notify":     req.EnableTradeNotify,
	} {
		if value != nil {
			columns[column] = *value
		}
	}
	if err := s.deps.Users.UpdateColumns(ctx, u.Id, columns); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update notification settings of user %d", u.Id)
	}
	return nil
}
