package gateway

import (
	"context"
	"fmt"

	"github.com/perfect-panel/backend/internal/module/billing/internal/payment/epay"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment/epay/protocol"
)

func (g *epayGateway) queryOrder(ctx context.Context, orderNo string) (*epay.QueryResult, error) {
	if g.v2 == nil {
		return g.client.QueryOrder(ctx, orderNo)
	}
	result, err := g.v2.QueryOrder(ctx, protocol.OrderRef{OutTradeNo: orderNo})
	if err != nil {
		return nil, err
	}
	if result.RefundMoney != "" {
		refunded, err := protocol.ParseAmount(result.RefundMoney)
		if err != nil || refunded != 0 {
			return nil, fmt.Errorf("EPay V2 order has a refund requiring manual reconciliation: %w", ErrUnconfirmed)
		}
	}
	// The V2 protocol client already verifies the response's pid and signature.
	return &epay.QueryResult{
		MerchantID: g.config.Pid, TradeNo: result.TradeNo, OrderNo: result.OutTradeNo,
		Type: result.Type, Money: result.Money,
		Paid: result.Status == protocol.OrderPaid, Unpaid: result.Status == protocol.OrderUnpaid,
	}, nil
}

// V2 must confirm cancellation remotely. Neither an unsigned API error nor a
// failed request proves that an order is unpaid or closed, including owner closes.
func (g *epayGateway) reconcileV2(ctx context.Context, req CloseRequest) (Reconciliation, error) {
	o := req.Order
	result, err := g.queryOrder(ctx, o.OrderNo)
	if err != nil {
		return Reconciliation{}, fmt.Errorf("EPay V2 query failed: %w: %w", err, ErrUnconfirmed)
	}
	if err := g.validateQueriedDetails(result, &Notice{
		OrderNo: o.OrderNo, TradeNo: result.TradeNo, Amount: o.PaymentAmount,
	}, result.Type); err != nil {
		return Reconciliation{}, err
	}
	if o.TradeNo != "" && o.TradeNo != result.TradeNo {
		return Reconciliation{}, fmt.Errorf("EPay V2 queried a different payment: %w", ErrUnconfirmed)
	}
	if result.Paid {
		return Reconciliation{TradeNo: result.TradeNo}, nil
	}
	if !result.Unpaid {
		return Reconciliation{}, fmt.Errorf("EPay V2 order needs manual reconciliation: %w", ErrUnconfirmed)
	}
	closeErr := g.v2.CloseOrder(ctx, protocol.OrderRef{TradeNo: result.TradeNo})
	if closeErr == nil {
		return Reconciliation{RequireStableCheckout: true}, nil
	}
	// A payment may have won the race with cancellation; settle only a newly
	// authenticated paid response that still matches the same order and trade.
	latest, queryErr := g.queryOrder(ctx, o.OrderNo)
	if queryErr == nil && latest.Paid {
		if err := g.validateQueried(latest, &Notice{
			OrderNo: o.OrderNo, TradeNo: result.TradeNo, Amount: o.PaymentAmount,
		}, result.Type); err != nil {
			return Reconciliation{}, err
		}
		return Reconciliation{TradeNo: latest.TradeNo}, nil
	}
	return Reconciliation{}, fmt.Errorf("EPay V2 close unconfirmed: %w: %w", closeErr, ErrUnconfirmed)
}
