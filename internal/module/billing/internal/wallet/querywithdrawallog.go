package wallet

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// QueryWithdrawalLog pages the current user's withdrawal requests.
func (s *Service) QueryWithdrawalLog(ctx context.Context, req *dto.QueryWithdrawalLogListRequest) (*dto.QueryWithdrawalLogListResponse, error) {
	u, ok := user.FromContext(ctx)
	if !ok {
		return nil, xerr.Errorf(xerr.InvalidAccess, "current user is not found in context")
	}
	data, total, err := s.deps.Withdrawals.QueryWithdrawalList(ctx, u.Id, nil, req.Page, req.Size)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "query withdrawal log of user %d failed", u.Id)
	}
	list := make([]dto.WithdrawalLog, 0, len(data))
	for _, item := range data {
		list = append(list, withdrawalDTO(item))
	}
	return &dto.QueryWithdrawalLogListResponse{List: list, Total: total}, nil
}
