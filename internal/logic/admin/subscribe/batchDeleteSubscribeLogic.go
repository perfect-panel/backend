package subscribe

import (
	"context"

	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type BatchDeleteSubscribeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Batch delete subscribe
func NewBatchDeleteSubscribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchDeleteSubscribeLogic {
	return &BatchDeleteSubscribeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

var errorIsExistActiveUser = errors.New("subscription ID belongs to an active user subscription")

func (l *BatchDeleteSubscribeLogic) BatchDeleteSubscribe(req *dto.BatchDeleteSubscribeRequest) error {
	err := l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		for _, id := range req.Ids {
			// Validate whether the subscription ID belongs to an active user subscription.
			count, err := store.UserSubscription().CountUserSubscribesBySubscribeIdAndStatus(l.ctx, id, 1)
			if err != nil {
				l.Logger.Error("[BatchDeleteSubscribe] Query Subscribe Error: ", logger.Field("error", err.Error()))
				return err
			}
			if count > 0 {
				return errorIsExistActiveUser
			}
			if err := store.Subscribe().Delete(l.ctx, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errorIsExistActiveUser) {
			return errors.Wrapf(xerr.NewErrCode(xerr.SubscribeIsUsedError), "subscription ID belongs to an active user subscription")
		}
		l.Logger.Error("[BatchDeleteSubscribe] Transaction Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete subscribe failed: %v", err.Error())
	}
	return nil
}
