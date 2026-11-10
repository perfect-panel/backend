package storefront

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/module/identity/entity/user"
	"github.com/perfect-panel/server/internal/module/network/entity/node"
	dto "github.com/perfect-panel/server/internal/module/subscription/contract"
	"github.com/perfect-panel/server/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/server/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/xerr"
	"gorm.io/gorm"
)

// QueryUserSubscribeNodeList lists the owner's subscriptions in their term
// with the nodes each may use. A subscription that may not use the service
// now lists no nodes, by the same rule the node user list and delivery
// apply; the plan's nodes are loaded once per plan.
func (s *Service) QueryUserSubscribeNodeList(ctx context.Context) (*dto.QueryUserSubscribeNodeListResponse, error) {
	log := logger.WithContext(ctx)
	u, ok := user.FromContext(ctx)
	if !ok {
		log.Error("current user is not found in context")
		return nil, xerr.NewErrCode(xerr.InvalidAccess)
	}
	userSubscribes, err := s.deps.UserSubs.QueryUserSubscribe(ctx, u.Id, usersub.InTermStatuses.Values()...)
	if err != nil {
		log.Errorw("[QueryUserSubscribeNodeList] Query subscriptions failed", logger.Field("error", err.Error()), logger.Field("user_id", u.Id))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "query subscriptions of user %d", u.Id)
	}

	resp := &dto.QueryUserSubscribeNodeListResponse{}
	nodesByPlan := make(map[int64][]*node.Node)
	now := timeutil.Now()
	for _, details := range userSubscribes {
		if details == nil {
			continue
		}
		userSubscribe := subscribeFromDetails(details)
		nodes, err := s.subscriptionNodes(ctx, userSubscribe, details.Subscribe, nodesByPlan, now)
		if err != nil {
			return nil, err
		}
		info := dto.UserSubscribeInfo{
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
			IsTryOut:    s.deps.isTrialPlan(userSubscribe.SubscribeId),
		}
		if userSubscribe.FinishedAt != nil {
			info.FinishedAt = userSubscribe.FinishedAt.Unix()
		}
		resp.List = append(resp.List, info)
	}
	return resp, nil
}

func subscribeFromDetails(item *usersub.SubscribeDetails) *usersub.Subscribe {
	return &usersub.Subscribe{
		Id: item.Id, UserId: item.UserId, OrderId: item.OrderId, SubscribeId: item.SubscribeId,
		StartTime: item.StartTime, ExpireTime: item.ExpireTime, FinishedAt: item.FinishedAt,
		Traffic: item.Traffic, Download: item.Download, Upload: item.Upload,
		Token: item.Token, UUID: item.UUID, Status: item.Status, Note: item.Note,
		EntitlementSource: item.EntitlementSource, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

// subscriptionNodes returns the nodes the subscription may use now: none
// unless it is servable, otherwise the enabled nodes of its plan. A
// subscription whose plan was deleted has no nodes left to list.
func (s *Service) subscriptionNodes(ctx context.Context, userSub *usersub.Subscribe, plan *subscribe.Subscribe, nodesByPlan map[int64][]*node.Node, now time.Time) ([]*dto.UserSubscribeNodeInfo, error) {
	if !userSub.ServableAt(now) {
		return nil, nil
	}
	if plan == nil {
		var err error
		if plan, err = s.deps.Plans.FindOne(ctx, userSub.SubscribeId); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				logger.WithContext(ctx).Infow("[QueryUserSubscribeNodeList] Plan of the subscription no longer exists", logger.Field("subscribe_id", userSub.SubscribeId), logger.Field("user_subscribe_id", userSub.Id))
				return nil, nil
			}
			logger.WithContext(ctx).Errorw("[QueryUserSubscribeNodeList] Find plan failed", logger.Field("error", err.Error()), logger.Field("subscribe_id", userSub.SubscribeId))
			return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find plan %d", userSub.SubscribeId)
		}
	}
	nodes, cached := nodesByPlan[plan.Id]
	if !cached {
		// A plan selecting no nodes has none, as in delivery; the scope
		// query without conditions would list every node.
		nodeIDs, tags, err := plan.NodeScope()
		if err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "plan nodes: %v", err)
		}
		if len(nodeIDs) > 0 || len(tags) > 0 {
			var err error
			nodes, err = s.deps.Nodes.ListEnabledNodesByScope(ctx, nodeIDs, tags)
			if err != nil {
				logger.WithContext(ctx).Errorw("[QueryUserSubscribeNodeList] List plan nodes failed", logger.Field("error", err.Error()), logger.Field("subscribe_id", plan.Id))
				return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "list nodes of plan %d", plan.Id)
			}
		}
		nodesByPlan[plan.Id] = nodes
	}

	infos := make([]*dto.UserSubscribeNodeInfo, 0, len(nodes))
	for _, n := range nodes {
		if n.Server == nil {
			continue
		}
		credential := userSub.UUID
		if strings.EqualFold(strings.TrimSpace(n.Protocol), "nowhere") {
			var err error
			credential, err = node.NowhereSharedKey(credential)
			if err != nil {
				return nil, xerr.Wrapf(err, xerr.ERROR, "invalid Nowhere credential for subscription %d", userSub.Id)
			}
		}
		infos = append(infos, &dto.UserSubscribeNodeInfo{
			Id:        n.Id,
			Name:      n.Name,
			Uuid:      credential,
			Protocol:  n.Protocol,
			Port:      n.Port,
			Address:   n.Address,
			Tags:      strings.Split(n.Tags, ","),
			Country:   n.Server.Country,
			City:      n.Server.City,
			CreatedAt: n.CreatedAt.Unix(),
		})
	}
	return infos, nil
}
