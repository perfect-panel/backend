package checkout

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/orderaudit"
	"github.com/perfect-panel/backend/internal/module/billing/internal/ordercontext"
	"github.com/perfect-panel/backend/internal/module/billing/internal/pricing"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Recharge creates a balance recharge order. The recharged amount is the
// order price; the payment method's fee is charged on top.
func (s *Service) Recharge(ctx context.Context, req *dto.RechargeOrderRequest) (*dto.RechargeOrderResponse, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if req.Amount < MinRechargeAmount {
		return nil, xerr.Errorf(xerr.InvalidParams, "recharge amount must be at least %d", MinRechargeAmount)
	}
	if req.Amount > MaxRechargeAmount {
		return nil, xerr.Errorf(xerr.InvalidParams, "recharge amount exceeds maximum limit")
	}
	method, err := gateway.LookupMethod(ctx, s.deps.Payments, req.Payment)
	if err != nil {
		return nil, err
	}
	// A top-up must bring money in from outside the wallet. The balance
	// checkout spends gift credit first, so a balance-paid recharge would turn
	// gift credit into regular balance.
	if gateway.IsBalance(method) {
		return nil, xerr.Errorf(xerr.PaymentMethodNotFound, "balance cannot pay for a recharge")
	}
	quote := pricing.Compute(pricing.Input{UnitPrice: req.Amount, Quantity: 1, Fee: pricing.FeeTerms(method)})
	if err := orderAmountWithinLimit(quote.Amount); err != nil {
		return nil, err
	}
	isNew, err := s.deps.Orders.IsUserEligibleForNewOrder(ctx, u.Id)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find orders of user %d", u.Id)
	}
	orderInfo := &order.Order{
		UserId:    u.Id,
		OrderNo:   order.GenerateTradeNo(),
		Type:      order.TypeRecharge,
		Price:     quote.Price,
		Amount:    quote.Amount,
		FeeAmount: quote.FeeAmount,
		PaymentId: method.Id,
		Method:    method.Platform,
		Status:    order.StatusPending,
		IsNew:     isNew,
	}
	ordercontext.ApplyIdempotency(ctx, orderInfo)
	if err := s.deps.Tx.InBillingTx(ctx, func(tx repository.BillingStore) error {
		return InsertOrder(ctx, tx, orderInfo, orderaudit.SourceUser)
	}); err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseInsertError, "create recharge order")
	}
	s.enqueueDeferredClose(ctx, "[Recharge]", orderInfo.OrderNo)
	return &dto.RechargeOrderResponse{OrderNo: orderInfo.OrderNo}, nil
}
