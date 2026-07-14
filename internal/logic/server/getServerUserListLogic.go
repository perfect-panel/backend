package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/internal/model/entity/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
)

type GetServerUserListLogic struct {
	logger.Logger
	ctx      context.Context
	svcCtx   *svc.ServiceContext
	request  RequestMeta
	response ResponseMeta
}

// NewGetServerUserListLogic Get user list
func NewGetServerUserListLogic(ctx context.Context, svcCtx *svc.ServiceContext, request RequestMeta) *GetServerUserListLogic {
	return &GetServerUserListLogic{
		Logger:   logger.WithContext(ctx),
		ctx:      ctx,
		svcCtx:   svcCtx,
		request:  request,
		response: NewResponseMeta(),
	}
}

func (l *GetServerUserListLogic) ResponseMeta() ResponseMeta {
	return l.response
}

func placeholderServerUser(serverID int64, protocol, secret string) dto.ServerUser {
	name := fmt.Sprintf("ppanel:server-user-placeholder:%d:%s:%s", serverID, strings.TrimSpace(protocol), secret)
	return dto.ServerUser{
		Id:   1,
		UUID: uuidx.NewDeterministicUUID(name).String(),
	}
}

func mergeSubscribeLists(lists ...[]*subscribe.Subscribe) []*subscribe.Subscribe {
	seen := make(map[int64]struct{})
	result := make([]*subscribe.Subscribe, 0)
	for _, list := range lists {
		for _, item := range list {
			if item == nil {
				continue
			}
			if _, ok := seen[item.Id]; ok {
				continue
			}
			seen[item.Id] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}

func (l *GetServerUserListLogic) queryMatchedSubscribes(nodeIds []int64, nodeTags []string) ([]*subscribe.Subscribe, error) {
	var lists [][]*subscribe.Subscribe
	if len(nodeIds) > 0 {
		_, subs, err := l.svcCtx.Store.Subscribe().FilterList(l.ctx, &subscribe.FilterParams{
			Page: 1,
			Size: 9999,
			Node: nodeIds,
		})
		if err != nil {
			return nil, err
		}
		lists = append(lists, subs)
	}

	nodeTags = tool.RemoveDuplicateElements(nodeTags...)
	if len(nodeTags) > 0 {
		_, subs, err := l.svcCtx.Store.Subscribe().FilterList(l.ctx, &subscribe.FilterParams{
			Page: 1,
			Size: 9999,
			Tags: nodeTags,
		})
		if err != nil {
			return nil, err
		}
		lists = append(lists, subs)
	}

	return mergeSubscribeLists(lists...), nil
}

func (l *GetServerUserListLogic) GetServerUserList(req *dto.GetServerUserListRequest) (resp *dto.GetServerUserListResponse, err error) {
	cacheKey := fmt.Sprintf("%s%d:%s", node.ServerUserListCacheKey, req.ServerId, req.Protocol)
	cache, err := l.svcCtx.Redis.Get(l.ctx, cacheKey).Result()
	if cache != "" {
		etag := tool.GenerateETag([]byte(cache))
		resp = &dto.GetServerUserListResponse{}
		//  Check If-None-Match header
		if match := l.request.IfNoneMatch; match == etag {
			return nil, xerr.StatusNotModified
		}
		l.response.SetHeader("ETag", etag)
		err = json.Unmarshal([]byte(cache), resp)
		if err != nil {
			l.Errorw("[ServerUserListCacheKey] json unmarshal error", logger.Field("error", err.Error()))
			return nil, err
		}
		return resp, nil
	}
	server, err := l.svcCtx.Store.Node().FindOneServer(l.ctx, req.ServerId)
	if err != nil {
		return nil, err
	}

	_, nodes, err := l.svcCtx.Store.Node().FilterNodeList(l.ctx, &node.FilterNodeParams{
		Page:     1,
		Size:     1000,
		ServerId: []int64{server.Id},
		Protocol: req.Protocol,
	})
	if err != nil {
		l.Errorw("FilterNodeList error", logger.Field("error", err.Error()))
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

	subs, err := l.queryMatchedSubscribes(nodeIds, nodeTag)
	if err != nil {
		l.Errorw("QuerySubscribeIdsByServerIdAndServerGroupId error", logger.Field("error", err.Error()))
		return nil, err
	}
	if len(subs) == 0 {
		return &dto.GetServerUserListResponse{
			Users: []dto.ServerUser{placeholderServerUser(req.ServerId, req.Protocol, l.svcCtx.Config.Node.NodeSecret)},
		}, nil
	}
	users := make([]dto.ServerUser, 0)
	for _, sub := range subs {
		if err := l.svcCtx.Store.User().ActivatePendingSubscribesBySubscribeId(l.ctx, sub.Id); err != nil {
			return nil, err
		}
		data, err := l.svcCtx.Store.User().FindUsersSubscribeBySubscribeId(l.ctx, sub.Id)
		if err != nil {
			return nil, err
		}
		for _, datum := range data {
			users = append(users, dto.ServerUser{
				Id:          datum.Id,
				UUID:        datum.UUID,
				SpeedLimit:  sub.SpeedLimit,
				DeviceLimit: sub.DeviceLimit,
			})
		}
	}
	if len(users) == 0 {
		users = append(users, placeholderServerUser(req.ServerId, req.Protocol, l.svcCtx.Config.Node.NodeSecret))
	}
	resp = &dto.GetServerUserListResponse{
		Users: users,
	}
	val, _ := json.Marshal(resp)
	etag := tool.GenerateETag(val)
	l.response.SetHeader("ETag", etag)
	err = l.svcCtx.Redis.Set(l.ctx, cacheKey, string(val), node.ServerCacheTTL).Err()
	if err != nil {
		l.Errorw("[ServerUserListCacheKey] redis set error", logger.Field("error", err.Error()))
	}
	//  Check If-None-Match header
	if match := l.request.IfNoneMatch; match == etag {
		return nil, xerr.StatusNotModified
	}
	return resp, nil
}
