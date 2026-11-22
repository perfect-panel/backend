package adminserver

import (
	"context"
	"sort"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/network/internal/protocolmap"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// serverListReader is what the server list reads of the network data: the
// servers of a page and their reported status and online users.
type serverListReader interface {
	FilterServerList(ctx context.Context, params *node.FilterParams) (int64, []*node.Server, error)
	StatusCache(ctx context.Context, serverId int64) (node.Status, error)
	OnlineUserSubscribe(ctx context.Context, serverId int64, protocol string) (node.OnlineUserSubscribe, error)
}

// OnlineSubscriptionReader reads the subscriptions behind the online users,
// with their plans.
type OnlineSubscriptionReader interface {
	SubscriptionDetailsByIDs(ctx context.Context, ids []int64) ([]*usersub.SubscribeDetails, error)
}

// listedServer is a server of the page with the online IPs its nodes
// reported, by subscription.
type listedServer struct {
	server dto.Server
	online map[int64][]dto.ServerOnlineIP
}

// FilterServerList lists a page of servers with their protocols, status and
// online users. The subscriptions behind every online user of the page are
// read in one query, not one per user.
func (s *Service) FilterServerList(ctx context.Context, req *dto.FilterServerListRequest) (*dto.FilterServerListResponse, error) {
	return listServers(ctx, s.deps.Store.Node(), s.deps.Subscriptions, req)
}

func listServers(ctx context.Context, servers serverListReader, subscriptions OnlineSubscriptionReader, req *dto.FilterServerListRequest) (*dto.FilterServerListResponse, error) {
	log := logger.WithContext(ctx)
	total, data, err := servers.FilterServerList(ctx, &node.FilterParams{
		Page:   req.Page,
		Size:   req.Size,
		Search: req.Search,
	})
	if err != nil {
		log.Errorw("[FilterServerList] Query Database Error: ", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "filter servers")
	}

	page := make([]listedServer, 0, len(data))
	var subscriptionIDs []int64
	for _, datum := range data {
		server := serverDTO(datum)
		stored, err := datum.UnmarshalProtocols()
		if err != nil {
			log.Errorw("[FilterServerList] Unmarshal protocols failed", logger.Field("error", err.Error()), logger.Field("server_id", datum.Id))
			continue
		}
		if server.Protocols, err = protocolmap.ToDTO(stored); err != nil {
			log.Errorw("[FilterServerList] Map protocols failed", logger.Field("error", err.Error()), logger.Field("server_id", datum.Id))
			continue
		}
		status, err := servers.StatusCache(ctx, datum.Id)
		if err != nil {
			log.Errorw("[FilterServerList] Read server status failed", logger.Field("error", err.Error()), logger.Field("server_id", datum.Id))
			return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "read status of server %d", datum.Id)
		}
		server.Status = dto.ServerStatus{
			Mem:    status.Mem,
			Cpu:    status.Cpu,
			Disk:   status.Disk,
			Status: reportStatus(datum.LastReportedAt),
		}
		online := onlineIPs(ctx, servers, datum.Id, server.Protocols)
		for id := range online {
			subscriptionIDs = append(subscriptionIDs, id)
		}
		page = append(page, listedServer{server: server, online: online})
	}

	users := onlineSubscriptions(ctx, subscriptions, subscriptionIDs)
	list := make([]dto.Server, 0, len(page))
	for _, item := range page {
		item.server.Status.Online = onlineUsers(item.online, users)
		list = append(list, item.server)
	}
	return &dto.FilterServerListResponse{List: list, Total: total}, nil
}

// serverDTO is the listed form of a stored server, before the list adds its
// protocols and status. Its times are Unix milliseconds; a server that never
// reported has a zero report time.
func serverDTO(datum *node.Server) dto.Server {
	server := dto.Server{
		Id:        datum.Id,
		Name:      datum.Name,
		Country:   datum.Country,
		City:      datum.City,
		Address:   datum.Address,
		Sort:      datum.Sort,
		CreatedAt: datum.CreatedAt.UnixMilli(),
		UpdatedAt: datum.UpdatedAt.UnixMilli(),
	}
	if datum.LastReportedAt != nil {
		server.LastReportedAt = datum.LastReportedAt.UnixMilli()
	}
	return server
}

// onlineIPs gathers the IPs the server's protocols report online, merged by
// subscription.
func onlineIPs(ctx context.Context, servers serverListReader, serverID int64, protocols []dto.Protocol) map[int64][]dto.ServerOnlineIP {
	online := make(map[int64][]dto.ServerOnlineIP)
	for _, protocol := range protocols {
		data, err := servers.OnlineUserSubscribe(ctx, serverID, protocol.Type)
		if err != nil {
			logger.WithContext(ctx).Errorw("[FilterServerList] Read online users failed", logger.Field("error", err.Error()), logger.Field("server_id", serverID), logger.Field("protocol", protocol.Type))
			continue
		}
		for subscriptionID, ips := range data {
			for _, ip := range ips {
				online[subscriptionID] = append(online[subscriptionID], dto.ServerOnlineIP{IP: ip, Protocol: protocol.Type})
			}
		}
	}
	return online
}

// onlineSubscriptions reads the online users' subscriptions with their plans
// in one query. Without them the users are left out, as a missing
// subscription always was.
func onlineSubscriptions(ctx context.Context, subscriptions OnlineSubscriptionReader, ids []int64) map[int64]dto.ServerOnlineUser {
	users := make(map[int64]dto.ServerOnlineUser, len(ids))
	if len(ids) == 0 {
		return users
	}
	details, err := subscriptions.SubscriptionDetailsByIDs(ctx, ids)
	if err != nil {
		logger.WithContext(ctx).Errorw("[FilterServerList] Read online subscriptions failed", logger.Field("error", err.Error()), logger.Field("count", len(ids)))
		return users
	}
	for _, info := range details {
		user := dto.ServerOnlineUser{
			UserId:      info.UserId,
			SubscribeId: info.Id,
			Traffic:     info.Download + info.Upload,
			ExpiredAt:   info.ExpireTime.UnixMilli(),
		}
		if info.Subscribe != nil {
			user.Subscribe = info.Subscribe.Name
		}
		users[info.Id] = user
	}
	return users
}

// onlineUsers lists a server's online users in subscription order.
func onlineUsers(online map[int64][]dto.ServerOnlineIP, subscriptions map[int64]dto.ServerOnlineUser) []dto.ServerOnlineUser {
	result := make([]dto.ServerOnlineUser, 0, len(online))
	for id, ips := range online {
		user, ok := subscriptions[id]
		if !ok {
			continue
		}
		user.IP = ips
		result = append(result, user)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SubscribeId < result[j].SubscribeId })
	return result
}

// reportStatus rates a server by its last report: online within three
// minutes, a warning up to five, offline after that or without any.
func reportStatus(last *time.Time) string {
	if last == nil {
		return "offline"
	}
	if time.Since(*last) > time.Minute*5 {
		return "offline"
	}
	if time.Since(*last) > time.Minute*3 {
		return "warning"
	}
	return "online"
}
