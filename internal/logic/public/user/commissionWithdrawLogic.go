package user

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type CommissionWithdrawLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Commission Withdraw
func NewCommissionWithdrawLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CommissionWithdrawLogic {
	return &CommissionWithdrawLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CommissionWithdrawLogic) CommissionWithdraw(req *dto.CommissionWithdrawRequest) (resp *dto.WithdrawalLog, err error) {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	if u.Commission < req.Amount {
		logger.Errorf("User %d has insufficient commission balance: %.2f, requested: %.2f", u.Id, float64(u.Commission)/100, float64(req.Amount)/100)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserCommissionNotEnough), "User %d has insufficient commission balance", u.Id)
	}

	// create withdrawal log
	// Use negative amount to reflect the balance decrease, so that
	// SumAmountByTypeAndObjectID produces the correct net total.
	logInfo := log.Commission{
		Type:      log.CommissionTypeConvertBalance,
		Amount:    -req.Amount,
		Timestamp: timeutil.Now().UnixMilli(),
	}
	b, err := logInfo.Marshal()

	if err != nil {
		l.Errorf("Failed to marshal commission log for user %d: %v", u.Id, err)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Failed to marshal commission log for user %d: %v", u.Id, err)
	}

	err = l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		updatedUser := *u
		updatedUser.Commission -= req.Amount
		if err = store.User().Update(l.ctx, &updatedUser); err != nil {
			l.Errorf("Failed to update user %d commission balance: %v", u.Id, err)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "Failed to update user %d commission balance: %v", u.Id, err)
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

		if err = store.User().InsertWithdrawal(l.ctx, &user.Withdrawal{
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

	return &dto.WithdrawalLog{
		UserId:    u.Id,
		Amount:    req.Amount,
		Content:   req.Content,
		Status:    0,
		Reason:    "",
		CreatedAt: timeutil.Now().UnixMilli(),
	}, nil
}
