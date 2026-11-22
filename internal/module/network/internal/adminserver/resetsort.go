package adminserver

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// ResetSortWithServer stores the server order an administrator set.
func (s *Service) ResetSortWithServer(ctx context.Context, req *dto.ResetSortRequest) error {
	return resetSort(ctx, storeSorts{store: s.deps.Store}, serverSorts, req)
}

// ResetSortWithNode stores the node order an administrator set.
func (s *Service) ResetSortWithNode(ctx context.Context, req *dto.ResetSortRequest) error {
	return resetSort(ctx, storeSorts{store: s.deps.Store}, nodeSorts, req)
}

// sortRepo reads and stores the positions of the sortable lists.
type sortRepo interface {
	QueryServerSorts(ctx context.Context) ([]node.SortItem, error)
	UpdateServerSort(ctx context.Context, id int64, sort int64) error
	QueryNodeSorts(ctx context.Context) ([]node.SortItem, error)
	UpdateNodeSort(ctx context.Context, id int64, sort int64) error
}

// sortTransactor runs fn over the sortable lists in one network transaction.
type sortTransactor interface {
	InSortTx(ctx context.Context, fn func(sortRepo) error) error
}

// storeSorts runs the sort transactions as network transactions of the
// store.
type storeSorts struct {
	store Store
}

func (s storeSorts) InSortTx(ctx context.Context, fn func(sortRepo) error) error {
	return s.store.InNetworkTx(ctx, func(tx repository.NetworkStore) error {
		return fn(tx.Node())
	})
}

// sortList is one of the admin lists whose order an administrator sets: the
// servers or the nodes.
type sortList struct {
	name   string
	query  func(sortRepo, context.Context) ([]node.SortItem, error)
	update func(sortRepo, context.Context, int64, int64) error
}

var (
	serverSorts = sortList{name: "server", query: sortRepo.QueryServerSorts, update: sortRepo.UpdateServerSort}
	nodeSorts   = sortList{name: "node", query: sortRepo.QueryNodeSorts, update: sortRepo.UpdateNodeSort}
)

// resetSort stores, in one network transaction, the positions of req that
// differ from the stored ones; ids the list does not hold are ignored.
func resetSort(ctx context.Context, sorts sortTransactor, list sortList, req *dto.ResetSortRequest) error {
	err := sorts.InSortTx(ctx, func(repo sortRepo) error {
		current, err := list.query(repo, ctx)
		if err != nil {
			return err
		}
		stored := make(map[int64]int64, len(current))
		for _, item := range current {
			stored[item.Id] = item.Sort
		}
		for _, item := range req.Sort {
			if sort, ok := stored[item.Id]; !ok || sort == item.Sort {
				continue
			}
			if err := list.update(repo, ctx, item.Id, item.Sort); err != nil {
				logger.WithContext(ctx).Errorw("[ResetSort] update the "+list.name+" sort failed",
					logger.Field("error", err.Error()), logger.Field("id", item.Id), logger.Field("sort", item.Sort))
				return err
			}
		}
		return nil
	})
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "reset the %s sort", list.name)
	}
	return nil
}
