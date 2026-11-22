package adminuser

import (
	"context"

	"github.com/perfect-panel/backend/internal/infra/mapping"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// CurrentUser returns the signed-in administrator's account with its wallet.
func (s *Service) CurrentUser(ctx context.Context) (*dto.User, error) {
	log := logger.WithContext(ctx)
	u, ok := user.FromContext(ctx)
	if !ok {
		log.Error("current user is not found in context")
		return nil, xerr.Errorf(xerr.InvalidAccess, "Invalid Access")
	}

	log.Infow("current user", logger.Field("user_id", u.Id))
	resp := &dto.User{}
	if err := mapping.Copy(resp, u); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "map user %d", u.Id)
	}
	// The context user is the middleware's cached identity row; wallet
	// values come from the billing-owned table, and a read failure fails
	// the request rather than rendering zero balances.
	w, err := s.deps.Wallet.FindWallet(ctx, u.Id)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "load user wallet error: %v", err.Error())
	}
	if w != nil {
		resp.Balance = w.Balance
		resp.GiftAmount = w.GiftAmount
		resp.Commission = w.Commission
	}
	return resp, nil
}
