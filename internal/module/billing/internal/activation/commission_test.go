package activation

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	logEntity "github.com/perfect-panel/backend/internal/module/platform/entity/log"
)

type commissionFixture struct {
	h        *billingtest.Harness
	svc      *Service
	buyer    *user.User
	referrer *user.User
}

func newCommissionFixture(t *testing.T, onlyFirst bool) *commissionFixture {
	t.Helper()
	h := billingtest.New(t)
	referrer := h.User()
	buyer := h.User(func(u *user.User) { u.RefererId = referrer.Id })
	return &commissionFixture{
		h: h, buyer: buyer, referrer: referrer,
		svc: NewService(Deps{
			Orders:       h.Store.Order(),
			Store:        h.Store,
			Profiles:     h.Store.User(),
			InvitePolicy: func() (uint8, bool) { return 20, onlyFirst },
		}),
	}
}

func (f *commissionFixture) paidOrder(orderNo string, isNew bool) {
	f.h.Order(&order.Order{
		OrderNo: orderNo, UserId: f.buyer.Id, Type: order.TypeSubscribe, Amount: 10000,
		Status: order.StatusPaid, IsNew: isNew,
	})
}

// The paid commission is kept on the order: a refund reverses it from there
// because inbox markers and logs are purged over time.
func TestSettleOrderCommissionRecordsAmountOnOrder(t *testing.T) {
	f := newCommissionFixture(t, false)
	f.paidOrder("A", false)

	if err := f.svc.SettleOrderCommission(context.Background(), "A", f.buyer.Id); err != nil {
		t.Fatalf("SettleOrderCommission() error = %v", err)
	}
	if got := f.h.ReloadWallet(f.referrer.Id).Commission; got != 2000 {
		t.Fatalf("referrer commission = %d, want 2000", got)
	}
	if got := f.h.ReloadOrder("A").Commission; got != 2000 {
		t.Fatalf("order commission = %d, want 2000", got)
	}
	// A replayed stage credits nothing more.
	if err := f.svc.SettleOrderCommission(context.Background(), "A", f.buyer.Id); err != nil {
		t.Fatal(err)
	}
	if got := f.h.ReloadWallet(f.referrer.Id).Commission; got != 2000 {
		t.Fatalf("replayed commission = %d, want 2000", got)
	}
	if logs := f.h.Logs(logEntity.TypeCommission, f.referrer.Id); len(logs) != 1 {
		t.Fatalf("commission logs = %d, want 1", len(logs))
	}
}

// IsNew is fixed when an order is created, so orders opened before the first
// one was paid all carry it. First-purchase-only commission must still be
// paid once.
func TestSettleOrderCommissionFirstPurchaseOnlyPaysOnce(t *testing.T) {
	f := newCommissionFixture(t, true)
	f.paidOrder("A", true)
	f.paidOrder("B", true)

	for _, orderNo := range []string{"A", "B"} {
		if err := f.svc.SettleOrderCommission(context.Background(), orderNo, f.buyer.Id); err != nil {
			t.Fatalf("SettleOrderCommission(%s) error = %v", orderNo, err)
		}
	}
	if got := f.h.ReloadWallet(f.referrer.Id).Commission; got != 2000 {
		t.Fatalf("referrer commission = %d, want one first-purchase commission of 2000", got)
	}
	if got := f.h.ReloadOrder("B").Commission; got != 0 {
		t.Fatalf("second order commission = %d, want 0", got)
	}
}

// A recharge credits the recharged price exactly once and records the
// movement in the balance ledger.
func TestActivateRechargeCreditsOnceWithLedgerEntry(t *testing.T) {
	f := newCommissionFixture(t, false)
	f.h.Wallet(f.buyer.Id, 500, 0)
	f.h.Order(&order.Order{OrderNo: "R", UserId: f.buyer.Id, Type: order.TypeRecharge, Price: 1990, Amount: 2050, FeeAmount: 60, Status: order.StatusPaid})

	for range 2 {
		balance, err := f.svc.ActivateRecharge(context.Background(), "R")
		if err != nil || balance != 2490 {
			t.Fatalf("ActivateRecharge = (%d, %v), want 2490", balance, err)
		}
	}
	if w := f.h.ReloadWallet(f.buyer.Id); w.Balance != 2490 {
		t.Fatalf("wallet = %+v, want the price credited once", w)
	}
	logs := f.h.Logs(logEntity.TypeBalance, f.buyer.Id)
	if len(logs) != 1 {
		t.Fatalf("balance logs = %d, want 1", len(logs))
	}
	var entry logEntity.Balance
	if err := entry.Unmarshal([]byte(logs[0].Content)); err != nil {
		t.Fatal(err)
	}
	if entry.Type != logEntity.BalanceTypeRecharge || entry.Amount != 1990 || entry.Balance != 2490 || entry.OrderNo != "R" || entry.Timestamp == 0 {
		t.Fatalf("recharge log = %+v", entry)
	}
}

// Finalizing moves the order from Paid to Finished once; a lost race leaves
// it untouched.
func TestFinalizeOrderRequiresPaidOrder(t *testing.T) {
	f := newCommissionFixture(t, false)
	f.paidOrder("F", false)
	if err := f.svc.FinalizeOrder(context.Background(), "F"); err != nil {
		t.Fatal(err)
	}
	if got := f.h.ReloadOrder("F").Status; got != order.StatusFinished {
		t.Fatalf("status = %d, want finished", got)
	}
	if err := f.svc.FinalizeOrder(context.Background(), "F"); !errors.Is(err, ErrInvalidOrderStatus) {
		t.Fatalf("second finalize = %v, want ErrInvalidOrderStatus", err)
	}
}

// Commission is the exact percentage rounded down. The float product this
// replaced under-paid one unit for percentages without an exact binary
// fraction.
func TestCalculateCommissionIsExact(t *testing.T) {
	for price := int64(0); price <= 5000; price++ {
		for percentage := range uint8(101) {
			exact := new(big.Int).Div(new(big.Int).Mul(big.NewInt(price), big.NewInt(int64(percentage))), big.NewInt(100))
			if got := calculateCommission(price, percentage); got != exact.Int64() {
				t.Fatalf("calculateCommission(%d, %d) = %d, want %d", price, percentage, got, exact.Int64())
			}
		}
	}
	if got := calculateCommission(100, 29); got != 29 {
		t.Fatalf("29%% of 100 = %d", got)
	}
}

// A referral percentage above 100, which an older administration API
// accepted, pays the whole price at most; 150% must not pay 1.5 times the
// order as withdrawable commission.
func TestCalculateCommissionClampsThePercentage(t *testing.T) {
	for _, percentage := range []uint8{101, 150, 255} {
		if got := calculateCommission(1000, percentage); got != 1000 {
			t.Fatalf("calculateCommission(1000, %d) = %d, want the whole price", percentage, got)
		}
	}
	f := newCommissionFixture(t, false)
	// The referrer's own percentage applies, with its default of paying the
	// first purchase only.
	if err := f.h.Store.User().UpdateColumns(context.Background(), f.referrer.Id, map[string]any{"referral_percentage": 150}); err != nil {
		t.Fatal(err)
	}
	f.paidOrder("A", true)
	if err := f.svc.SettleOrderCommission(context.Background(), "A", f.buyer.Id); err != nil {
		t.Fatalf("SettleOrderCommission: %v", err)
	}
	if got := f.h.ReloadWallet(f.referrer.Id).Commission; got != 10000 {
		t.Fatalf("referrer commission = %d, want the 10000 paid, not 150%% of it", got)
	}
}

// The referrer's commission is computed on what the buyer paid for the plan,
// without the gateway fee.
func TestSettleOrderCommissionExcludesTheFee(t *testing.T) {
	f := newCommissionFixture(t, false)
	f.h.Order(&order.Order{
		OrderNo: "fee", UserId: f.buyer.Id, Type: order.TypeSubscribe, Amount: 1045, FeeAmount: 45,
		Status: order.StatusPaid,
	})
	if err := f.svc.SettleOrderCommission(context.Background(), "fee", f.buyer.Id); err != nil {
		t.Fatalf("SettleOrderCommission() error = %v", err)
	}
	if got := f.h.ReloadWallet(f.referrer.Id).Commission; got != 200 {
		t.Fatalf("referrer commission = %d, want 20%% of 1000", got)
	}
}
