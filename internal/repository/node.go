package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/pkg/orm"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// NodeRepo node/server 数据访问接口
type NodeRepo interface {
	// server
	InsertServer(ctx context.Context, data *node.Server, tx ...*gorm.DB) error
	FindOneServer(ctx context.Context, id int64) (*node.Server, error)
	UpdateServer(ctx context.Context, data *node.Server, tx ...*gorm.DB) error
	DeleteServer(ctx context.Context, id int64, tx ...*gorm.DB) error
	FindServerConfigOverride(ctx context.Context, serverId int64) (*node.ServerConfigOverride, error)
	SaveServerConfigOverride(ctx context.Context, data *node.ServerConfigOverride, tx ...*gorm.DB) error
	DeleteServerConfigOverride(ctx context.Context, serverId int64, tx ...*gorm.DB) error
	Transaction(ctx context.Context, fn func(db *gorm.DB) error) error
	QueryServerList(ctx context.Context, ids []int64) (servers []*node.Server, err error)
	// node
	InsertNode(ctx context.Context, data *node.Node, tx ...*gorm.DB) error
	FindOneNode(ctx context.Context, id int64) (*node.Node, error)
	UpdateNode(ctx context.Context, data *node.Node, tx ...*gorm.DB) error
	DeleteNode(ctx context.Context, id int64, tx ...*gorm.DB) error
	// cache
	StatusCache(ctx context.Context, serverId int64) (node.Status, error)
	UpdateStatusCache(ctx context.Context, serverId int64, status *node.Status) error
	OnlineUserSubscribe(ctx context.Context, serverId int64, protocol string) (node.OnlineUserSubscribe, error)
	UpdateOnlineUserSubscribe(ctx context.Context, serverId int64, protocol string, subscribe node.OnlineUserSubscribe) error
	OnlineUserSubscribeGlobal(ctx context.Context) (int64, error)
	UpdateOnlineUserSubscribeGlobal(ctx context.Context, subscribe node.OnlineUserSubscribe) error
	// query
	FilterServerList(ctx context.Context, params *node.FilterParams) (int64, []*node.Server, error)
	FilterNodeList(ctx context.Context, params *node.FilterNodeParams) (int64, []*node.Node, error)
	QueryNodeSorts(ctx context.Context) ([]node.SortItem, error)
	QueryServerSorts(ctx context.Context) ([]node.SortItem, error)
	UpdateNodeSort(ctx context.Context, id int64, sort int64) error
	UpdateServerSort(ctx context.Context, id int64, sort int64) error
	QueryNodeTags(ctx context.Context) ([]string, error)
	CountEnabledNodes(ctx context.Context) (int64, error)
	CountServersByReportStatus(ctx context.Context, cutoff time.Time) (int64, int64, error)
	QueryServerAddresses(ctx context.Context) ([]string, error)
	QueryEnabledNodeProtocols(ctx context.Context) ([]string, error)
	ClearNodeCache(ctx context.Context, params *node.FilterNodeParams) error
}

var _ NodeRepo = (*nodeRepo)(nil)

type nodeRepo struct {
	*gorm.DB
	Cache *redis.Client
}

func newNodeRepo(db *gorm.DB, cache *redis.Client) NodeRepo {
	return &nodeRepo{
		DB:    db,
		Cache: cache,
	}
}

// nodeInSet 支持多值 OR 查询
func nodeInSet(field string, values []string) func(db *gorm.DB) *gorm.DB {
	return orm.CommaSeparatedContains(field, values)
}

func (m *nodeRepo) InsertServer(ctx context.Context, data *node.Server, tx ...*gorm.DB) error {
	db := m.DB
	if len(tx) > 0 {
		db = tx[0]
	}
	return db.WithContext(ctx).Create(data).Error
}

func (m *nodeRepo) FindOneServer(ctx context.Context, id int64) (*node.Server, error) {
	var server node.Server
	err := m.WithContext(ctx).Model(&node.Server{}).Where("id = ?", id).First(&server).Error
	return &server, err
}

func (m *nodeRepo) UpdateServer(ctx context.Context, data *node.Server, tx ...*gorm.DB) error {
	_, err := m.FindOneServer(ctx, data.Id)
	if err != nil {
		return err
	}

	db := m.DB
	if len(tx) > 0 {
		db = tx[0]
	}
	return db.WithContext(ctx).Where("id = ?", data.Id).Save(data).Error
}

func (m *nodeRepo) DeleteServer(ctx context.Context, id int64, tx ...*gorm.DB) error {
	db := m.DB
	if len(tx) > 0 {
		db = tx[0]
	}
	return db.WithContext(ctx).Where("id = ?", id).Delete(&node.Server{}).Error
}

func (m *nodeRepo) FindServerConfigOverride(ctx context.Context, serverId int64) (*node.ServerConfigOverride, error) {
	var data []node.ServerConfigOverride

	err := m.WithContext(ctx).Model(&node.ServerConfigOverride{}).Where("server_id = ?", serverId).Limit(1).Find(&data).Error
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, nil
	}

	return &data[0], nil
}

func (m *nodeRepo) SaveServerConfigOverride(ctx context.Context, data *node.ServerConfigOverride, tx ...*gorm.DB) error {
	db := m.DB
	if len(tx) > 0 {
		db = tx[0]
	}

	var old node.ServerConfigOverride
	err := db.WithContext(ctx).Model(&node.ServerConfigOverride{}).Where("server_id = ?", data.ServerId).First(&old).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err == nil {
		data.Id = old.Id
		data.CreatedAt = old.CreatedAt
	}

	return db.WithContext(ctx).Save(data).Error
}

func (m *nodeRepo) DeleteServerConfigOverride(ctx context.Context, serverId int64, tx ...*gorm.DB) error {
	db := m.DB
	if len(tx) > 0 {
		db = tx[0]
	}
	return db.WithContext(ctx).Where("server_id = ?", serverId).Delete(&node.ServerConfigOverride{}).Error
}

func (m *nodeRepo) InsertNode(ctx context.Context, data *node.Node, tx ...*gorm.DB) error {
	db := m.DB
	if len(tx) > 0 {
		db = tx[0]
	}
	return db.WithContext(ctx).Create(data).Error
}

func (m *nodeRepo) FindOneNode(ctx context.Context, id int64) (*node.Node, error) {
	var n node.Node
	err := m.WithContext(ctx).Model(&node.Node{}).Where("id = ?", id).First(&n).Error
	return &n, err
}

func (m *nodeRepo) UpdateNode(ctx context.Context, data *node.Node, tx ...*gorm.DB) error {
	_, err := m.FindOneNode(ctx, data.Id)
	if err != nil {
		return err
	}

	db := m.DB
	if len(tx) > 0 {
		db = tx[0]
	}
	return db.WithContext(ctx).Where("id = ?", data.Id).Save(data).Error
}

func (m *nodeRepo) DeleteNode(ctx context.Context, id int64, tx ...*gorm.DB) error {
	db := m.DB
	if len(tx) > 0 {
		db = tx[0]
	}
	return db.WithContext(ctx).Where("id = ?", id).Delete(&node.Node{}).Error
}

func (m *nodeRepo) Transaction(ctx context.Context, fn func(db *gorm.DB) error) error {
	return m.WithContext(ctx).Transaction(fn)
}

// UpdateStatusCache Update server status to cache
func (m *nodeRepo) UpdateStatusCache(ctx context.Context, serverId int64, status *node.Status) error {
	key := fmt.Sprintf(node.StatusCacheKey, serverId)
	return m.Cache.Set(ctx, key, status.Marshal(), node.Expiry).Err()
}

// DeleteStatusCache Delete server status from cache
func (m *nodeRepo) DeleteStatusCache(ctx context.Context, serverId int64) error {
	key := fmt.Sprintf(node.StatusCacheKey, serverId)
	return m.Cache.Del(ctx, key).Err()
}

// StatusCache Get server status from cache
func (m *nodeRepo) StatusCache(ctx context.Context, serverId int64) (node.Status, error) {
	var status node.Status
	key := fmt.Sprintf(node.StatusCacheKey, serverId)

	result, err := m.Cache.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return status, nil
		}
		return status, err
	}
	if result == "" {
		return status, nil
	}
	err = status.Unmarshal(result)
	return status, err
}

// OnlineUserSubscribe Get online user subscribe
func (m *nodeRepo) OnlineUserSubscribe(ctx context.Context, serverId int64, protocol string) (node.OnlineUserSubscribe, error) {
	key := fmt.Sprintf(node.OnlineUserCacheKeyWithSubscribe, serverId, protocol)
	result, err := m.Cache.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return node.OnlineUserSubscribe{}, nil
		}
		return nil, err
	}
	if result == "" {
		return node.OnlineUserSubscribe{}, nil
	}
	var subscribe node.OnlineUserSubscribe
	err = json.Unmarshal([]byte(result), &subscribe)
	return subscribe, err
}

// UpdateOnlineUserSubscribe Update online user subscribe
func (m *nodeRepo) UpdateOnlineUserSubscribe(ctx context.Context, serverId int64, protocol string, subscribe node.OnlineUserSubscribe) error {
	key := fmt.Sprintf(node.OnlineUserCacheKeyWithSubscribe, serverId, protocol)
	data, err := json.Marshal(subscribe)
	if err != nil {
		return err
	}
	return m.Cache.Set(ctx, key, data, node.Expiry).Err()
}

// DeleteOnlineUserSubscribe Delete online user subscribe
func (m *nodeRepo) DeleteOnlineUserSubscribe(ctx context.Context, serverId int64, protocol string) error {
	key := fmt.Sprintf(node.OnlineUserCacheKeyWithSubscribe, serverId, protocol)
	return m.Cache.Del(ctx, key).Err()
}

// OnlineUserSubscribeGlobal Get global online user subscribe count
func (m *nodeRepo) OnlineUserSubscribeGlobal(ctx context.Context) (int64, error) {
	now := time.Now().Unix()
	// Clear expired data
	if err := m.Cache.ZRemRangeByScore(ctx, node.OnlineUserSubscribeCacheKeyWithGlobal, "-inf", fmt.Sprintf("%d", now)).Err(); err != nil {
		return 0, err
	}
	return m.Cache.ZCard(ctx, node.OnlineUserSubscribeCacheKeyWithGlobal).Result()
}

// UpdateOnlineUserSubscribeGlobal Update global online user subscribe count
func (m *nodeRepo) UpdateOnlineUserSubscribeGlobal(ctx context.Context, subscribe node.OnlineUserSubscribe) error {
	now := time.Now()
	expireTime := now.Add(5 * time.Minute).Unix() // set expire time 5 minutes later

	pipe := m.Cache.Pipeline()

	// Clear expired data
	pipe.ZRemRangeByScore(ctx, node.OnlineUserSubscribeCacheKeyWithGlobal, "-inf", fmt.Sprintf("%d", now.Unix()))
	// Add or update each subscribe with new expire time
	for sub := range subscribe {
		// Use ZAdd to add or update the member with new score (expire time)
		pipe.ZAdd(ctx, node.OnlineUserSubscribeCacheKeyWithGlobal, redis.Z{
			Score:  float64(expireTime),
			Member: sub,
		})
	}

	_, err := pipe.Exec(ctx)
	return err
}

// DeleteOnlineUserSubscribeGlobal Delete global online user subscribe count
func (m *nodeRepo) DeleteOnlineUserSubscribeGlobal(ctx context.Context) error {
	return m.Cache.Del(ctx, node.OnlineUserSubscribeCacheKeyWithGlobal).Err()
}

// FilterServerList Filter Server List
func (m *nodeRepo) FilterServerList(ctx context.Context, params *node.FilterParams) (int64, []*node.Server, error) {
	var servers []*node.Server
	var total int64
	query := m.WithContext(ctx).Model(&node.Server{})
	if params == nil {
		params = &node.FilterParams{
			Page: 1,
			Size: 10,
		}
	}
	params.Page, params.Size = normalizePageFloor(params.Page, params.Size)
	if params.Search != "" {
		query = query.Scopes(orm.PrefixLike([]string{"name", "address"}, params.Search))
	}
	if len(params.Ids) > 0 {
		query = query.Where("id IN ?", params.Ids)
	}
	err := query.Count(&total).Order("sort ASC").Limit(params.Size).Offset((params.Page - 1) * params.Size).Find(&servers).Error
	return total, servers, err
}

func (m *nodeRepo) QueryServerList(ctx context.Context, ids []int64) (servers []*node.Server, err error) {
	query := m.WithContext(ctx).Model(&node.Server{})
	err = query.Where("id IN (?)", ids).Find(&servers).Error
	return
}

func (m *nodeRepo) QueryServerSorts(ctx context.Context) ([]node.SortItem, error) {
	var items []node.SortItem
	err := m.WithContext(ctx).Model(&node.Server{}).Select("id", "sort").Order("sort ASC").Find(&items).Error
	return items, err
}

func (m *nodeRepo) UpdateServerSort(ctx context.Context, id int64, sort int64) error {
	server, err := m.FindOneServer(ctx, id)
	if err != nil {
		return err
	}
	server.Sort = int(sort)
	return m.UpdateServer(ctx, server)
}

// FilterNodeList Filter Node List
func (m *nodeRepo) FilterNodeList(ctx context.Context, params *node.FilterNodeParams) (int64, []*node.Node, error) {
	var nodes []*node.Node
	var total int64
	query := m.WithContext(ctx).Model(&node.Node{})
	if params == nil {
		params = &node.FilterNodeParams{
			Page: 1,
			Size: 10,
		}
	}
	params.Page, params.Size = normalizePageFloor(params.Page, params.Size)
	if params.Search != "" {
		pattern := orm.LikePrefixPattern(params.Search)
		condition := "(name LIKE ?" + orm.LikeEscapeClause() + " OR address LIKE ?" + orm.LikeEscapeClause() + " OR tags LIKE ?" + orm.LikeEscapeClause()
		args := []interface{}{pattern, pattern, pattern}
		if port, err := strconv.ParseUint(params.Search, 10, 16); err == nil {
			condition += " OR port = ?"
			args = append(args, uint16(port))
		}
		condition += ")"
		query = query.Where(condition, args...)
	}
	if len(params.NodeId) > 0 {
		query = query.Where("id IN ?", params.NodeId)
	}
	if len(params.ServerId) > 0 {
		query = query.Where("server_id IN ?", params.ServerId)
	}
	if len(params.Tag) > 0 {
		query = query.Scopes(nodeInSet("tags", params.Tag))
	}
	if params.Protocol != "" {
		query = query.Where("protocol = ?", params.Protocol)
	}

	if params.Enabled != nil {
		query = query.Where("enabled = ?", *params.Enabled)
	}

	if params.Preload {
		query = query.Preload("Server")
	}

	err := query.Count(&total).Order("sort ASC").Limit(params.Size).Offset((params.Page - 1) * params.Size).Find(&nodes).Error
	return total, nodes, err
}

func (m *nodeRepo) QueryNodeSorts(ctx context.Context) ([]node.SortItem, error) {
	var items []node.SortItem
	err := m.WithContext(ctx).Model(&node.Node{}).Select("id", "sort").Order("sort ASC").Find(&items).Error
	return items, err
}

func (m *nodeRepo) UpdateNodeSort(ctx context.Context, id int64, sort int64) error {
	n, err := m.FindOneNode(ctx, id)
	if err != nil {
		return err
	}
	n.Sort = int(sort)
	return m.UpdateNode(ctx, n)
}

func (m *nodeRepo) QueryNodeTags(ctx context.Context) ([]string, error) {
	var tags []string
	err := m.WithContext(ctx).Model(&node.Node{}).Pluck("tags", &tags).Error
	return tags, err
}

func (m *nodeRepo) CountEnabledNodes(ctx context.Context) (int64, error) {
	var total int64
	err := m.WithContext(ctx).Model(&node.Node{}).Where("enabled = ?", true).Count(&total).Error
	return total, err
}

func (m *nodeRepo) CountServersByReportStatus(ctx context.Context, cutoff time.Time) (int64, int64, error) {
	var online int64
	if err := m.WithContext(ctx).Model(&node.Server{}).Where("last_reported_at > ?", cutoff).Count(&online).Error; err != nil {
		return 0, 0, err
	}

	var offline int64
	if err := m.WithContext(ctx).Model(&node.Server{}).Where("last_reported_at <= ? OR last_reported_at IS NULL", cutoff).Count(&offline).Error; err != nil {
		return 0, 0, err
	}

	return online, offline, nil
}

func (m *nodeRepo) QueryServerAddresses(ctx context.Context) ([]string, error) {
	var addresses []string
	err := m.WithContext(ctx).Model(&node.Server{}).Pluck("address", &addresses).Error
	return addresses, err
}

func (m *nodeRepo) QueryEnabledNodeProtocols(ctx context.Context) ([]string, error) {
	var protocols []string
	err := m.WithContext(ctx).Model(&node.Node{}).Where("enabled = ?", true).Pluck("protocol", &protocols).Error
	return protocols, err
}

// ClearNodeCache Clear Node Cache
func (m *nodeRepo) ClearNodeCache(ctx context.Context, params *node.FilterNodeParams) error {
	_, nodes, err := m.FilterNodeList(ctx, params)
	if err != nil {
		return err
	}
	var cacheKeys []string
	for _, n := range nodes {
		// Scan all protocol variants of user list and config cache
		patterns := []string{
			fmt.Sprintf("%s%d:*", node.ServerUserListCacheKey, n.ServerId),
			fmt.Sprintf("%s%d:*", node.ServerConfigCacheKey, n.ServerId),
		}
		// Also delete legacy user-list key written before protocol was added to the key.
		cacheKeys = append(cacheKeys, fmt.Sprintf("%s%d", node.ServerUserListCacheKey, n.ServerId))
		for _, pattern := range patterns {
			var cursor uint64
			for {
				keys, newCursor, err := m.Cache.Scan(ctx, cursor, pattern, 100).Result()
				if err != nil {
					return err
				}
				if len(keys) > 0 {
					cacheKeys = append(cacheKeys, keys...)
				}
				cursor = newCursor
				if cursor == 0 {
					break
				}
			}
		}
	}

	if len(cacheKeys) > 0 {
		cacheKeys = tool.RemoveDuplicateElements(cacheKeys...)
		return m.Cache.Del(ctx, cacheKeys...).Err()
	}
	return nil
}

// ClearServerCache Clear Server Cache
func (m *nodeRepo) ClearServerCache(ctx context.Context, serverId int64) error {
	var cacheKeys []string
	// Scan all protocol variants of both user list and config cache
	patterns := []string{
		fmt.Sprintf("%s%d:*", node.ServerUserListCacheKey, serverId),
		fmt.Sprintf("%s%d:*", node.ServerConfigCacheKey, serverId),
	}
	// Also delete legacy user-list key written before protocol was added to the key.
	cacheKeys = append(cacheKeys, fmt.Sprintf("%s%d", node.ServerUserListCacheKey, serverId))
	for _, pattern := range patterns {
		var cursor uint64
		for {
			keys, newCursor, err := m.Cache.Scan(ctx, cursor, pattern, 100).Result()
			if err != nil {
				return err
			}
			if len(keys) > 0 {
				cacheKeys = append(cacheKeys, keys...)
			}
			cursor = newCursor
			if cursor == 0 {
				break
			}
		}
	}

	if len(cacheKeys) > 0 {
		cacheKeys = tool.RemoveDuplicateElements(cacheKeys...)
		return m.Cache.Del(ctx, cacheKeys...).Err()
	}
	return nil
}
