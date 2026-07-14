package server

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type ResetSortWithServerLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewResetSortWithServerLogic Reset server sort
func NewResetSortWithServerLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetSortWithServerLogic {
	return &ResetSortWithServerLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResetSortWithServerLogic) ResetSortWithServer(req *dto.ResetSortRequest) error {
	err := l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		nodeStore := store.Node()
		currentItems, err := nodeStore.QueryServerSorts(l.ctx)
		if err != nil {
			return err
		}
		currentSortMap := make(map[int64]int64)
		for _, item := range currentItems {
			currentSortMap[item.Id] = item.Sort
		}

		var itemsToUpdate []dto.SortItem
		for _, item := range req.Sort {
			if oldSort, exists := currentSortMap[item.Id]; exists && oldSort != item.Sort {
				itemsToUpdate = append(itemsToUpdate, item)
			}
		}
		for _, item := range itemsToUpdate {
			if err := nodeStore.UpdateServerSort(l.ctx, item.Id, item.Sort); err != nil {
				l.Errorw("[NodeSort] Update Database Error: ", logger.Field("error", err.Error()), logger.Field("id", item.Id), logger.Field("sort", item.Sort))
				return err
			}
		}
		return nil
	})
	if err != nil {
		l.Errorw("[NodeSort] Update Database Error: ", logger.Field("error", err.Error()))
		return errors.Wrap(xerr.NewErrCode(xerr.DatabaseUpdateError), err.Error())
	}
	return nil
}
