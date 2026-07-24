package storefront

import (
	"context"
	"strings"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type QueryUserSubscribeNodeListLogic struct {
	logger.Logger
	ctx  context.Context
	deps Deps
}

// Get user subscribe node info
func newQueryUserSubscribeNodeListLogic(ctx context.Context, deps Deps) *QueryUserSubscribeNodeListLogic {
	return &QueryUserSubscribeNodeListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *QueryUserSubscribeNodeListLogic) QueryUserSubscribeNodeList() (resp *dto.QueryUserSubscribeNodeListResponse, err error) {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	userSubscribes, err := l.deps.UserSubs.QueryUserSubscribe(l.ctx, u.Id, 1, 2)
	if err != nil {
		logger.Errorw("failed to query user subscribe", logger.Field("error", err.Error()), logger.Field("user_id", u.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "DB_ERROR")
	}

	resp = &dto.QueryUserSubscribeNodeListResponse{}
	for _, us := range userSubscribes {
		userSubscribe, err := l.getUserSubscribe(us.Token)
		if err != nil {
			l.Errorw("[SubscribeLogic] Get user subscribe failed", logger.Field("error", err.Error()), logger.Field("token", userSubscribe.Token))
			return nil, err
		}
		nodes, err := l.getServers(userSubscribe)
		if err != nil {
			return nil, err
		}
		userSubscribeInfo := dto.UserSubscribeInfo{
			Id:          userSubscribe.Id,
			Nodes:       nodes,
			Traffic:     userSubscribe.Traffic,
			Upload:      userSubscribe.Upload,
			Download:    userSubscribe.Download,
			Token:       userSubscribe.Token,
			UserId:      userSubscribe.UserId,
			OrderId:     userSubscribe.OrderId,
			SubscribeId: userSubscribe.SubscribeId,
			StartTime:   userSubscribe.StartTime.Unix(),
			ExpireTime:  userSubscribe.ExpireTime.Unix(),
			Status:      userSubscribe.Status,
			CreatedAt:   userSubscribe.CreatedAt.Unix(),
			UpdatedAt:   userSubscribe.UpdatedAt.Unix(),
		}

		if userSubscribe.FinishedAt != nil {
			userSubscribeInfo.FinishedAt = userSubscribe.FinishedAt.Unix()
		}

		if l.deps.isTrialPlan(userSubscribe.SubscribeId) {
			userSubscribeInfo.IsTryOut = true
		}

		resp.List = append(resp.List, userSubscribeInfo)
	}

	return
}

func (l *QueryUserSubscribeNodeListLogic) getServers(userSub *user.Subscribe) (userSubscribeNodes []*dto.UserSubscribeNodeInfo, err error) {
	userSubscribeNodes = make([]*dto.UserSubscribeNodeInfo, 0)
	if l.isSubscriptionExpired(userSub) || l.isTrafficExhausted(userSub) {
		return l.createExpiredServers(), nil
	}

	subDetails, err := l.deps.Plans.FindOne(l.ctx, userSub.SubscribeId)
	if err != nil {
		l.Errorw("[Generate Subscribe]find subscribe details error: %v", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe details error: %v", err.Error())
	}
	nodeIds := tool.StringToInt64Slice(subDetails.Nodes)
	tags := strings.Split(subDetails.NodeTags, ",")
	cleanTags := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			cleanTags = append(cleanTags, tag)
		}
	}
	tags = cleanTags

	l.Debugf("[Generate Subscribe]nodes: %v, NodeTags: %v", nodeIds, tags)

	enable := true

	nodes, err := l.filterSubscribeNodes(nodeIds, tags, enable)
	if err != nil {
		l.Errorw("[Generate Subscribe]find server details error: %v", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find server details error: %v", err.Error())
	}

	if len(nodes) > 0 {
		var serverMapIds = make(map[int64]*node.Server)
		for _, n := range nodes {
			serverMapIds[n.ServerId] = nil
		}
		var serverIds []int64
		for k := range serverMapIds {
			serverIds = append(serverIds, k)
		}

		servers, err := l.deps.Nodes.QueryServerList(l.ctx, serverIds)
		if err != nil {
			l.Errorw("[Generate Subscribe]find server details error: %v", logger.Field("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find server details error: %v", err.Error())
		}

		for _, s := range servers {
			serverMapIds[s.Id] = s
		}

		for _, n := range nodes {
			server := serverMapIds[n.ServerId]
			if server == nil {
				continue
			}
			userSubscribeNode := &dto.UserSubscribeNodeInfo{
				Id:        n.Id,
				Name:      n.Name,
				Uuid:      userSub.UUID,
				Protocol:  n.Protocol,
				Port:      n.Port,
				Address:   n.Address,
				Tags:      strings.Split(n.Tags, ","),
				Country:   server.Country,
				City:      server.City,
				CreatedAt: n.CreatedAt.Unix(),
			}
			userSubscribeNodes = append(userSubscribeNodes, userSubscribeNode)
		}
	}

	l.Debugf("[Query Subscribe]found servers: %v", len(nodes))
	logger.Debugf("[Generate Subscribe]found servers: %v", len(nodes))
	return userSubscribeNodes, nil
}

func (l *QueryUserSubscribeNodeListLogic) filterSubscribeNodes(nodeIds []int64, tags []string, enable bool) ([]*node.Node, error) {
	addNodes := func(result []*node.Node, seen map[int64]struct{}, items []*node.Node) []*node.Node {
		for _, item := range items {
			if item == nil {
				continue
			}
			if _, ok := seen[item.Id]; ok {
				continue
			}
			seen[item.Id] = struct{}{}
			result = append(result, item)
		}
		return result
	}

	if len(nodeIds) == 0 && len(tags) == 0 {
		_, nodes, err := l.deps.Nodes.FilterNodeList(l.ctx, &node.FilterNodeParams{
			Page:    0,
			Size:    1000,
			Enabled: &enable,
		})
		return nodes, err
	}

	seen := make(map[int64]struct{})
	nodes := make([]*node.Node, 0)
	if len(nodeIds) > 0 {
		_, directNodes, err := l.deps.Nodes.FilterNodeList(l.ctx, &node.FilterNodeParams{
			Page:    0,
			Size:    1000,
			NodeId:  nodeIds,
			Enabled: &enable,
		})
		if err != nil {
			return nil, err
		}
		nodes = addNodes(nodes, seen, directNodes)
	}
	if len(tags) > 0 {
		_, tagNodes, err := l.deps.Nodes.FilterNodeList(l.ctx, &node.FilterNodeParams{
			Page:    0,
			Size:    1000,
			Tag:     tags,
			Enabled: &enable,
		})
		if err != nil {
			return nil, err
		}
		nodes = addNodes(nodes, seen, tagNodes)
	}
	return nodes, nil
}

func (l *QueryUserSubscribeNodeListLogic) isSubscriptionExpired(userSub *user.Subscribe) bool {
	return userSub.ExpireTime.Unix() < timeutil.Now().Unix() && userSub.ExpireTime.Unix() != 0
}

// isTrafficExhausted reports whether the subscription has used up its traffic
// quota (Traffic == 0 means unlimited).
func (l *QueryUserSubscribeNodeListLogic) isTrafficExhausted(userSub *user.Subscribe) bool {
	return userSub.Traffic > 0 && userSub.Download+userSub.Upload >= userSub.Traffic
}

func (l *QueryUserSubscribeNodeListLogic) createExpiredServers() []*dto.UserSubscribeNodeInfo {
	return nil
}

func (l *QueryUserSubscribeNodeListLogic) getFirstHostLine() string {
	host := l.deps.Host
	lines := strings.Split(host, "\n")
	if len(lines) > 0 {
		return lines[0]
	}
	return host
}
func (l *QueryUserSubscribeNodeListLogic) getUserSubscribe(token string) (*user.Subscribe, error) {
	userSub, err := l.deps.UserSubs.FindOneSubscribeByToken(l.ctx, token)
	if err != nil {
		l.Infow("[Generate Subscribe]find subscribe error: %v", logger.Field("error", err.Error()), logger.Field("token", token))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe error: %v", err.Error())
	}

	//  Ignore expiration check
	//if userSub.Status > 1 {
	//	l.Infow("[Generate Subscribe]subscribe is not available", logger.Field("status", int(userSub.Status)), logger.Field("token", token))
	//	return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeNotAvailable), "subscribe is not available")
	//}

	return userSub, nil
}
