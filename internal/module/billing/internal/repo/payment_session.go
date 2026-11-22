package repo

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/settle"
	"gorm.io/gorm"
)

func (m *orderRepo) BeginPaymentSession(ctx context.Context, orderNo string) (bool, error) {
	return m.changePaymentSession(ctx, orderNo, func(conn *gorm.DB) *gorm.DB {
		return conn.Model(&order.Order{}).
			Where("order_no = ? AND status = ? AND payment_currency <> '' AND payment_amount > 0", orderNo, order.StatusPending).
			Where("(payment_session IS NULL OR payment_session = '') AND (trade_no IS NULL OR trade_no = '')").
			Update("payment_session", order.PaymentSessionStarting)
	})
}

func (m *orderRepo) CompletePaymentSession(ctx context.Context, orderNo, tradeNo, session string) (bool, error) {
	if err := settle.ValidateTradeNo(tradeNo); err != nil {
		return false, err
	}
	if session == "" || session == order.PaymentSessionStarting || len(session) > 64<<10 || !json.Valid([]byte(session)) {
		return false, errors.New("invalid payment session")
	}
	return m.changePaymentSession(ctx, orderNo, func(conn *gorm.DB) *gorm.DB {
		return conn.Model(&order.Order{}).
			Where("order_no = ? AND status = ? AND payment_session = ?", orderNo, order.StatusPending, order.PaymentSessionStarting).
			Where("trade_no IS NULL OR trade_no = '' OR trade_no = ?", tradeNo).
			Updates(map[string]any{"trade_no": tradeNo, "payment_session": session})
	})
}

func (m *orderRepo) changePaymentSession(ctx context.Context, orderNo string, update func(*gorm.DB) *gorm.DB) (bool, error) {
	snapshot, err := m.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return false, err
	}
	var updated bool
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		result := update(conn)
		updated = result.RowsAffected == 1
		return result.Error
	}, m.getCacheKeys(snapshot)...)
	return updated && err == nil, err
}
