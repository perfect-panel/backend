package wallet

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// QueryUserBalanceLog lists the current user's latest 100 balance movements.
// An entry whose content cannot be decoded is logged and left out.
func (s *Service) QueryUserBalanceLog(ctx context.Context) (*dto.QueryUserBalanceLogListResponse, error) {
	u, ok := user.FromContext(ctx)
	if !ok {
		logger.WithContext(ctx).Error("current user is not found in context")
		return nil, xerr.Errorf(xerr.InvalidAccess, "Invalid Access")
	}

	data, total, err := s.deps.Logs.FilterSystemLog(ctx, &log.FilterParams{
		Page:     1,
		Size:     100,
		Type:     log.TypeBalance.Uint8(),
		ObjectID: u.Id,
	})
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "Query User Balance Log Error")
	}

	list := make([]dto.BillingBalanceLogSnapshot, 0)
	for _, datum := range data {
		var content log.Balance
		if err := content.Unmarshal([]byte(datum.Content)); err != nil {
			logger.WithContext(ctx).Errorf("[QueryUserBalanceLog] unmarshal balance log content failed: %v", err.Error())
			continue
		}
		list = append(list, dto.BillingBalanceLogSnapshot{
			UserId:           datum.ObjectID,
			Amount:           content.Amount,
			Type:             content.Type,
			OrderNo:          content.OrderNo,
			Balance:          content.Balance,
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

	return &dto.QueryUserBalanceLogListResponse{
		Total: total,
		List:  list,
	}, nil
}
