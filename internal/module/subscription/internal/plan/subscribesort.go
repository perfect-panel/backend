package plan

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// SubscribeSort reorders the listed plans in the request's order, from the
// lowest position among them.
func (s *Service) SubscribeSort(ctx context.Context, req *dto.SubscribeSortRequest) error {
	log := logger.WithContext(ctx)
	var sort = make(map[int64]int64, len(req.Sort))
	var ids []int64
	for i, v := range req.Sort {
		sort[v.Id] = int64(i)
		ids = append(ids, v.Id)
	}
	minSort, err := s.deps.Plans.QuerySubscribeMinSortByIds(ctx, ids)
	if err != nil {
		log.Error("[SubscribeSortLogic] query subscribe list by ids error: ", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "query subscribe list by ids error: %v", err.Error())
	}
	_, subs, err := s.deps.Plans.FilterList(ctx, &subscribe.FilterParams{
		Page: 1,
		Size: 9999,
		Ids:  ids,
	})
	if err != nil {
		log.Error("[SubscribeSortLogic] query subscribe list by ids error: ", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "query subscribe list by ids error: %v", err.Error())
	}
	for _, sub := range subs {
		if newSort, ok := sort[sub.Id]; ok {
			sub.Sort = minSort + newSort
		}
	}
	err = s.deps.Store.InSubscriptionTx(ctx, func(store repository.SubscriptionStore) error {
		return store.Subscribe().UpdateSort(ctx, subs)
	})
	if err != nil {
		log.Error("[SubscribeSortLogic] update subscribe sort error: ", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update subscribe sort error: %v", err.Error())
	}
	log.Info("[UpdateSubscribeSort] Successfully updated subscribe sort")
	return nil
}
