package subscribe

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type DeleteSubscribeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Delete subscribe
func NewDeleteSubscribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteSubscribeLogic {
	return &DeleteSubscribeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteSubscribeLogic) DeleteSubscribe(req *dto.DeleteSubscribeRequest) error {
	// Check if the subscribe exists
	phase := "check"
	err := l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		total, err := store.UserSubscription().CountUserSubscribesBySubscribeIdAndStatus(l.ctx, req.Id, 1)
		if err != nil {
			return err
		}
		if total != 0 {
			return errorIsExistActiveUser
		}
		phase = "delete"
		return store.Subscribe().Delete(l.ctx, req.Id)
	})
	if err != nil {
		if errors.Is(err, errorIsExistActiveUser) {
			return errors.Wrapf(xerr.NewErrCode(xerr.SubscribeIsUsedError), "subscribe is used")
		}
		if phase == "delete" {
			l.Logger.Error("[DeleteSubscribeLogic] delete subscribe failed: ", logger.Field("error", err.Error()))
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete subscribe failed: %v", err.Error())
		}
		l.Logger.Error("[DeleteSubscribeLogic] check subscribe failed: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "check subscribe failed: %v", err.Error())
	}
	return nil
}
