package repo

import (
	"context"
	"errors"

	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/cache"

	walletEntity "github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WalletRepo is the billing-owned wallet/withdrawal implementation.
type WalletRepo struct {
	cache.CachedConn
}

var (
	_ repository.WalletRepo         = (*WalletRepo)(nil)
	_ repository.UserWithdrawalRepo = (*WalletRepo)(nil)
)

// NewWalletRepo builds the module-owned implementation over the shared
// cached connection.
func NewWalletRepo(conn cache.CachedConn) *WalletRepo {
	return &WalletRepo{CachedConn: conn}
}

// Billing-domain methods of the user repository family: the wallet table and
// withdrawal records (ADR-001 step 5).

// FindOneForUpdate locks the billing-owned wallet row, creating it on first
// use so every account has a wallet once money moves.
func (m *WalletRepo) FindOneForUpdate(ctx context.Context, userId int64) (*walletEntity.Wallet, error) {
	var result *walletEntity.Wallet
	err := m.QueryNoCacheCtx(ctx, &result, func(conn *gorm.DB, v any) error {
		var w walletEntity.Wallet
		err := conn.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ?", userId).First(&w).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			seed := walletEntity.Wallet{UserId: userId}
			if err := conn.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
				return err
			}
			err = conn.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("user_id = ?", userId).First(&w).Error
		}
		if err != nil {
			return err
		}
		result = &w
		return nil
	})
	return result, err
}

// FindWallet reads the wallet row without locking. A nil result (no error)
// means the account has no wallet row yet; callers treat it as zero values.
func (m *WalletRepo) FindWallet(ctx context.Context, userId int64) (*walletEntity.Wallet, error) {
	var w *walletEntity.Wallet
	err := m.QueryNoCacheCtx(ctx, &w, func(conn *gorm.DB, v any) error {
		var row walletEntity.Wallet
		err := conn.Where("user_id = ?", userId).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		w = &row
		return nil
	})
	if err != nil {
		return nil, err
	}
	return w, nil
}

// FindWalletsByUserIds batch-reads wallet rows for display lists; missing
// rows are absent from the map (treat as zero values).
func (m *WalletRepo) FindWalletsByUserIds(ctx context.Context, userIds []int64) (map[int64]*walletEntity.Wallet, error) {
	result := make(map[int64]*walletEntity.Wallet, len(userIds))
	if len(userIds) == 0 {
		return result, nil
	}
	var rows []*walletEntity.Wallet
	err := m.QueryNoCacheCtx(ctx, &rows, func(conn *gorm.DB, v any) error {
		return conn.Where("user_id IN ?", userIds).Find(v).Error
	})
	if err != nil {
		return nil, err
	}
	for _, w := range rows {
		result[w.UserId] = w
	}
	return result, nil
}

// UpdateBalanceFields persists the balance and gift columns of a wallet row
// previously locked by FindOneForUpdate.
func (m *WalletRepo) UpdateBalanceFields(ctx context.Context, data *walletEntity.Wallet) error {
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&walletEntity.Wallet{}).
			Where("user_id = ?", data.UserId).
			Updates(map[string]any{
				"balance":     data.Balance,
				"gift_amount": data.GiftAmount,
			}).Error
	})
}

// UpdateCommission persists only the commission column: balance movements
// and commission credits may race on different flows.
func (m *WalletRepo) UpdateCommission(ctx context.Context, data *walletEntity.Wallet) error {
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&walletEntity.Wallet{}).
			Where("user_id = ?", data.UserId).
			Update("commission", data.Commission).Error
	})
}

// --- withdrawal ---

func (m *WalletRepo) InsertWithdrawal(ctx context.Context, data *walletEntity.Withdrawal) error {
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Create(data).Error
	})
}

func (m *WalletRepo) FindWithdrawalForUpdate(ctx context.Context, id int64) (*walletEntity.Withdrawal, error) {
	var data walletEntity.Withdrawal
	err := m.QueryNoCacheCtx(ctx, &data, func(conn *gorm.DB, v any) error {
		return conn.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", id).
			First(v).Error
	})
	return &data, err
}

func (m *WalletRepo) QueryWithdrawalList(ctx context.Context, userID int64, status *uint8, page, size int) ([]*walletEntity.Withdrawal, int64, error) {
	var list []*walletEntity.Withdrawal
	var total int64
	page, size = repository.NormalizePage(page, size)
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
		query := conn.Model(&walletEntity.Withdrawal{})
		if userID != 0 {
			query = query.Where("user_id = ?", userID)
		}
		if status != nil {
			query = query.Where("status = ?", *status)
		}
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		return query.Order("id DESC").Limit(size).Offset((page - 1) * size).Find(v).Error
	})
	return list, total, err
}

func (m *WalletRepo) UpdateWithdrawalStatus(ctx context.Context, id int64, from, to uint8, reason string) (bool, error) {
	updated := false
	err := m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		result := conn.Model(&walletEntity.Withdrawal{}).
			Where("id = ? AND status = ?", id, from).
			Updates(map[string]any{"status": to, "reason": reason})
		if result.Error != nil {
			return result.Error
		}
		updated = result.RowsAffected == 1
		return nil
	})
	return updated, err
}
