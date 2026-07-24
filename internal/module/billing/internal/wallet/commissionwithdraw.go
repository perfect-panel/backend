package wallet

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type CommissionWithdrawLogic struct {
	logger.Logger
	ctx    context.Context
	deps Deps
}

// Commission Withdraw
func newCommissionWithdrawLogic(ctx context.Context, deps Deps) *CommissionWithdrawLogic {
	return &CommissionWithdrawLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *CommissionWithdrawLogic) CommissionWithdraw(req *dto.CommissionWithdrawRequest) (resp *dto.WithdrawalLog, err error) {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	if req.Amount <= 0 {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "withdraw amount must be positive")
	}

	var updatedUser *user.User
	err = l.deps.Tx.InBillingTx(l.ctx, func(store repository.BillingStore) error {
		// Do not rely on the user object placed in the request context: it can
		// be stale while another withdrawal or commission credit is committed.
		// The row lock serializes the balance check and debit.
		lockedUser, txErr := store.Wallet().FindOneForUpdate(l.ctx, u.Id)
		if txErr != nil {
			return txErr
		}
		if lockedUser.Commission < req.Amount {
			return errors.Wrapf(xerr.NewErrCode(xerr.UserCommissionNotEnough), "User %d has insufficient commission balance", u.Id)
		}
		lockedUser.Commission -= req.Amount
		if err = store.Wallet().UpdateCommission(l.ctx, lockedUser); err != nil {
			l.Errorf("Failed to update user %d commission balance: %v", u.Id, err)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "Failed to update user %d commission balance: %v", u.Id, err)
		}
		updatedUser = lockedUser

		// Use negative amount to reflect the balance decrease, so that
		// SumAmountByTypeAndObjectID produces the correct net total.
		logInfo := log.Commission{
			Type:      log.CommissionTypeConvertBalance,
			Amount:    -req.Amount,
			Timestamp: timeutil.Now().UnixMilli(),
		}
		b, marshalErr := logInfo.Marshal()
		if marshalErr != nil {
			return marshalErr
		}

		if err = store.Log().Insert(l.ctx, &log.SystemLog{
			Type:      log.TypeCommission.Uint8(),
			Date:      timeutil.Now().Format("2006-01-02"),
			ObjectID:  u.Id,
			Content:   string(b),
			CreatedAt: timeutil.Now(),
		}); err != nil {
			l.Errorf("Failed to create commission log for user %d: %v", u.Id, err)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "Failed to create commission log for user %d: %v", u.Id, err)
		}

		if err = store.UserWithdrawal().InsertWithdrawal(l.ctx, &user.Withdrawal{
			UserId:  u.Id,
			Amount:  req.Amount,
			Content: req.Content,
			Status:  0,
			Reason:  "",
		}); err != nil {
			l.Errorf("Failed to create withdrawal log for user %d: %v", u.Id, err)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "Failed to create withdrawal log for user %d: %v", u.Id, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if updatedUser != nil {
		if cacheErr := l.deps.Cache.ClearUserCache(l.ctx, updatedUser); cacheErr != nil {
			l.Errorf("Failed to clear commission cache for user %d: %v", u.Id, cacheErr)
		}
	}

	return &dto.WithdrawalLog{
		UserId:    u.Id,
		Amount:    req.Amount,
		Content:   req.Content,
		Status:    0,
		Reason:    "",
		CreatedAt: timeutil.Now().UnixMilli(),
	}, nil
}
