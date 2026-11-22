package portal

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetAvailablePaymentMethods lists the payment methods enabled for checkout.
func (s *Service) GetAvailablePaymentMethods(ctx context.Context) (*dto.GetAvailablePaymentMethodsResponse, error) {
	data, err := s.deps.Payments.FindAvailableMethods(ctx)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "list available payment methods")
	}
	resp := &dto.GetAvailablePaymentMethodsResponse{
		List: make([]dto.PaymentMethod, 0, len(data)),
	}
	for _, method := range data {
		resp.List = append(resp.List, paymentMethodDTO(method))
	}
	return resp, nil
}

// paymentMethodDTO describes a payment method to a buyer: what it is called
// and what it charges, never its configuration, domain or notify token.
func paymentMethodDTO(method *payment.Payment) dto.PaymentMethod {
	if method == nil {
		return dto.PaymentMethod{}
	}
	return dto.PaymentMethod{
		Id:          method.Id,
		Name:        method.Name,
		Platform:    method.Platform,
		Description: method.Description,
		Icon:        method.Icon,
		FeeMode:     method.FeeMode,
		FeePercent:  method.FeePercent,
		FeeAmount:   method.FeeAmount,
		Sort:        method.Sort,
	}
}
