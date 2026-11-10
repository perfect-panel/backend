package serverapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	dto "github.com/perfect-panel/server/internal/module/network/contract"
	"github.com/perfect-panel/server/internal/module/network/entity/node"
	"github.com/perfect-panel/server/pkg/httpx"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/slicesx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// placeholderServerUserID is the id of the stand-in user an empty list
// carries. It is also a valid subscription id.
const placeholderServerUserID int64 = 1

// placeholderServerUser is the stand-in user of an empty list, with a fresh
// UUIDv7. It is generated only while rebuilding the list: cache hits reuse
// the serialized UUID and ETag until the list expires or changes.
func placeholderServerUser() dto.ServerUser {
	return dto.ServerUser{
		Id:   placeholderServerUserID,
		UUID: uuid.NewV7().String(),
	}
}

func serverUserListCacheKey(serverID int64, protocol string) string {
	return fmt.Sprintf("%s%d:%s", node.ServerUserListCacheKey, serverID, protocol)
}

// GetServerUserList returns the users a server serves over a protocol, from
// the server's response cache when it holds one, rebuilding and caching the
// list otherwise. meta carries the node's If-None-Match: a matching ETag
// returns xerr.ErrNotModified. The returned response metadata holds the
// headers set before any error.
func (s *Service) GetServerUserList(ctx context.Context, req *dto.GetServerUserListRequest, meta RequestMeta) (*dto.GetServerUserListResponse, ResponseMeta, error) {
	log := logger.WithContext(ctx)
	response := NewResponseMeta()
	cacheKey := serverUserListCacheKey(req.ServerId, req.Protocol)
	cache, err := s.deps.Redis.Get(ctx, cacheKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		// An unreadable cache must not cost the node its user list: fall
		// through to the database like a miss.
		log.Errorw("[GetServerUserList] read cached user list failed",
			logger.Field("server_id", req.ServerId),
			logger.Field("protocol", req.Protocol),
			logger.Field("error", err.Error()))
		cache = ""
	}
	if cache != "" {
		etag := httpx.GenerateETag([]byte(cache))
		if meta.IfNoneMatch == etag {
			return nil, response, xerr.ErrNotModified
		}
		response.SetHeader("ETag", etag)
		resp := &dto.GetServerUserListResponse{}
		if err := json.Unmarshal([]byte(cache), resp); err != nil {
			log.Errorw("[ServerUserListCacheKey] json unmarshal error", logger.Field("error", err.Error()))
			return nil, response, err
		}
		return resp, response, nil
	}
	list, err := s.rebuildUserList(ctx, req.ServerId, req.Protocol)
	if err != nil {
		return nil, response, err
	}
	etag := httpx.GenerateETag(list.payload)
	response.SetHeader("ETag", etag)
	if meta.IfNoneMatch == etag {
		return nil, response, xerr.ErrNotModified
	}
	return list.resp, response, nil
}

// userList is a rebuilt node user list with its cached serialization.
type userList struct {
	resp       *dto.GetServerUserListResponse
	payload    []byte
	generation int64
	expiresAt  time.Time
}

// rebuildUserList resolves the server protocol's user list from the database
// and caches it for the next reader. All endpoints share one fill per scope.
func (s *Service) rebuildUserList(ctx context.Context, serverID int64, protocol string) (*userList, error) {
	result := s.userListLoads.DoChan(serverUserListCacheKey(serverID, protocol), func() (any, error) {
		// One disconnected node must not cancel a fill needed by other nodes.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), userListLoadTimeout)
		defer cancel()
		select {
		case s.userListBuilds <- struct{}{}:
			defer func() { <-s.userListBuilds }()
		default:
			return nil, ErrServerBusy
		}
		generation, err := s.deps.Caches.ServerCacheGeneration(ctx, serverID)
		if err != nil {
			return nil, err
		}
		// Another caller may have filled Redis since the original miss.
		cached, err := s.readCachedUserList(ctx, serverID, protocol, generation)
		if err == nil && cached != nil {
			return cached, nil
		}
		return s.buildUserList(ctx, serverID, protocol)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-result:
		if result.Err != nil {
			return nil, result.Err
		}
		return result.Val.(*userList), nil
	}
}

func (s *Service) buildUserList(ctx context.Context, serverID int64, protocol string) (*userList, error) {
	generation, err := s.deps.Caches.ServerCacheGeneration(ctx, serverID)
	if err != nil {
		return nil, err
	}
	server, err := s.deps.Servers.FindOneServer(ctx, serverID)
	if err != nil {
		return nil, err
	}
	users, err := s.servedUsers(ctx, server, protocol)
	if err != nil {
		return nil, err
	}
	list := &userList{generation: generation, expiresAt: time.Now().Add(node.ServerCacheTTL)}
	if len(users) == 0 {
		users = []dto.ServerUser{placeholderServerUser()}
	}
	list.resp = &dto.GetServerUserListResponse{Users: users}
	list.payload, _ = json.Marshal(list.resp)
	if err := s.deps.Caches.SetServerCache(ctx, serverID, serverUserListCacheKey(serverID, protocol), string(list.payload), generation); err != nil {
		logger.WithContext(ctx).Errorw("[ServerUserListCacheKey] cache set error", logger.Field("error", err.Error()))
	}
	return list, nil
}

// servedUsers resolves the users the server serves over the protocol: the
// servable subscriptions of the plans scoped to its nodes, owned by enabled
// accounts.
func (s *Service) servedUsers(ctx context.Context, server *node.Server, protocol string) ([]dto.ServerUser, error) {
	nodes, err := s.deps.Nodes.ListNodes(ctx, &node.FilterNodeParams{
		ServerId: []int64{server.Id},
		Protocol: protocol,
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetServerUserList] list the server's nodes failed", logger.Field("error", err.Error()))
		return nil, err
	}
	var nodeTag []string
	var nodeIds []int64
	for _, n := range nodes {
		nodeIds = append(nodeIds, n.Id)
		if n.Tags != "" {
			nodeTag = append(nodeTag, strings.Split(n.Tags, ",")...)
		}
	}

	// A read endpoint: it selects the servable subscriptions of the plans
	// scoped to the nodes and writes nothing (legacy Pending rows are
	// servable as they are).
	served, err := s.deps.Subscriptions.ServableSubscriptionsByNodeScope(ctx, nodeIds, nodeTag)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetServerUserList] read the servable subscriptions failed", logger.Field("error", err.Error()))
		return nil, err
	}
	if len(served) == 0 {
		return nil, nil
	}
	userIDs := make([]int64, 0, len(served))
	for _, item := range served {
		userIDs = append(userIDs, item.Subscription.UserId)
	}
	enabledIDs, err := s.deps.Accounts.FindEnabledUserIDs(ctx, slicesx.RemoveDuplicateElements(userIDs...))
	if err != nil {
		return nil, err
	}
	enabled := make(map[int64]struct{}, len(enabledIDs))
	for _, id := range enabledIDs {
		enabled[id] = struct{}{}
	}
	users := make([]dto.ServerUser, 0, len(served))
	for _, item := range served {
		if _, ok := enabled[item.Subscription.UserId]; !ok {
			continue
		}
		users = append(users, dto.ServerUser{
			Id: item.Subscription.Id, UUID: item.Subscription.UUID,
			SpeedLimit: item.Plan.SpeedLimit, DeviceLimit: item.Plan.DeviceLimit,
		})
	}
	return users, nil
}

// servedSubscriptionIDs returns an immutable, shared set. Callers must not
// mutate it: reports and device admission use the same generation of users.
func (s *Service) servedSubscriptionIDs(ctx context.Context, serverID int64, protocol string) (map[int64]struct{}, error) {
	index, err := s.servedUserIndex(ctx, serverID, protocol)
	if err != nil {
		return nil, err
	}
	return index.ids, nil
}

// readCachedUserList is used only during a shared fill. The local index must
// expire no later than the source list, including lists filled by older servers.
func (s *Service) readCachedUserList(ctx context.Context, serverID int64, protocol string, generation int64) (*userList, error) {
	key := serverUserListCacheKey(serverID, protocol)
	started := time.Now()
	pipe := s.deps.Redis.TxPipeline()
	data := pipe.Get(ctx, key)
	ttl := pipe.PTTL(ctx, key)
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	if data.Val() == "" {
		return nil, nil
	}
	lifetime := ttl.Val()
	if lifetime < 0 || lifetime > node.ServerCacheTTL {
		lifetime = node.ServerCacheTTL
	}
	list := &userList{
		resp: &dto.GetServerUserListResponse{}, payload: []byte(data.Val()),
		generation: generation, expiresAt: started.Add(lifetime),
	}
	if err := json.Unmarshal(list.payload, list.resp); err != nil {
		return nil, err
	}
	return list, nil
}

// withoutPlaceholder drops the stand-in user of a cached empty list. Its id
// is a valid subscription id, so a lone entry with that id is kept only when
// its UUID is that subscription's UUID.
func (s *Service) withoutPlaceholder(ctx context.Context, users []dto.ServerUser) ([]dto.ServerUser, error) {
	if len(users) != 1 || users[0].Id != placeholderServerUserID {
		return users, nil
	}
	sub, err := s.deps.Subscriptions.SubscriptionByID(ctx, placeholderServerUserID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if sub.UUID != users[0].UUID {
		return nil, nil
	}
	return users, nil
}
