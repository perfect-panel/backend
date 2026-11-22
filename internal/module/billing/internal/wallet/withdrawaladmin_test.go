package wallet

import (
	"context"
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	walletEntity "github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	userEntity "github.com/perfect-panel/backend/internal/module/identity/entity/user"
	logEntity "github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/xerr"
)

type withdrawalTestRepo struct{ item *walletEntity.Withdrawal }

func (r *withdrawalTestRepo) InsertWithdrawal(_ context.Context, data *walletEntity.Withdrawal) error {
	r.item = data
	return nil
}
func (r *withdrawalTestRepo) FindWithdrawalForUpdate(context.Context, int64) (*walletEntity.Withdrawal, error) {
	return r.item, nil
}
func (r *withdrawalTestRepo) QueryWithdrawalList(context.Context, int64, *uint8, int, int) ([]*walletEntity.Withdrawal, int64, error) {
	return []*walletEntity.Withdrawal{r.item}, 1, nil
}
func (r *withdrawalTestRepo) UpdateWithdrawalStatus(_ context.Context, _ int64, from, to uint8, reason string) (bool, error) {
	if r.item.Status != from {
		return false, nil
	}
	r.item.Status, r.item.Reason = to, reason
	return true, nil
}

type withdrawalTestWallet struct{ item *walletEntity.Wallet }

func (r *withdrawalTestWallet) FindOneForUpdate(context.Context, int64) (*walletEntity.Wallet, error) {
	return r.item, nil
}
func (r *withdrawalTestWallet) FindWallet(context.Context, int64) (*walletEntity.Wallet, error) {
	return r.item, nil
}
func (r *withdrawalTestWallet) FindWalletsByUserIds(context.Context, []int64) (map[int64]*walletEntity.Wallet, error) {
	return map[int64]*walletEntity.Wallet{r.item.UserId: r.item}, nil
}
func (r *withdrawalTestWallet) UpdateBalanceFields(context.Context, *walletEntity.Wallet) error {
	return nil
}
func (r *withdrawalTestWallet) UpdateCommission(_ context.Context, data *walletEntity.Wallet) error {
	r.item.Commission = data.Commission
	return nil
}

type withdrawalTestLogs struct{ entries []*logEntity.SystemLog }

func (r *withdrawalTestLogs) Insert(_ context.Context, data *logEntity.SystemLog) error {
	r.entries = append(r.entries, data)
	return nil
}
func (r *withdrawalTestLogs) InsertBatch(_ context.Context, data []*logEntity.SystemLog, _ int) error {
	r.entries = append(r.entries, data...)
	return nil
}
func (*withdrawalTestLogs) FindOne(context.Context, int64) (*logEntity.SystemLog, error) {
	return nil, nil
}
func (*withdrawalTestLogs) Update(context.Context, *logEntity.SystemLog) error { return nil }
func (*withdrawalTestLogs) Delete(context.Context, int64) error                { return nil }
func (*withdrawalTestLogs) FilterSystemLog(context.Context, *logEntity.FilterParams) ([]*logEntity.SystemLog, int64, error) {
	return nil, 0, nil
}
func (*withdrawalTestLogs) FindFirstByDateType(context.Context, string, uint8) (*logEntity.SystemLog, error) {
	return nil, nil
}
func (*withdrawalTestLogs) FindByDatesType(context.Context, []string, uint8) ([]*logEntity.SystemLog, error) {
	return nil, nil
}
func (*withdrawalTestLogs) DeleteBefore(context.Context, time.Time) error { return nil }
func (*withdrawalTestLogs) DeleteBeforeBatch(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}
func (*withdrawalTestLogs) SumAmountByTypeAndObjectID(context.Context, uint8, int64) (int64, error) {
	return 0, nil
}

type withdrawalTestStore struct {
	withdrawals repository.UserWithdrawalRepo
	wallet      repository.WalletRepo
	logs        repository.LogRepo
}

func (*withdrawalTestStore) Order() repository.OrderRepo           { return nil }
func (*withdrawalTestStore) OrderEvent() repository.OrderEventRepo { return nil }
func (*withdrawalTestStore) Payment() repository.PaymentRepo       { return nil }
func (*withdrawalTestStore) Coupon() repository.CouponRepo         { return nil }
func (s *withdrawalTestStore) UserWithdrawal() repository.UserWithdrawalRepo {
	return s.withdrawals
}
func (s *withdrawalTestStore) Wallet() repository.WalletRepo { return s.wallet }
func (*withdrawalTestStore) Inbox() repository.InboxRepo     { return nil }
func (*withdrawalTestStore) Outbox() repository.OutboxRepo   { return nil }
func (s *withdrawalTestStore) Log() repository.LogRepo       { return s.logs }

type withdrawalTestTx struct{ store repository.BillingStore }

func (t withdrawalTestTx) InBillingTx(ctx context.Context, fn func(repository.BillingStore) error) error {
	return fn(t.store)
}

// Compile-time assertions keep the fake transaction surface aligned without
// concealing changes to the real domain contracts.
var (
	_ repository.UserWithdrawalRepo = (*withdrawalTestRepo)(nil)
	_ repository.WalletRepo         = (*withdrawalTestWallet)(nil)
	_ repository.LogRepo            = (*withdrawalTestLogs)(nil)
	_ repository.BillingStore       = (*withdrawalTestStore)(nil)
)

func TestRejectWithdrawalRefundsExactlyOnce(t *testing.T) {
	withdrawal := &walletEntity.Withdrawal{Id: 4, UserId: 7, Amount: 30, Status: walletEntity.WithdrawalStatusPending}
	wallet := &withdrawalTestWallet{item: &walletEntity.Wallet{UserId: 7, Commission: 70}}
	logs := &withdrawalTestLogs{}
	store := &withdrawalTestStore{withdrawals: &withdrawalTestRepo{item: withdrawal}, wallet: wallet, logs: logs}
	svc := NewService(Deps{Tx: withdrawalTestTx{store: store}})
	ctx := context.Background()

	err := svc.ReviewWithdrawal(ctx, &dto.ReviewWithdrawalRequest{Id: 4, Status: walletEntity.WithdrawalStatusRejected, Reason: "invalid account"})
	if err != nil {
		t.Fatal(err)
	}
	if wallet.item.Commission != 100 || withdrawal.Status != walletEntity.WithdrawalStatusRejected || len(logs.entries) != 1 {
		t.Fatalf("wallet=%+v withdrawal=%+v logs=%d", wallet.item, withdrawal, len(logs.entries))
	}
	var entry logEntity.Commission
	if err := entry.Unmarshal([]byte(logs.entries[0].Content)); err != nil {
		t.Fatal(err)
	}
	if entry.Type != logEntity.CommissionTypeWithdraw || entry.Amount != 30 {
		t.Fatalf("refund log = %+v", entry)
	}

	if err := svc.ReviewWithdrawal(ctx, &dto.ReviewWithdrawalRequest{Id: 4, Status: walletEntity.WithdrawalStatusRejected, Reason: "retry"}); xerr.CodeOf(err) != xerr.WithdrawalAlreadyReviewed {
		t.Fatalf("second review = %v, want WithdrawalAlreadyReviewed", err)
	}
	if wallet.item.Commission != 100 || len(logs.entries) != 1 {
		t.Fatalf("duplicate review changed funds: commission=%d logs=%d", wallet.item.Commission, len(logs.entries))
	}
}

func TestCommissionWithdrawCreatesPendingRequestWithWithdrawLog(t *testing.T) {
	withdrawals := &withdrawalTestRepo{}
	wallet := &withdrawalTestWallet{item: &walletEntity.Wallet{UserId: 7, Commission: 100}}
	logs := &withdrawalTestLogs{}
	store := &withdrawalTestStore{withdrawals: withdrawals, wallet: wallet, logs: logs}
	ctx := userEntity.NewContext(context.Background(), &userEntity.User{Id: 7})
	svc := NewService(Deps{Tx: withdrawalTestTx{store: store}})

	resp, err := svc.CommissionWithdraw(ctx, &dto.CommissionWithdrawRequest{Amount: 30, Content: "bank"})
	if err != nil {
		t.Fatal(err)
	}
	if wallet.item.Commission != 70 || withdrawals.item == nil || withdrawals.item.Status != walletEntity.WithdrawalStatusPending {
		t.Fatalf("wallet=%+v withdrawal=%+v", wallet.item, withdrawals.item)
	}
	if resp.Amount != 30 || resp.Status != walletEntity.WithdrawalStatusPending || len(logs.entries) != 1 {
		t.Fatalf("response=%+v logs=%d", resp, len(logs.entries))
	}
	var entry logEntity.Commission
	if err := entry.Unmarshal([]byte(logs.entries[0].Content)); err != nil {
		t.Fatal(err)
	}
	if entry.Type != logEntity.CommissionTypeWithdraw || entry.Amount != -30 {
		t.Fatalf("withdraw log = %+v", entry)
	}
}
