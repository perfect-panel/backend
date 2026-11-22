package ledger

import (
	"context"
	"errors"
	"testing"
	"time"

	logEntity "github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/xerr"
)

type recordingLogs struct {
	entries []*logEntity.SystemLog
	err     error
}

func (r *recordingLogs) Insert(_ context.Context, data *logEntity.SystemLog) error {
	if r.err != nil {
		return r.err
	}
	r.entries = append(r.entries, data)
	return nil
}

// Every gift movement carries the timestamp the administrator's gift log
// displays; the balance checkout used to omit it.
func TestGiftMovementsShareOneShape(t *testing.T) {
	logs := &recordingLogs{}
	ctx := context.Background()
	before := time.Now().Add(-time.Second).UnixMilli()
	if err := SpendGift(ctx, logs, Gift{UserID: 7, OrderNo: "o-1", Amount: 300, Balance: 100, Remark: RemarkBalancePayment}); err != nil {
		t.Fatal(err)
	}
	if err := RefundGift(ctx, logs, Gift{UserID: 7, OrderNo: "o-1", Amount: 300, Balance: 400, Remark: RemarkCancellationRefund}); err != nil {
		t.Fatal(err)
	}
	if len(logs.entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(logs.entries))
	}
	for i, want := range []struct {
		kind    uint16
		balance int64
		remark  string
	}{{logEntity.GiftTypeReduce, 100, RemarkBalancePayment}, {logEntity.GiftTypeIncrease, 400, RemarkCancellationRefund}} {
		entry := logs.entries[i]
		if entry.Type != logEntity.TypeGift.Uint8() || entry.ObjectID != 7 || entry.Date == "" {
			t.Fatalf("entry %d = %+v", i, entry)
		}
		var gift logEntity.Gift
		if err := gift.Unmarshal([]byte(entry.Content)); err != nil {
			t.Fatal(err)
		}
		if gift.Type != want.kind || gift.OrderNo != "o-1" || gift.Amount != 300 || gift.Balance != want.balance || gift.Remark != want.remark || gift.Timestamp < before {
			t.Fatalf("gift %d = %+v", i, gift)
		}
	}
}

func TestBalanceMovements(t *testing.T) {
	logs := &recordingLogs{}
	ctx := context.Background()
	if err := PayWithBalance(ctx, logs, Balance{UserID: 3, OrderNo: "o-2", Amount: 500, Balance: 0}); err != nil {
		t.Fatal(err)
	}
	if err := Recharge(ctx, logs, Balance{UserID: 3, OrderNo: "o-3", Amount: 900, Balance: 900}); err != nil {
		t.Fatal(err)
	}
	for i, kind := range []uint16{logEntity.BalanceTypePayment, logEntity.BalanceTypeRecharge} {
		var balance logEntity.Balance
		if err := balance.Unmarshal([]byte(logs.entries[i].Content)); err != nil {
			t.Fatal(err)
		}
		if logs.entries[i].Type != logEntity.TypeBalance.Uint8() || balance.Type != kind || balance.Timestamp == 0 {
			t.Fatalf("balance %d = %+v / %+v", i, logs.entries[i], balance)
		}
	}
}

func TestLedgerWriteFailureIsADatabaseError(t *testing.T) {
	cause := errors.New("disk full")
	err := SpendGift(context.Background(), &recordingLogs{err: cause}, Gift{OrderNo: "o-1"})
	if !errors.Is(err, cause) || xerr.CodeOf(err) != xerr.DatabaseInsertError {
		t.Fatalf("error = %v, want the cause under DatabaseInsertError", err)
	}
}
