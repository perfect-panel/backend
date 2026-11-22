package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/cache"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/orm"
	"github.com/perfect-panel/backend/pkg/slicesx"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	cacheSubscribeIdPrefix = "cache:subscribe:id:"
)

var _ repository.SubscribeRepo = (*subscribeRepo)(nil)

type subscribeRepo struct {
	cache.CachedConn
	table string
	// nodes is the network bundle's fenced invalidation of the node-facing
	// server caches; this domain must neither query the node table nor
	// delete the `server:user:*` keys itself, which bypassed the fence.
	nodes repository.NodeCacheKeyBridge
}

// NewSubscribeRepo builds the module-owned implementation over the shared
// cached connection.
func NewSubscribeRepo(conn cache.CachedConn, nodes repository.NodeCacheKeyBridge) repository.SubscribeRepo {
	return &subscribeRepo{
		CachedConn: conn,
		table:      "subscribe",
		nodes:      nodes,
	}
}

func subscribeInSet(field string, values []string) func(db *gorm.DB) *gorm.DB {
	return orm.CommaSeparatedContains(field, values)
}

// cacheKeys returns the plans' own cache entries. The node-facing user
// lists of the servers carrying their nodes are not keys to delete along
// with them: clearNodeCaches invalidates those after the write through the
// network bundle, whose cache generation fence rejects a list a concurrent
// rebuild read before the write; a plain DEL let such a list be cached over
// the invalidation for its whole TTL.
func (m *subscribeRepo) cacheKeys(plans ...*subscribe.Subscribe) []string {
	keys := make([]string, 0, len(plans))
	for _, plan := range plans {
		if plan != nil {
			keys = append(keys, planCacheKey(plan.Id))
		}
	}
	return keys
}

// clearNodeCaches drops, through the network bundle, the node-facing caches
// of every server carrying a node the plans select. It runs once the plans'
// write is committed. A damaged node list narrows the scope, as it narrowed
// the key list before, rather than failing the call.
func (m *subscribeRepo) clearNodeCaches(ctx context.Context, plans ...*subscribe.Subscribe) error {
	if m.nodes == nil {
		return nil
	}
	var nodeIDs []int64
	var tags []string
	for _, plan := range plans {
		if plan == nil {
			continue
		}
		if plan.Nodes != "" {
			ids, err := slicesx.ParseInt64CSV(plan.Nodes)
			if err != nil {
				logger.WithContext(ctx).Errorw("[SubscribeRepo] plan node list is damaged; its node caches are not cleared",
					logger.Field("subscribe_id", plan.Id), logger.Field("error", err.Error()))
			}
			nodeIDs = append(nodeIDs, ids...)
		}
		if plan.NodeTags != "" {
			tags = append(tags, strings.Split(plan.NodeTags, ",")...)
		}
	}
	if len(nodeIDs) == 0 && len(tags) == 0 {
		return nil
	}
	return m.nodes.ClearNodeUserListCaches(ctx, slicesx.RemoveDuplicateElements(nodeIDs...), slicesx.RemoveDuplicateElements(tags...))
}

// clearNodeCachesAfterWrite runs clearNodeCaches for a committed plan write.
// A failure is only logged: the write stays committed, and the node lists
// expire on their own.
func (m *subscribeRepo) clearNodeCachesAfterWrite(ctx context.Context, operation string, plans ...*subscribe.Subscribe) {
	if err := m.clearNodeCaches(ctx, plans...); err != nil {
		logger.WithContext(ctx).Errorw("[SubscribeRepo] clear the node caches of the plans failed",
			logger.Field("operation", operation), logger.Field("error", err.Error()))
	}
}

func planCacheKey(id int64) string {
	return fmt.Sprintf("%s%v", cacheSubscribeIdPrefix, id)
}

func (m *subscribeRepo) getUserSubscribeCacheKeys(ctx context.Context, subscribeId int64) ([]string, error) {
	var userIds []int64
	err := m.QueryNoCacheCtx(ctx, &userIds, func(conn *gorm.DB, v any) error {
		return conn.Model(&usersub.Subscribe{}).
			Where("subscribe_id = ?", subscribeId).
			Distinct("user_id").
			Pluck("user_id", &userIds).Error
	})
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(userIds))
	for _, userId := range userIds {
		keys = append(keys, fmt.Sprintf("%s%d", cacheUserSubscribeUserPrefix, userId))
	}
	return keys, nil
}

func (m *subscribeRepo) Insert(ctx context.Context, data *subscribe.Subscribe) error {
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Create(&data).Error
	}, m.cacheKeys(data)...)
}

func (m *subscribeRepo) FindOne(ctx context.Context, id int64) (*subscribe.Subscribe, error) {
	subscribeIdKey := planCacheKey(id)
	var resp subscribe.Subscribe
	err := m.QueryCtx(ctx, &resp, subscribeIdKey, func(conn *gorm.DB, v any) error {
		return conn.Model(&subscribe.Subscribe{}).Where("id = ?", id).First(&resp).Error
	})
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// planColumns are the plan settings an administrator's edit writes: every
// column but the id and the timestamps. Naming them keeps the edit off a
// whole-row save, which would also insert a plan that no longer exists and
// would write the timestamps as the edit's copy has them.
var planColumns = []string{
	"name", "language", "description", "unit_price", "unit_time", "discount", "replacement",
	"inventory", "traffic", "speed_limit", "device_limit", "quota", "nodes", "node_tags",
	"show", "sell", "sort", "deduction_ratio", "allow_deduction", "reset_cycle", "renewal_reset",
	"show_original_price",
}

// groupColumns are the group settings an administrator's edit writes.
var groupColumns = []string{"name", "description"}

// errPlanID rejects a write without a plan id: GORM would turn it into an
// insert or an update of every row.
var errPlanID = errors.New("plan id is required")

// Update writes the plan's settings (planColumns, plus the update timestamp)
// as data has them, an omitted switch included as NULL where the column
// allows it, so the caller resolves the switches it keeps beforehand.
func (m *subscribeRepo) Update(ctx context.Context, data *subscribe.Subscribe) error {
	if data == nil || data.Id == 0 {
		return errPlanID
	}
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	cacheKeys := m.cacheKeys(old)
	userSubscribeCacheKeys, err := m.getUserSubscribeCacheKeys(ctx, data.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	cacheKeys = append(cacheKeys, userSubscribeCacheKeys...)
	if err := m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&subscribe.Subscribe{}).Where("id = ?", data.Id).Select(planColumns).Updates(data).Error
	}, cacheKeys...); err != nil {
		return err
	}
	// The servers of the previous and of the new node scope re-read their
	// user lists.
	m.clearNodeCachesAfterWrite(ctx, "update plan", old, data)
	return nil
}

// ReserveInventory consumes one finite inventory unit with a conditional update.
// A stale plan object must never decide whether stock is still available.
func (m *subscribeRepo) ReserveInventory(ctx context.Context, id int64) (bool, error) {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		return false, err
	}
	if data.Inventory == -1 {
		return true, nil
	}
	if data.Inventory <= 0 {
		return false, nil
	}
	var reserved bool
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		result := conn.Model(&subscribe.Subscribe{}).
			Where("id = ? AND inventory > 0", id).
			UpdateColumn("inventory", gorm.Expr("inventory - 1"))
		reserved = result.RowsAffected == 1
		return result.Error
	}, m.cacheKeys(data)...)
	return reserved, err
}

// RestoreInventory returns one previously reserved finite inventory unit. An
// unlimited plan (inventory = -1) remains unlimited.
func (m *subscribeRepo) RestoreInventory(ctx context.Context, id int64) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		return err
	}
	if data.Inventory == -1 {
		return nil
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&subscribe.Subscribe{}).
			Where("id = ? AND inventory >= 0", id).
			UpdateColumn("inventory", gorm.Expr("inventory + 1")).Error
	}, m.cacheKeys(data)...)
}

func (m *subscribeRepo) Delete(ctx context.Context, id int64) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	cacheKeys := m.cacheKeys(data)
	userSubscribeCacheKeys, err := m.getUserSubscribeCacheKeys(ctx, id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	cacheKeys = append(cacheKeys, userSubscribeCacheKeys...)
	if err := m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Delete(&subscribe.Subscribe{}, id).Error
	}, cacheKeys...); err != nil {
		return err
	}
	m.clearNodeCachesAfterWrite(ctx, "delete plan", data)
	return nil
}

func (m *subscribeRepo) QuerySubscribeMinSortByIds(ctx context.Context, ids []int64) (int64, error) {
	var minSort int64
	err := m.QueryNoCacheCtx(ctx, &minSort, func(conn *gorm.DB, v any) error {
		return conn.Model(&subscribe.Subscribe{}).Where("id IN ?", ids).Select("COALESCE(MIN(sort), 0)").Scan(v).Error
	})
	return minSort, err
}

func (m *subscribeRepo) QueryResetCycleSubscribeIds(ctx context.Context, resetCycle int) ([]int64, error) {
	var ids []int64
	err := m.QueryNoCacheCtx(ctx, &ids, func(conn *gorm.DB, v any) error {
		return conn.Model(&subscribe.Subscribe{}).Select("id").Where("reset_cycle = ?", resetCycle).Find(&ids).Error
	})
	return ids, err
}

// ClearCache invalidates the plans' cache entries, reading every plan in one
// query, and then the node-facing user lists of the servers carrying their
// nodes, through the network bundle's fenced invalidation. Callers run it
// once the change to what the servers serve has committed. A plan that no
// longer exists still loses its own entry.
func (m *subscribeRepo) ClearCache(ctx context.Context, ids ...int64) error {
	ids = slicesx.RemoveDuplicateElements(ids...)
	if len(ids) == 0 {
		return nil
	}
	var plans []*subscribe.Subscribe
	err := m.QueryNoCacheCtx(ctx, &plans, func(conn *gorm.DB, v any) error {
		return conn.Model(&subscribe.Subscribe{}).Where("id IN ?", ids).Find(v).Error
	})
	if err != nil {
		return err
	}
	keys := m.cacheKeys(plans...)
	for _, id := range ids {
		keys = append(keys, planCacheKey(id))
	}
	if err := m.DelCacheCtx(ctx, keys...); err != nil {
		return err
	}
	return m.clearNodeCaches(ctx, plans...)
}

// UpdateSort writes each plan's sort position and nothing else. The plans
// were read before the positions were assigned; saving them whole (as an
// upsert of every column) carried their stale copies back into the table, an
// inventory a purchase had decremented meanwhile included, and re-inserted a
// plan deleted in between.
func (m *subscribeRepo) UpdateSort(ctx context.Context, data []*subscribe.Subscribe) error {
	if len(data) == 0 {
		return nil
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		now := timeutil.Now()
		for _, plan := range data {
			if plan == nil || plan.Id == 0 {
				return errPlanID
			}
			if err := conn.Model(&subscribe.Subscribe{}).Where("id = ?", plan.Id).
				UpdateColumns(map[string]any{"sort": plan.Sort, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		return nil
	}, m.cacheKeys(data...)...)
}

func (m *subscribeRepo) QueryGroupList(ctx context.Context) (int64, []*subscribe.Group, error) {
	var list []*subscribe.Group
	var total int64
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
		return conn.Model(&subscribe.Group{}).Count(&total).Find(v).Error
	})
	return total, list, err
}

func (m *subscribeRepo) CreateGroup(ctx context.Context, data *subscribe.Group) error {
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&subscribe.Group{}).Create(data).Error
	})
}

// UpdateGroup writes the group's settings (groupColumns, plus the update
// timestamp); a group that no longer exists is not re-inserted.
func (m *subscribeRepo) UpdateGroup(ctx context.Context, data *subscribe.Group) error {
	if data == nil || data.Id == 0 {
		return errPlanID
	}
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&subscribe.Group{}).Where("id = ?", data.Id).Select(groupColumns).Updates(data).Error
	})
}

func (m *subscribeRepo) DeleteGroup(ctx context.Context, id int64) error {
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&subscribe.Group{}).Where("id = ?", id).Delete(&subscribe.Group{}).Error
	})
}

func (m *subscribeRepo) BatchDeleteGroup(ctx context.Context, ids []int64) error {
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&subscribe.Group{}).Where("id IN ?", ids).Delete(&subscribe.Group{}).Error
	})
}

// FilterList Filter Subscribe List
func (m *subscribeRepo) FilterList(ctx context.Context, params *subscribe.FilterParams) (int64, []*subscribe.Subscribe, error) {
	if params == nil {
		params = &subscribe.FilterParams{}
	}
	params.Normalize()

	var list []*subscribe.Subscribe
	var total int64

	buildQuery := func(conn *gorm.DB, lang string) *gorm.DB {
		query := conn.Model(&subscribe.Subscribe{})

		if params.Search != "" {
			query = query.Scopes(orm.ContainsLike([]string{"name", "description"}, params.Search))
		}
		if params.Show {
			query = query.Where(clause.Eq{
				Column: clause.Column{Name: "show"},
				Value:  true,
			})
		}
		if params.Sell {
			query = query.Where("sell = true")
		}

		if len(params.Ids) > 0 {
			query = query.Where("id IN ?", params.Ids)
		}
		if len(params.Node) > 0 {
			query = query.Scopes(subscribeInSet("nodes", slicesx.Int64SliceToStringSlice(params.Node)))
		}

		if len(params.Tags) > 0 {
			query = query.Scopes(subscribeInSet("node_tags", params.Tags))
		}
		if lang != "" {
			query = query.Where("language = ?", lang)
		} else if params.DefaultLanguage {
			query = query.Where("language = ''")
		}

		return query
	}

	queryFunc := func(lang string) error {
		return m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
			query := buildQuery(conn, lang)
			if err := query.Count(&total).Error; err != nil {
				return err
			}
			return query.Order("sort ASC").
				Limit(params.Size).
				Offset((params.Page - 1) * params.Size).
				Find(v).Error
		})
	}

	err := queryFunc(params.Language)
	if err != nil {
		return 0, nil, err
	}

	if params.DefaultLanguage && total == 0 {
		err = queryFunc("")
		if err != nil {
			return 0, nil, err
		}
	}

	return total, list, nil
}

// FindByNodeScope resolves every plan visible to a node without applying the
// public/admin pagination cap or issuing a separate COUNT query.
func (m *subscribeRepo) FindByNodeScope(ctx context.Context, nodeIDs []int64, tags []string) ([]*subscribe.Subscribe, error) {
	conditions := make([]string, 0, 2)
	args := make([]any, 0, len(nodeIDs)+len(tags))
	list := make([]*subscribe.Subscribe, 0)
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
		if condition, values := orm.CommaSeparatedContainsCondition(conn, "nodes", slicesx.Int64SliceToStringSlice(nodeIDs)); condition != "" {
			conditions = append(conditions, condition)
			args = append(args, values...)
		}
		if condition, values := orm.CommaSeparatedContainsCondition(conn, "node_tags", slicesx.RemoveDuplicateElements(tags...)); condition != "" {
			conditions = append(conditions, condition)
			args = append(args, values...)
		}
		if len(conditions) == 0 {
			return nil
		}
		return conn.Model(&subscribe.Subscribe{}).
			Where("("+strings.Join(conditions, " OR ")+")", args...).
			Order("sort ASC").Find(v).Error
	})
	return list, err
}
