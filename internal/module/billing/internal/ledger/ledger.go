// Package ledger writes the wallet movement records of the billing module:
// every gift-credit and balance change an order makes is recorded here, with
// the same shape, timestamp and remark vocabulary. Callers pass the
// transaction-scoped log repository so a record commits with the movement it
// describes.
package ledger

import (
	"context"
	"time"

	logEntity "github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Remarks of gift-credit movements, shown to administrators in the gift log.
const (
	RemarkPurchaseDeduction     = "Purchase order deduction"
	RemarkRenewalDeduction      = "Renewal order deduction"
	RemarkResetTrafficDeduction = "Reset traffic order deduction"
	RemarkBalancePayment        = "Purchase payment"
	RemarkCancellationRefund    = "Order cancellation refund"
)

// LogWriter appends audit records; the platform log repository satisfies it.
type LogWriter interface {
	Insert(ctx context.Context, data *logEntity.SystemLog) error
}

// Gift describes a gift-credit movement of one wallet for an order.
type Gift struct {
	UserID  int64
	OrderNo string
	// Amount is the credit moved; Balance is the gift balance afterwards.
	Amount  int64
	Balance int64
	Remark  string
}

// SpendGift records gift credit an order consumed.
func SpendGift(ctx context.Context, logs LogWriter, entry Gift) error {
	return writeGift(ctx, logs, logEntity.GiftTypeReduce, entry)
}

// RefundGift records gift credit returned to the wallet by an order.
func RefundGift(ctx context.Context, logs LogWriter, entry Gift) error {
	return writeGift(ctx, logs, logEntity.GiftTypeIncrease, entry)
}

func writeGift(ctx context.Context, logs LogWriter, kind uint16, entry Gift) error {
	now := timeutil.Now()
	content, err := (&logEntity.Gift{
		Type:      kind,
		OrderNo:   entry.OrderNo,
		Amount:    entry.Amount,
		Balance:   entry.Balance,
		Remark:    entry.Remark,
		Timestamp: now.UnixMilli(),
	}).Marshal()
	if err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "encode gift log for order %s", entry.OrderNo)
	}
	return insert(ctx, logs, logEntity.TypeGift, entry.UserID, now, content)
}

// Balance describes a balance movement of one wallet for an order.
type Balance struct {
	UserID  int64
	OrderNo string
	// Amount is the balance moved; Balance is the balance afterwards.
	Amount  int64
	Balance int64
}

// PayWithBalance records balance spent to pay an order.
func PayWithBalance(ctx context.Context, logs LogWriter, entry Balance) error {
	return writeBalance(ctx, logs, logEntity.BalanceTypePayment, entry)
}

// Recharge records balance credited by a recharge order.
func Recharge(ctx context.Context, logs LogWriter, entry Balance) error {
	return writeBalance(ctx, logs, logEntity.BalanceTypeRecharge, entry)
}

func writeBalance(ctx context.Context, logs LogWriter, kind uint16, entry Balance) error {
	now := timeutil.Now()
	content, err := (&logEntity.Balance{
		Type:      kind,
		Amount:    entry.Amount,
		OrderNo:   entry.OrderNo,
		Balance:   entry.Balance,
		Timestamp: now.UnixMilli(),
	}).Marshal()
	if err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "encode balance log for order %s", entry.OrderNo)
	}
	return insert(ctx, logs, logEntity.TypeBalance, entry.UserID, now, content)
}

func insert(ctx context.Context, logs LogWriter, kind logEntity.Type, userID int64, now time.Time, content []byte) error {
	if err := logs.Insert(ctx, &logEntity.SystemLog{
		Type:     kind.Uint8(),
		Date:     now.Format(time.DateOnly),
		ObjectID: userID,
		Content:  string(content),
	}); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "record wallet movement")
	}
	return nil
}
