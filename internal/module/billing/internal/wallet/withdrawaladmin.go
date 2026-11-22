package wallet

import (
	"context"
	"strings"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	walletEntity "github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetWithdrawalList pages the withdrawal requests for the administrator,
// optionally only one user's or those in one status.
func (s *Service) GetWithdrawalList(ctx context.Context, req *dto.GetWithdrawalListRequest) (*dto.GetWithdrawalListResponse, error) {
	data, total, err := s.deps.Withdrawals.QueryWithdrawalList(ctx, req.UserId, req.Status, req.Page, req.Size)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "query withdrawals failed")
	}
	list := make([]dto.WithdrawalLog, 0, len(data))
	for _, item := range data {
		list = append(list, withdrawalDTO(item))
	}
	return &dto.GetWithdrawalListResponse{List: list, Total: total}, nil
}

// ReviewWithdrawal records the administrator's decision on a pending
// withdrawal. A rejection needs a reason and returns the withdrawn amount to
// the user's commission with its log entry; a request that is no longer
// pending is refused, so the refund happens at most once.
func (s *Service) ReviewWithdrawal(ctx context.Context, req *dto.ReviewWithdrawalRequest) error {
	reason := strings.TrimSpace(req.Reason)
	if req.Status == walletEntity.WithdrawalStatusRejected && reason == "" {
		return xerr.Errorf(xerr.InvalidParams, "rejection reason is required")
	}
	if req.Status != walletEntity.WithdrawalStatusApproved && req.Status != walletEntity.WithdrawalStatusRejected {
		return xerr.Errorf(xerr.InvalidParams, "withdrawal status must be approved or rejected")
	}
	if req.Status == walletEntity.WithdrawalStatusApproved {
		reason = ""
	}

	return s.deps.Tx.InBillingTx(ctx, func(store repository.BillingStore) error {
		withdrawal, err := store.UserWithdrawal().FindWithdrawalForUpdate(ctx, req.Id)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "find withdrawal failed")
		}
		if withdrawal.Status != walletEntity.WithdrawalStatusPending {
			return xerr.Errorf(xerr.WithdrawalAlreadyReviewed, "withdrawal is no longer pending")
		}

		if req.Status == walletEntity.WithdrawalStatusRejected {
			account, err := store.Wallet().FindOneForUpdate(ctx, withdrawal.UserId)
			if err != nil {
				return err
			}
			// The commission may be negative here: a refund claws back
			// commission the user already withdrew. Only a sum that does not
			// fit an int64 is refused.
			refunded, ok := addInt64(account.Commission, withdrawal.Amount)
			if !ok {
				return xerr.Errorf(xerr.DatabaseUpdateError, "withdrawal refund would overflow commission balance")
			}
			account.Commission = refunded
			if err := store.Wallet().UpdateCommission(ctx, account); err != nil {
				return xerr.Wrapf(err, xerr.DatabaseUpdateError, "refund withdrawal commission failed")
			}
			entry := log.Commission{Type: log.CommissionTypeWithdraw, Amount: withdrawal.Amount, Balance: account.Commission, Timestamp: timeutil.Now().UnixMilli()}
			content, err := entry.Marshal()
			if err != nil {
				return err
			}
			if err := store.Log().Insert(ctx, &log.SystemLog{
				Type: log.TypeCommission.Uint8(), Date: timeutil.Now().Format("2006-01-02"),
				ObjectID: withdrawal.UserId, Content: string(content), CreatedAt: timeutil.Now(),
			}); err != nil {
				return xerr.Wrapf(err, xerr.DatabaseInsertError, "record withdrawal refund failed")
			}
		}

		updated, err := store.UserWithdrawal().UpdateWithdrawalStatus(
			ctx, withdrawal.Id, walletEntity.WithdrawalStatusPending, req.Status, reason,
		)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update withdrawal failed")
		}
		if !updated {
			return xerr.Errorf(xerr.WithdrawalAlreadyReviewed, "withdrawal changed concurrently")
		}
		return nil
	})
}

// addInt64 is a + b, reporting false when the sum does not fit an int64,
// whatever the signs of the operands.
func addInt64(a, b int64) (int64, bool) {
	sum := a + b
	if (b > 0 && sum < a) || (b < 0 && sum > a) {
		return 0, false
	}
	return sum, true
}

func withdrawalDTO(item *walletEntity.Withdrawal) dto.WithdrawalLog {
	if item == nil {
		return dto.WithdrawalLog{}
	}
	return dto.WithdrawalLog{
		Id: item.Id, UserId: item.UserId, Amount: item.Amount, Content: item.Content,
		Status: item.Status, Reason: item.Reason,
		CreatedAt: item.CreatedAt.UnixMilli(), UpdatedAt: item.UpdatedAt.UnixMilli(),
	}
}
