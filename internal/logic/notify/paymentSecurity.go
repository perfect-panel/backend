package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/model/entity/payment"
	"github.com/perfect-panel/server/internal/svc"
	queueType "github.com/perfect-panel/server/queue/types"
)

const (
	orderStatusPending  = uint8(1)
	orderStatusPaid     = uint8(2)
	orderStatusFinished = uint8(5)
)

func validateOrderPayment(orderInfo *order.Order, paymentConfig *payment.Payment) error {
	if orderInfo.PaymentId != paymentConfig.Id {
		return errors.New("payment method mismatch")
	}
	if orderInfo.Method != paymentConfig.Platform {
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

func validateTradeNo(tradeNo string) error {
	if tradeNo == "" || len(tradeNo) > 255 || strings.TrimSpace(tradeNo) != tradeNo || !utf8.ValidString(tradeNo) {
		return errors.New("invalid trade number")
	}
	for _, char := range tradeNo {
		if char < 0x20 || char == 0x7f {
			return errors.New("invalid trade number")
		}
	}
	return nil
}

func finishedOrderDuplicate(orderInfo *order.Order, tradeNo string) (bool, error) {
	if orderInfo.Status != orderStatusFinished {
		return false, nil
	}
	if err := validateTradeNo(tradeNo); err != nil {
		return false, err
	}
	if orderInfo.TradeNo == "" || orderInfo.TradeNo != tradeNo {
		return false, errors.New("order trade number mismatch")
	}
	return true, nil
}

func validateOrderCanSettle(orderInfo *order.Order) error {
	if orderInfo.Status != orderStatusPending && orderInfo.Status != orderStatusPaid {
		return fmt.Errorf("invalid order status transition: %d", orderInfo.Status)
	}
	return nil
}

// markOrderPaidAndEnqueue implements callback idempotency. A callback may only
// perform Pending -> Paid. A retry for an already-paid order may recreate a
// previously failed queue insertion, while a deterministic task ID prevents
// concurrent callbacks from activating the order twice.
func markOrderPaidAndEnqueue(ctx context.Context, svcCtx *svc.ServiceContext, orderInfo *order.Order, tradeNo string) error {
	if err := validateTradeNo(tradeNo); err != nil {
		return err
	}
	if orderInfo.TradeNo != "" && orderInfo.TradeNo != tradeNo {
		return errors.New("order trade number mismatch")
	}

	switch orderInfo.Status {
	case orderStatusFinished:
		return nil
	case orderStatusPaid:
		// A prior callback may have committed the database update but failed to
		// contact Redis. Re-enqueue below so retries heal that partial failure.
	case orderStatusPending:
		updated, err := svcCtx.Store.Order().MarkOrderPaid(ctx, orderInfo.OrderNo, tradeNo)
		if err != nil {
			return err
		}
		if !updated {
			latest, err := svcCtx.Store.Order().FindOneByOrderNo(ctx, orderInfo.OrderNo)
			if err != nil {
				return err
			}
			if latest.TradeNo != "" && latest.TradeNo != tradeNo {
				return errors.New("order trade number mismatch")
			}
			if latest.Status == orderStatusFinished {
				return nil
			}
			if latest.Status != orderStatusPaid {
				return fmt.Errorf("invalid order status transition: %d", latest.Status)
			}
		}
	default:
		return fmt.Errorf("invalid order status transition: %d", orderInfo.Status)
	}

	payload, err := json.Marshal(queueType.ForthwithActivateOrderPayload{OrderNo: orderInfo.OrderNo})
	if err != nil {
		return err
	}
	task := asynq.NewTask(queueType.ForthwithActivateOrder, payload, asynq.MaxRetry(5))
	_, err = svcCtx.Queue.EnqueueContext(ctx, task, asynq.TaskID(queueType.ActivationTaskID(orderInfo.OrderNo)))
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}
