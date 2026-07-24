package plan

import (
	"context"

	"github.com/perfect-panel/server/internal/model/entity/subscribe"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/pkg/logger"
)

type SubscribeSortLogic struct {
	logger.Logger
	ctx  context.Context
	deps Deps
}

// NewSubscribeSortLogic Subscribe sort
func newSubscribeSortLogic(ctx context.Context, deps Deps) *SubscribeSortLogic {
	return &SubscribeSortLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *SubscribeSortLogic) SubscribeSort(req *dto.SubscribeSortRequest) error {
	var sort = make(map[int64]int64, len(req.Sort))
	var ids []int64
	for i, v := range req.Sort {
		sort[v.Id] = int64(i)
		ids = append(ids, v.Id)
	}
	// query min sort by ids
	minSort, err := l.deps.Plans.QuerySubscribeMinSortByIds(l.ctx, ids)
	if err != nil {
		l.Logger.Error("[SubscribeSortLogic] query subscribe list by ids error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query subscribe list by ids error: %v", err.Error())
	}
	_, subs, err := l.deps.Plans.FilterList(l.ctx, &subscribe.FilterParams{
		Page: 1,
		Size: 9999,
		Ids:  ids,
	})
	if err != nil {
		l.Logger.Error("[SubscribeSortLogic] query subscribe list by ids error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query subscribe list by ids error: %v", err.Error())
	}
	// reordering
	for _, sub := range subs {
		if newSort, ok := sort[sub.Id]; ok {
			sub.Sort = minSort + newSort
		}
	}
	// update sort
	err = l.deps.Store.InSubscriptionTx(l.ctx, func(store repository.SubscriptionStore) error {
		return store.Subscribe().UpdateSort(l.ctx, subs)
	})
	if err != nil {
		l.Logger.Error("[SubscribeSortLogic] update subscribe sort error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update subscribe sort error: %v", err.Error())
	}
	l.Logger.Info("[UpdateSubscribeSort] Successfully updated subscribe sort")
	return nil
}
