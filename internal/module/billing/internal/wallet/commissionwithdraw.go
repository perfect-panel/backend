package wallet

import (
	"context"
	"strings"
	"unicode/utf8"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	walletEntity "github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// CommissionWithdraw files the current user's request to withdraw amount of
// their commission: the commission is debited and logged at once, and the
// request waits pending for an administrator's review.
func (s *Service) CommissionWithdraw(ctx context.Context, req *dto.CommissionWithdrawRequest) (*dto.WithdrawalLog, error) {
	u, ok := user.FromContext(ctx)
	if !ok {
		logger.WithContext(ctx).Error("current user is not found in context")
		return nil, xerr.Errorf(xerr.InvalidAccess, "Invalid Access")
	}

	if req.Amount <= 0 {
		return nil, xerr.Errorf(xerr.InvalidParams, "withdraw amount must be positive")
	}
	content := strings.TrimSpace(req.Content)
	if content == "" || utf8.RuneCountInString(content) > 4000 {
		return nil, xerr.Errorf(xerr.InvalidParams, "withdrawal content is required and must not exceed 4000 characters")
	}

	var withdrawal walletEntity.Withdrawal
	err := s.deps.Tx.InBillingTx(ctx, func(store repository.BillingStore) error {
		// Do not rely on the user object placed in the request context: it can
		// be stale while another withdrawal or commission credit is committed.
		// The row lock serializes the balance check and debit.
		lockedUser, err := store.Wallet().FindOneForUpdate(ctx, u.Id)
		if err != nil {
			return err
		}
		if lockedUser.Commission < req.Amount {
			return xerr.Errorf(xerr.UserCommissionNotEnough, "User %d has insufficient commission balance", u.Id)
		}
		lockedUser.Commission -= req.Amount
		if err := store.Wallet().UpdateCommission(ctx, lockedUser); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update commission balance of user %d", u.Id)
		}
		// Use negative amount to reflect the balance decrease, so that
		// SumAmountByTypeAndObjectID produces the correct net total.
		logInfo := log.Commission{
			Type:      log.CommissionTypeWithdraw,
			Amount:    -req.Amount,
			Timestamp: timeutil.Now().UnixMilli(),
		}
		b, err := logInfo.Marshal()
		if err != nil {
			return err
		}

		if err := store.Log().Insert(ctx, &log.SystemLog{
			Type:      log.TypeCommission.Uint8(),
			Date:      timeutil.Now().Format("2006-01-02"),
			ObjectID:  u.Id,
			Content:   string(b),
			CreatedAt: timeutil.Now(),
		}); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseInsertError, "create commission log of user %d", u.Id)
		}

		withdrawal = walletEntity.Withdrawal{
			UserId:  u.Id,
			Amount:  req.Amount,
			Content: content,
			Status:  walletEntity.WithdrawalStatusPending,
			Reason:  "",
		}
		if err := store.UserWithdrawal().InsertWithdrawal(ctx, &withdrawal); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseInsertError, "create withdrawal of user %d", u.Id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	result := withdrawalDTO(&withdrawal)
	return &result, nil
}
