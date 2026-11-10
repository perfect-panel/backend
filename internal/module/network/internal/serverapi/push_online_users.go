package serverapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"

	dto "github.com/perfect-panel/server/internal/module/network/contract"
	"github.com/perfect-panel/server/internal/module/network/entity/node"
	"github.com/perfect-panel/server/pkg/logger"
)

// The bounds of one online-user report.
const (
	// maxOnlineUsersPerReport bounds the entries of one report.
	maxOnlineUsersPerReport = 10000
	// maxIPTextLength is the longest textual IP address, an IPv4-mapped
	// IPv6 address written out.
	maxIPTextLength = 45
	// droppedOnlineSIDLogLimit is how many dropped subscription ids a log
	// line names.
	droppedOnlineSIDLogLimit = 20
)

// PushOnlineUsers records the users a server reports online over a protocol,
// with their IPs, for the server and in the global online count. Only the
// subscriptions the server serves over the protocol, the users its user list
// hands it, are recorded; an entry for any other subscription is dropped and
// logged, as the traffic report does. A report over the entry bound, or with
// an address that is not an IP, is refused.
func (s *Service) PushOnlineUsers(ctx context.Context, req *dto.OnlineUsersRequest) error {
	// A report names its server and at least one user, each with a
	// subscription id and an IP.
	if req.ServerId <= 0 || len(req.Users) == 0 {
		return errors.New("invalid request parameters")
	}
	if len(req.Users) > maxOnlineUsersPerReport {
		return fmt.Errorf("too many online users in one report: %d", len(req.Users))
	}
	addresses := make([]net.IP, len(req.Users))
	for i, user := range req.Users {
		if user.SID <= 0 || user.IP == "" || len(user.IP) > maxIPTextLength {
			return fmt.Errorf("invalid user data: uid=%d, ip=%s", user.SID, user.IP)
		}
		ip := net.ParseIP(user.IP)
		if ip == nil {
			return fmt.Errorf("invalid user data: uid=%d, ip=%s", user.SID, user.IP)
		}
		addresses[i] = ip
	}

	log := logger.WithContext(ctx)
	served, err := s.servedSubscriptionIDs(ctx, req.ServerId, req.Protocol)
	if err != nil {
		log.Errorw("[PushOnlineUsers] resolve the served subscriptions failed", logger.Field("error", err.Error()))
		return fmt.Errorf("resolve served subscriptions: %w", err)
	}

	onlineUsers := make(node.OnlineUserSubscribe)
	unserved := 0
	var unservedSIDs []int64
	for i, user := range req.Users {
		if _, ok := served[user.SID]; !ok {
			unserved++
			if len(unservedSIDs) < droppedOnlineSIDLogLimit {
				unservedSIDs = append(unservedSIDs, user.SID)
			}
			continue
		}
		address := addresses[i].String()
		if slices.Contains(onlineUsers[user.SID], address) {
			continue
		}
		onlineUsers[user.SID] = append(onlineUsers[user.SID], address)
	}
	if unserved > 0 {
		// A few are expected around a user-list refresh; many point at a
		// misbehaving node.
		log.Infow("[PushOnlineUsers] Dropped online users the server does not serve",
			logger.Field("server_id", req.ServerId),
			logger.Field("protocol", req.Protocol),
			logger.Field("count", unserved),
			logger.Field("sids", unservedSIDs),
		)
	}
	if err := s.deps.Online.UpdateOnlineUserSubscribe(ctx, req.ServerId, req.Protocol, onlineUsers); err != nil {
		log.Errorw("[PushOnlineUsers] cache operation error", logger.Field("error", err))
		return err
	}
	if err := s.deps.Online.UpdateOnlineUserSubscribeGlobal(ctx, onlineUsers); err != nil {
		log.Errorw("[PushOnlineUsers] cache operation error", logger.Field("error", err))
		return err
	}
	return s.renewOnlineDevices(ctx, onlineUsers)
}
