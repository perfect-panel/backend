package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/internal/model/entity/subscribe"
	"github.com/perfect-panel/server/pkg/cache"
	"github.com/perfect-panel/server/pkg/orm"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	cacheSubscribeIdPrefix       = "cache:subscribe:id:"
	userSubscribeUserCachePrefix = "cache:user:subscribe:user:v2:"
)

// SubscribeRepo subscribe 数据访问接口
type SubscribeRepo interface {
	Insert(ctx context.Context, data *subscribe.Subscribe, tx ...*gorm.DB) error
	FindOne(ctx context.Context, id int64) (*subscribe.Subscribe, error)
	Update(ctx context.Context, data *subscribe.Subscribe, tx ...*gorm.DB) error
	Delete(ctx context.Context, id int64, tx ...*gorm.DB) error
	FilterList(ctx context.Context, params *subscribe.FilterParams) (int64, []*subscribe.Subscribe, error)
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

var _ SubscribeRepo = (*subscribeRepo)(nil)

type subscribeRepo struct {
	cache.CachedConn
	table string
}

func newSubscribeRepo(db *gorm.DB, c *redis.Client, invalidations ...*cache.InvalidationQueue) SubscribeRepo {
	return &subscribeRepo{
		CachedConn: newCachedConn(db, c, invalidations...),
		table:      "subscribe",
	}
}

func subscribeInSet(field string, values []string) func(db *gorm.DB) *gorm.DB {
	return orm.CommaSeparatedContains(field, values)
}

func (m *subscribeRepo) batchGetCacheKeys(subscribes ...*subscribe.Subscribe) []string {
	var keys []string
	for _, s := range subscribes {
		keys = append(keys, m.getCacheKeys(s)...)
	}
	return keys
}

func (m *subscribeRepo) getCacheKeys(data *subscribe.Subscribe) []string {
	if data == nil {
		return []string{}
	}
	var keys []string
	if data.Nodes != "" {
		var nodes []*node.Node
		ids := strings.Split(data.Nodes, ",")

		err := m.QueryNoCacheCtx(context.Background(), &nodes, func(conn *gorm.DB, v interface{}) error {
			return conn.Model(&node.Node{}).Where("id IN (?)", tool.StringSliceToInt64Slice(ids)).Find(&nodes).Error
		})
		if err == nil {
			for _, n := range nodes {
				keys = append(keys, fmt.Sprintf("%s%d", node.ServerUserListCacheKey, n.ServerId))
				keys = append(keys, fmt.Sprintf("%s%d:%s", node.ServerUserListCacheKey, n.ServerId, n.Protocol))
			}
		}
	}
	if data.NodeTags != "" {
		var nodes []*node.Node
		tags := tool.RemoveDuplicateElements(strings.Split(data.NodeTags, ",")...)
		err := m.QueryNoCacheCtx(context.Background(), &nodes, func(conn *gorm.DB, v interface{}) error {
			return conn.Model(&node.Node{}).Scopes(subscribeInSet("tags", tags)).Find(&nodes).Error
		})
		if err == nil {
			for _, n := range nodes {
				keys = append(keys, fmt.Sprintf("%s%d", node.ServerUserListCacheKey, n.ServerId))
				keys = append(keys, fmt.Sprintf("%s%d:%s", node.ServerUserListCacheKey, n.ServerId, n.Protocol))
			}
		}
	}

	return append(keys, fmt.Sprintf("%s%v", cacheSubscribeIdPrefix, data.Id))
}

func (m *subscribeRepo) getUserSubscribeCacheKeys(ctx context.Context, subscribeId int64) ([]string, error) {
	var userIds []int64
	now := timeutil.Now()
	sevenDaysAgo := now.Add(-7 * 24 * time.Hour)
	err := m.QueryNoCacheCtx(ctx, &userIds, func(conn *gorm.DB, v interface{}) error {
		return conn.Table("user_subscribe").
			Where("subscribe_id = ? AND (expire_time > ? OR finished_at >= ? OR expire_time = ?)", subscribeId, now, sevenDaysAgo, time.UnixMilli(0)).
			Distinct("user_id").
			Pluck("user_id", &userIds).Error
	})
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(userIds))
	for _, userId := range userIds {
		keys = append(keys, fmt.Sprintf("%s%d", userSubscribeUserCachePrefix, userId))
	}
	return keys, nil
}

func (m *subscribeRepo) Insert(ctx context.Context, data *subscribe.Subscribe, tx ...*gorm.DB) error {
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Create(&data).Error
	}, m.getCacheKeys(data)...)
}

func (m *subscribeRepo) FindOne(ctx context.Context, id int64) (*subscribe.Subscribe, error) {
	subscribeIdKey := fmt.Sprintf("%s%v", cacheSubscribeIdPrefix, id)
	var resp subscribe.Subscribe
	err := m.QueryCtx(ctx, &resp, subscribeIdKey, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&subscribe.Subscribe{}).Where("id = ?", id).First(&resp).Error
	})
	switch {
	case err == nil:
		return &resp, nil
	default:
		return nil, err
	}
}

func (m *subscribeRepo) Update(ctx context.Context, data *subscribe.Subscribe, tx ...*gorm.DB) error {
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	cacheKeys := m.getCacheKeys(old)
	userSubscribeCacheKeys, err := m.getUserSubscribeCacheKeys(ctx, data.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	cacheKeys = append(cacheKeys, userSubscribeCacheKeys...)
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		db := conn
		if len(tx) > 0 {
			db = tx[0]
		}
		return db.Save(data).Error
	}, cacheKeys...)
}

func (m *subscribeRepo) Delete(ctx context.Context, id int64, tx ...*gorm.DB) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	cacheKeys := m.getCacheKeys(data)
	userSubscribeCacheKeys, err := m.getUserSubscribeCacheKeys(ctx, id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	cacheKeys = append(cacheKeys, userSubscribeCacheKeys...)
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		db := conn
		if len(tx) > 0 {
			db = tx[0]
		}
		return db.Delete(&subscribe.Subscribe{}, id).Error
	}, cacheKeys...)
}

func (m *subscribeRepo) QuerySubscribeMinSortByIds(ctx context.Context, ids []int64) (int64, error) {
	var minSort int64
	err := m.QueryNoCacheCtx(ctx, &minSort, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&subscribe.Subscribe{}).Where("id IN ?", ids).Select("COALESCE(MIN(sort), 0)").Scan(v).Error
	})
	return minSort, err
}

func (m *subscribeRepo) QueryResetCycleSubscribeIds(ctx context.Context, resetCycle int) ([]int64, error) {
	var ids []int64
	err := m.QueryNoCacheCtx(ctx, &ids, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&subscribe.Subscribe{}).Select("id").Where("reset_cycle = ?", resetCycle).Find(&ids).Error
	})
	return ids, err
}

func (m *subscribeRepo) ClearCache(ctx context.Context, ids ...int64) error {
	if len(ids) <= 0 {
		return nil
	}

	var cacheKeys []string
	for _, id := range ids {
		data, err := m.FindOne(ctx, id)
		if err != nil {
			return err
		}
		cacheKeys = append(cacheKeys, m.getCacheKeys(data)...)
	}
	return m.CachedConn.DelCacheCtx(ctx, cacheKeys...)
}

func (m *subscribeRepo) UpdateSort(ctx context.Context, data []*subscribe.Subscribe) error {
	if len(data) == 0 {
		return nil
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Save(data).Error
	}, m.batchGetCacheKeys(data...)...)
}

func (m *subscribeRepo) QueryGroupList(ctx context.Context) (int64, []*subscribe.Group, error) {
	var list []*subscribe.Group
	var total int64
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&subscribe.Group{}).Count(&total).Find(v).Error
	})
	return total, list, err
}

func (m *subscribeRepo) CreateGroup(ctx context.Context, data *subscribe.Group) error {
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&subscribe.Group{}).Create(data).Error
	})
}

func (m *subscribeRepo) UpdateGroup(ctx context.Context, data *subscribe.Group) error {
	return m.ExecNoCacheCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&subscribe.Group{}).Where("id = ?", data.Id).Save(data).Error
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
			query = query.Scopes(subscribeInSet("nodes", tool.Int64SliceToStringSlice(params.Node)))
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
		return m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v interface{}) error {
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
