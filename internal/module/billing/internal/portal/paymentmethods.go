package portal

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

// GetAvailablePaymentMethods lists the payment methods enabled for checkout.
func (s *Service) GetAvailablePaymentMethods(ctx context.Context) (*dto.GetAvailablePaymentMethodsResponse, error) {
	data, err := s.deps.Payments.FindAvailableMethods(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetAvailablePaymentMethods] database error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetAvailablePaymentMethods: %v", err.Error())
	}
	resp := &dto.GetAvailablePaymentMethodsResponse{
		List: make([]dto.PaymentMethod, 0),
	}

	tool.DeepCopy(&resp.List, data)

	return resp, nil
}
