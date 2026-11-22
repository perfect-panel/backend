package wallet

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// QueryUserAffiliate summarises the current user's referrals: how many
// accounts they referred and the net commission their commission log adds up
// to.
func (s *Service) QueryUserAffiliate(ctx context.Context) (*dto.QueryUserAffiliateCountResponse, error) {
	u, ok := user.FromContext(ctx)
	if !ok {
		logger.WithContext(ctx).Error("current user is not found in context")
		return nil, xerr.Errorf(xerr.InvalidAccess, "Invalid Access")
	}
	total, err := s.deps.Affiliates.CountAffiliates(ctx, u.Id)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "Query User Affiliate failed")
	}
	sum, err := s.deps.Logs.SumAmountByTypeAndObjectID(ctx, log.TypeCommission.Uint8(), u.Id)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "Query User Affiliate sum commission failed")
	}

	return &dto.QueryUserAffiliateCountResponse{
		Registers:       total,
		TotalCommission: sum,
	}, nil
}
