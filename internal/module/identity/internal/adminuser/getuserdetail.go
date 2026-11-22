package adminuser

import (
	"context"

	"github.com/perfect-panel/backend/internal/infra/mapping"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetUserDetail returns an account with its wallet, for the admin edit form.
func (s *Service) GetUserDetail(ctx context.Context, req *dto.GetDetailRequest) (*dto.User, error) {
	userInfo, err := s.deps.Users.FindOne(ctx, req.Id)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "get user detail error: %v", err.Error())
	}
	resp := dto.User{}
	if err := mapping.Copy(&resp, userInfo); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "map user %d", userInfo.Id)
	}
	// Wallet values come from the billing-owned table. A read failure must
	// fail the request: this response populates the admin edit form, and a
	// silently zeroed balance would round-trip into a real adjustment.
	w, err := s.deps.Wallet.FindWallet(ctx, userInfo.Id)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "load user wallet error: %v", err.Error())
	}
	if w != nil {
		resp.Balance = w.Balance
		resp.GiftAmount = w.GiftAmount
		resp.Commission = w.Commission
	}
	return &resp, nil
}
