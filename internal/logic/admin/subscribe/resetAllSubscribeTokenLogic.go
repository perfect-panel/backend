package subscribe

import (
	"context"
	"strconv"

	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type ResetAllSubscribeTokenLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Reset all subscribe tokens
func NewResetAllSubscribeTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetAllSubscribeTokenLogic {
	return &ResetAllSubscribeTokenLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResetAllSubscribeTokenLogic) ResetAllSubscribeToken() (resp *dto.ResetAllSubscribeTokenResponse, err error) {
	err = l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		// select all active and Finished subscriptions
		list, err := store.UserSubscription().FindUserSubscribesByStatus(l.ctx, 1, 2)
		if err != nil {
			logger.Errorf("[ResetAllSubscribeToken] Failed to fetch subscribe list: %v", err.Error())
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Failed to fetch subscribe list: %v", err.Error())
		}
		for _, sub := range list {
			sub.Token = uuidx.SubscribeToken(strconv.FormatInt(timeutil.Now().UnixMilli(), 10) + strconv.FormatInt(sub.Id, 10))
			sub.UUID = uuidx.NewUUID().String()
			if updateErr := store.UserSubscription().UpdateSubscribe(l.ctx, sub); updateErr != nil {
				logger.Errorf("[ResetAllSubscribeToken] Failed to update subscribe token for ID %d: %v", sub.Id, updateErr.Error())
				return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "Failed to update subscribe token for ID %d: %v", sub.Id, updateErr.Error())
			}
		}
		return nil
	})
	if err != nil {
		return &dto.ResetAllSubscribeTokenResponse{
			Success: false,
		}, err
	}

	return &dto.ResetAllSubscribeTokenResponse{
		Success: true,
	}, nil
}
