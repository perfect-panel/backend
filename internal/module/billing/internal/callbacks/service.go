// Package callbacks implements the payment gateway callback subdomain of the
// billing module: it authenticates gateway notifications, verifies them
// against the order's immutable payment expectation, re-confirms with the
// gateway and settles the payment. The gateway-specific protocol lives in
// the gateway package; this is the one flow every gateway goes through. Only
// the module facade may reach it.
package callbacks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/settle"
	logEntity "github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// UnmatchedPaymentLog keeps the durable record of a gateway-confirmed payment
// that could not settle its order, and finds the record already kept for the
// same payment so a redelivered callback does not write it twice. The
// system log repository provides it.
type UnmatchedPaymentLog interface {
	Insert(ctx context.Context, data *logEntity.SystemLog) error
	FilterSystemLog(ctx context.Context, filter *logEntity.FilterParams) ([]*logEntity.SystemLog, int64, error)
}

// Option configures a Service.
type Option func(*Service)

// WithUnmatchedPaymentLog records the payments the flow cannot settle in
// logs; without it they are only reported in the process log.
func WithUnmatchedPaymentLog(logs UnmatchedPaymentLog) Option {
	return func(s *Service) { s.unmatched = logs }
}

type Service struct {
	orders    settle.Orders
	queue     settle.Queue
	gateways  *gateway.Registry
	unmatched UnmatchedPaymentLog
}

// NewService builds the callback flow; a nil registry selects the production
// gateways.
func NewService(orders settle.Orders, queue settle.Queue, gateways *gateway.Registry, opts ...Option) *Service {
	if gateways == nil {
		gateways = gateway.NewRegistry()
	}
	s := &Service{orders: orders, queue: queue, gateways: gateways}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Notify authenticates and settles a callback delivered to the notify URL of
// the payment method the notify middleware put in ctx. A rejection wraps
// gateway.ErrInvalidCallback when redelivery cannot succeed (see there); the
// log names the order and trade of an authenticated callback, so a payment
// refused after its order closed can be found at the gateway.
func (s *Service) Notify(ctx context.Context, n gateway.Notification) error {
	method, ok := ctx.Value(requestctx.CtxKeyPayment).(*payment.Payment)
	if !ok {
		return xerr.Errorf(xerr.ERROR, "payment config not found")
	}
	notice, err := s.notify(ctx, method, n)
	if err != nil {
		fields := []logger.LogField{logger.Field("platform", method.Platform), logger.Field("payment", method.Id), logger.Field("error", err.Error())}
		if notice != nil {
			fields = append(fields, logger.Field("orderNo", notice.OrderNo), logger.Field("tradeNo", notice.TradeNo))
		}
		logger.WithContext(ctx).Errorw("[PaymentNotify] Callback rejected", fields...)
		return err
	}
	return nil
}

// notify returns the authenticated notice with a failure that happened after
// authentication, for the rejection log.
func (s *Service) notify(ctx context.Context, method *payment.Payment, n gateway.Notification) (*gateway.Notice, error) {
	gw, err := s.gateways.Open(method)
	if err != nil {
		return nil, err
	}
	notice, err := gw.ParseCallback(ctx, n)
	if err != nil {
		return nil, gateway.InvalidCallback(err)
	}
	if notice.Ignore {
		return notice, nil
	}
	orderInfo, err := s.orders.FindOneByOrderNo(ctx, notice.OrderNo)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return notice, gateway.InvalidCallback(xerr.Errorf(xerr.OrderNotExist, "order not exist: %v", notice.OrderNo))
	}
	if err != nil {
		return notice, xerr.Wrapf(err, xerr.DatabaseQueryError, "find order %s", notice.OrderNo)
	}
	if err := validateOrderPayment(orderInfo, method); err != nil {
		return notice, gateway.InvalidCallback(err)
	}
	if err := gw.CheckOrder(orderInfo, notice); err != nil {
		return notice, gateway.InvalidCallback(err)
	}
	if !notice.Paid {
		return notice, s.acknowledgeLifecycle(ctx, method, orderInfo, notice)
	}
	// From here the callback is authenticated and announces money the
	// gateway collected. A payment that cannot settle its order is recorded
	// before it is rejected: the rejection is all the gateway learns, and the
	// record is what an operator refunds from.
	if finished, err := finishedOrderDuplicate(ctx, orderInfo, notice.TradeNo); err != nil || finished {
		if err != nil {
			s.recordUnmatched(ctx, method, orderInfo, notice, "the finished order was paid with another trade")
		}
		return notice, gateway.InvalidCallback(err)
	}
	if err := validateOrderCanSettle(orderInfo); err != nil {
		s.recordUnmatched(ctx, method, orderInfo, notice, fmt.Sprintf("the order was already %s when the payment arrived", order.StatusName(orderInfo.Status)))
		return notice, err
	}
	if orderInfo.TradeNo != "" && orderInfo.TradeNo != notice.TradeNo {
		// The order holds another gateway payment: a second payment of the
		// same order that settlement would refuse.
		s.recordUnmatched(ctx, method, orderInfo, notice, "the order is bound to another trade")
		return notice, errors.New("order trade number mismatch")
	}
	if err := validatePaymentExpectation(orderInfo, notice.Amount, notice.Currency); err != nil {
		return notice, gateway.InvalidCallback(err)
	}
	if err := gw.ConfirmPayment(ctx, orderInfo, notice); err != nil {
		return notice, err
	}
	if err := settle.VerifiedPayment(ctx, s.orders, s.queue, orderInfo, notice.TradeNo); err != nil {
		return notice, err
	}
	logger.WithContext(ctx).Infow("[PaymentNotify] Notify processed", logger.Field("platform", method.Platform), logger.Field("orderNo", orderInfo.OrderNo))
	return notice, nil
}

// acknowledgeLifecycle accepts a valid lifecycle event that does not
// announce a payment. It is not a failed payment callback: it is
// acknowledged without settling or downgrading the local order, even when
// delivery is out of order or a cancelled order is already closed. An event
// the gateway asks an operator to review (an underpaid, frozen or refunded
// invoice) concerns money the gateway holds for the order, so it is recorded
// as an unmatched payment for that operator.
func (s *Service) acknowledgeLifecycle(ctx context.Context, method *payment.Payment, orderInfo *order.Order, notice *gateway.Notice) error {
	if err := validatePaymentExpectation(orderInfo, notice.Amount, notice.Currency); err != nil {
		return gateway.InvalidCallback(err)
	}
	fields := append([]logger.LogField{
		logger.Field("orderNo", orderInfo.OrderNo),
		logger.Field("status", notice.Status),
		logger.Field("order_status", orderInfo.Status),
	}, notice.Fields...)
	if notice.ManualReview {
		s.recordUnmatched(ctx, method, orderInfo, notice, "the gateway asks for a manual review of the payment: "+notice.Status)
		logger.WithContext(ctx).Errorw("[PaymentNotify] Payment requires manual review", append(fields, logger.Field("requires_manual_review", true))...)
		return nil
	}
	logger.WithContext(ctx).Infow("[PaymentNotify] Payment status received without settlement", fields...)
	return nil
}

// recordUnmatched keeps the durable record of an authenticated payment
// callback that could not settle its order, once per payment: gateways
// redeliver a rejected callback, and the record of the first delivery is the
// one an operator works from. The record is best effort: a failure to keep
// it is reported and the callback is still rejected as before.
func (s *Service) recordUnmatched(ctx context.Context, method *payment.Payment, orderInfo *order.Order, notice *gateway.Notice, reason string) {
	log := logger.WithContext(ctx)
	fields := []logger.LogField{
		logger.Field("platform", method.Platform), logger.Field("payment", method.Id),
		logger.Field("orderNo", orderInfo.OrderNo), logger.Field("tradeNo", notice.TradeNo),
		logger.Field("amount", notice.Amount), logger.Field("currency", notice.Currency),
		logger.Field("userId", orderInfo.UserId), logger.Field("reason", reason),
	}
	if s.unmatched == nil {
		log.Errorw("[PaymentNotify] Unmatched payment could not be recorded: no log store is configured", fields...)
		return
	}
	recorded, err := s.unmatchedRecorded(ctx, orderInfo.UserId, orderInfo.OrderNo, notice.TradeNo)
	if err != nil {
		// Recording twice is better than not at all.
		log.Errorw("[PaymentNotify] Lookup of an earlier unmatched payment record failed", append(fields, logger.Field("error", err.Error()))...)
	}
	if recorded {
		log.Infow("[PaymentNotify] Unmatched payment redelivered; its record exists", fields...)
		return
	}
	metadata, _ := requestmeta.From(ctx)
	now := timeutil.Now()
	content, err := (&logEntity.UnmatchedPayment{
		Metadata:  metadata,
		OrderNo:   orderInfo.OrderNo,
		TradeNo:   notice.TradeNo,
		Platform:  method.Platform,
		Amount:    notice.Amount,
		Currency:  notice.Currency,
		Reason:    reason,
		Timestamp: now.UnixMilli(),
	}).Marshal()
	if err != nil {
		log.Errorw("[PaymentNotify] Unmatched payment could not be encoded", append(fields, logger.Field("error", err.Error()))...)
		return
	}
	if err := s.unmatched.Insert(ctx, &logEntity.SystemLog{
		Type:     logEntity.TypeUnmatchedPayment.Uint8(),
		Date:     now.Format(time.DateOnly),
		ObjectID: orderInfo.UserId,
		Content:  string(content),
	}); err != nil {
		log.Errorw("[PaymentNotify] Unmatched payment could not be recorded", append(fields, logger.Field("error", err.Error()))...)
		return
	}
	log.Errorw("[PaymentNotify] Unmatched payment recorded for manual refund", fields...)
}

// unmatchedRecorded reports whether the payment tradeNo of orderNo already
// has an unmatched payment record. The stored content is the JSON of
// UnmatchedPayment, in which the order number is immediately followed by
// the trade number, so that pair identifies the record.
func (s *Service) unmatchedRecorded(ctx context.Context, userID int64, orderNo, tradeNo string) (bool, error) {
	needle, err := unmatchedNeedle(orderNo, tradeNo)
	if err != nil {
		return false, err
	}
	rows, _, err := s.unmatched.FilterSystemLog(ctx, &logEntity.FilterParams{
		Type: logEntity.TypeUnmatchedPayment.Uint8(), ObjectID: userID, Search: needle, Page: 1, Size: 1, SkipCount: true,
	})
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		var recorded logEntity.UnmatchedPayment
		if err := recorded.Unmarshal([]byte(row.Content)); err == nil && recorded.OrderNo == orderNo && recorded.TradeNo == tradeNo {
			return true, nil
		}
	}
	return false, nil
}

// unmatchedNeedle is the fragment of the stored JSON that names the order
// and its trade, as UnmatchedPayment encodes them.
func unmatchedNeedle(orderNo, tradeNo string) (string, error) {
	fragment, err := json.Marshal(struct {
		OrderNo string `json:"order_no"`
		TradeNo string `json:"trade_no"`
	}{orderNo, tradeNo})
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimPrefix(string(fragment), "{"), "}"), nil
}

func validateOrderPayment(orderInfo *order.Order, method *payment.Payment) error {
	if orderInfo.PaymentId != method.Id {
		return errors.New("payment method mismatch")
	}
	if orderInfo.Method != method.Platform {
		return errors.New("payment platform mismatch")
	}
	return nil
}

func validatePaymentExpectation(orderInfo *order.Order, amount int64, currency string) error {
	if orderInfo.PaymentCurrency == "" {
		return errors.New("payment amount snapshot is missing; restart checkout")
	}
	if orderInfo.PaymentAmount != amount {
		return errors.New("payment amount mismatch")
	}
	if !strings.EqualFold(orderInfo.PaymentCurrency, currency) {
		return errors.New("payment currency mismatch")
	}
	return nil
}

// finishedOrderDuplicate reports whether the order is already in the finished
// state and the incoming callback is a safe duplicate.
//
// Historical orders created before trade_no persistence was introduced may
// have an empty TradeNo field.  Blocking those retried callbacks would
// permanently prevent them from being acknowledged.  Instead, a warning is
// emitted so the gap can be audited, and the callback is treated as a known
// duplicate so the gateway stops retrying.
func finishedOrderDuplicate(ctx context.Context, orderInfo *order.Order, tradeNo string) (bool, error) {
	if orderInfo.Status != order.StatusFinished {
		return false, nil
	}
	if err := settle.ValidateTradeNo(tradeNo); err != nil {
		return false, err
	}
	if orderInfo.TradeNo == "" {
		// Legacy order: trade_no was not persisted at payment time.
		// Warn for audit purposes and accept the duplicate gracefully.
		logger.WithContext(ctx).Infow("[finishedOrderDuplicate] finished order has no trade_no recorded; treating callback as duplicate",
			logger.Field("orderNo", orderInfo.OrderNo),
			logger.Field("incomingTradeNo", tradeNo),
		)
		return true, nil
	}
	if orderInfo.TradeNo != tradeNo {
		return false, errors.New("order trade number mismatch")
	}
	return true, nil
}

func validateOrderCanSettle(orderInfo *order.Order) error {
	if !order.CanSettle(orderInfo.Status) {
		return fmt.Errorf("invalid order status transition: %d", orderInfo.Status)
	}
	return nil
}
