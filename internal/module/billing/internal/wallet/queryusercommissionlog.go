package wallet

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// QueryUserCommissionLog pages the current user's commission movements. An
// entry whose content cannot be decoded is logged and left out.
func (s *Service) QueryUserCommissionLog(ctx context.Context, req *dto.QueryUserCommissionLogListRequest) (*dto.QueryUserCommissionLogListResponse, error) {
	u, ok := user.FromContext(ctx)
	if !ok {
		logger.WithContext(ctx).Error("current user is not found in context")
		return nil, xerr.Errorf(xerr.InvalidAccess, "Invalid Access")
	}
	data, total, err := s.deps.Logs.FilterSystemLog(ctx, &log.FilterParams{
		Page:     req.Page,
		Size:     req.Size,
		Type:     log.TypeCommission.Uint8(),
		ObjectID: u.Id,
	})
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "Query User Commission Log failed")
	}
	var list []dto.BillingCommissionLogSnapshot

	for _, datum := range data {
		var content log.Commission
		if err := content.Unmarshal([]byte(datum.Content)); err != nil {
			logger.WithContext(ctx).Errorf("unmarshal commission log content failed: %v", err.Error())
			continue
		}
		list = append(list, dto.BillingCommissionLogSnapshot{
			UserId: datum.ObjectID,
			Type:   content.Type,
			Amount: content.Amount,
			// OrderNo is left out: it belongs to the referee's order, and
			// subscription tokens issued before random tokens were derived
			// from it.
			Timestamp:        content.Timestamp,
			ClientIP:         content.ClientIP,
			UserAgent:        content.UserAgent,
			ActorID:          content.ActorID,
			IPCountryCode:    content.IPCountryCode,
			IPCountry:        content.IPCountry,
			IPRegion:         content.IPRegion,
			IPCity:           content.IPCity,
			IPASN:            content.IPASN,
			IPASOrganization: content.IPASOrganization,
		})
	}

	return &dto.QueryUserCommissionLogListResponse{
		List:  list,
		Total: total,
	}, nil
}
