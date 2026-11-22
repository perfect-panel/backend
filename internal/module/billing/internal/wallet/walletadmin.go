package wallet

import (
	"context"

	walletEntity "github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// FindWallet reads the user's wallet for display; a user without a wallet
// row reads as nil.
func (s *Service) FindWallet(ctx context.Context, userID int64) (*walletEntity.Wallet, error) {
	return s.deps.Store.Wallet().FindWallet(ctx, userID)
}

// FindWallets reads the wallets of the users for display; a user without a
// wallet row is absent from the map.
func (s *Service) FindWallets(ctx context.Context, userIDs []int64) (map[int64]*walletEntity.Wallet, error) {
	return s.deps.Store.Wallet().FindWalletsByUserIds(ctx, userIDs)
}

// OpenWallet sets the opening balance, gift amount and commission of an
// account an administrator created. It runs in a billing transaction of its
// own after the identity transaction that created the account; a failure
// leaves an uncredited account the administrator can adjust.
func (s *Service) OpenWallet(ctx context.Context, opening walletEntity.Wallet) error {
	return s.deps.Store.InBillingTx(ctx, func(store repository.BillingStore) error {
		w, err := store.Wallet().FindOneForUpdate(ctx, opening.UserId)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "load new user wallet")
		}
		w.Balance = opening.Balance
		w.GiftAmount = opening.GiftAmount
		w.Commission = opening.Commission
		if err := store.Wallet().UpdateBalanceFields(ctx, w); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "credit new user wallet")
		}
		if err := store.Wallet().UpdateCommission(ctx, w); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "credit new user commission")
		}
		return nil
	})
}

// AdjustWallet applies an administrator's edit of the user's wallet: each
// amount the adjustment sets becomes the wallet's, its audit log recording
// the change and the resulting amount (so the amount before it is the
// difference); amounts left nil or equal to the wallet's are left alone,
// which is why a form that carries only the edited amounts cannot revert the
// movements made since it was loaded. It runs in a billing transaction of
// its own after the identity transaction of the edit; a failure leaves the
// money unadjusted for the administrator to retry.
func (s *Service) AdjustWallet(ctx context.Context, adjustment walletEntity.Adjustment) error {
	if adjustment.Balance == nil && adjustment.GiftAmount == nil && adjustment.Commission == nil {
		return nil
	}
	return s.deps.Store.InBillingTx(ctx, func(store repository.BillingStore) error {
		// Financial adjustments must compare and write the latest values
		// under the wallet lock, with their audit logs in the same
		// transaction.
		walletInfo, err := store.Wallet().FindOneForUpdate(ctx, adjustment.UserId)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "find wallet of user %d", adjustment.UserId)
		}
		now := timeutil.Now()
		record := func(kind log.Type, entry interface{ Marshal() ([]byte, error) }) error {
			content, err := entry.Marshal()
			if err != nil {
				return xerr.Wrapf(err, xerr.ERROR, "encode wallet adjustment log of user %d", adjustment.UserId)
			}
			return insertLog(ctx, store, kind, adjustment.UserId, now, content)
		}
		moneyChanged, commissionChanged := false, false
		if target, ok := adjusted(adjustment.Balance, walletInfo.Balance); ok {
			if err := record(log.TypeBalance, &log.Balance{Type: log.BalanceTypeAdjust, Amount: target - walletInfo.Balance, Balance: target, Timestamp: now.UnixMilli()}); err != nil {
				return err
			}
			walletInfo.Balance, moneyChanged = target, true
		}
		if target, ok := adjusted(adjustment.GiftAmount, walletInfo.GiftAmount); ok {
			changeType := log.GiftTypeReduce
			if target > walletInfo.GiftAmount {
				changeType = log.GiftTypeIncrease
			}
			if err := record(log.TypeGift, &log.Gift{Type: changeType, Amount: target - walletInfo.GiftAmount, Balance: target, Remark: "Admin adjustment", Timestamp: now.UnixMilli()}); err != nil {
				return err
			}
			walletInfo.GiftAmount, moneyChanged = target, true
		}
		if target, ok := adjusted(adjustment.Commission, walletInfo.Commission); ok {
			if err := record(log.TypeCommission, &log.Commission{Type: log.CommissionTypeAdjust, Amount: target - walletInfo.Commission, Balance: target, Timestamp: now.UnixMilli()}); err != nil {
				return err
			}
			walletInfo.Commission, commissionChanged = target, true
		}
		if moneyChanged {
			if err := store.Wallet().UpdateBalanceFields(ctx, walletInfo); err != nil {
				return err
			}
		}
		if commissionChanged {
			return store.Wallet().UpdateCommission(ctx, walletInfo)
		}
		return nil
	})
}

// adjusted reports whether the adjustment sets the amount to something other
// than current, and to what.
func adjusted(target *int64, current int64) (int64, bool) {
	if target == nil || *target == current {
		return 0, false
	}
	return *target, true
}
