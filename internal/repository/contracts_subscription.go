package repository

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/client"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
)

// SubscribeRepo manages the plans (the subscribe table) and their groups,
// including each plan's inventory and its cached projections.
type SubscribeRepo interface {
	Insert(ctx context.Context, data *subscribe.Subscribe) error
	FindOne(ctx context.Context, id int64) (*subscribe.Subscribe, error)
	Update(ctx context.Context, data *subscribe.Subscribe) error
	ReserveInventory(ctx context.Context, id int64) (bool, error)
	RestoreInventory(ctx context.Context, id int64) error
	Delete(ctx context.Context, id int64) error
	FilterList(ctx context.Context, params *subscribe.FilterParams) (int64, []*subscribe.Subscribe, error)
	FindByNodeScope(ctx context.Context, nodeIDs []int64, tags []string) ([]*subscribe.Subscribe, error)
	ClearCache(ctx context.Context, id ...int64) error
	QuerySubscribeMinSortByIds(ctx context.Context, ids []int64) (int64, error)
	QueryResetCycleSubscribeIds(ctx context.Context, resetCycle int) ([]int64, error)
	UpdateSort(ctx context.Context, data []*subscribe.Subscribe) error
	QueryGroupList(ctx context.Context) (int64, []*subscribe.Group, error)
	CreateGroup(ctx context.Context, data *subscribe.Group) error
	UpdateGroup(ctx context.Context, data *subscribe.Group) error
	DeleteGroup(ctx context.Context, id int64) error
	BatchDeleteGroup(ctx context.Context, ids []int64) error
}

// ClientRepo manages the subscribe_application rows: the client
// applications subscription delivery renders for (ADR-001 step 5 places the
// table in the subscription module).
type ClientRepo interface {
	Insert(ctx context.Context, data *client.SubscribeApplication) error
	FindOne(ctx context.Context, id int64) (*client.SubscribeApplication, error)
	Update(ctx context.Context, data *client.SubscribeApplication) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context) ([]*client.SubscribeApplication, error)
}
