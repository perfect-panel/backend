package serverapi

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"time"

	dto "github.com/perfect-panel/server/internal/module/network/contract"
	"github.com/perfect-panel/server/internal/module/network/entity/node"
	"github.com/perfect-panel/server/internal/module/network/internal/onlinecache"
	"github.com/redis/go-redis/v9"
)

func onlineDeviceKey(sid int64) string { return fmt.Sprintf("node:online:devices:%d", sid) }

// Each subscription has one global set of canonical source IPs. Redis time
// and an atomic admission prevent simultaneous claims on different nodes from
// exceeding the limit. Reports renew leases; silent nodes expire after 5 min.
var claimDeviceScript = redis.NewScript(`
redis.replicate_commands()
local now = tonumber(redis.call('TIME')[1])
local ttl = tonumber(ARGV[3])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
local previous = redis.call('ZSCORE', KEYS[1], ARGV[1])
if previous or tonumber(ARGV[2]) <= 0 or redis.call('ZCARD', KEYS[1]) < tonumber(ARGV[2]) then
  redis.call('ZADD', KEYS[1], math.max(now + ttl, tonumber(previous) or 0), ARGV[1])
  redis.call('EXPIRE', KEYS[1], ttl)
  return 1
end
return 0
`)

func (s *Service) GetServerAliveList(ctx context.Context, req *dto.GetServerUserListRequest) (*dto.GetServerAliveListResponse, error) {
	if req.ServerId <= 0 || req.Protocol == "" {
		return nil, errors.New("invalid server scope")
	}
	ids, err := s.servedSubscriptionIDs(ctx, req.ServerId, req.Protocol)
	if err != nil {
		return nil, err
	}
	result := &dto.GetServerAliveListResponse{Alive: make(map[int64]int64)}
	if len(ids) == 0 {
		return result, nil
	}
	now, err := s.deps.Redis.Time(ctx).Result()
	if err != nil {
		return nil, err
	}
	pipe := s.deps.Redis.Pipeline()
	counts := make(map[int64]*redis.IntCmd, len(ids))
	for sid := range ids {
		counts[sid] = pipe.ZCount(ctx, onlineDeviceKey(sid), "("+strconv.FormatInt(now.Unix(), 10), "+inf")
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	for sid, count := range counts {
		if count.Val() > 0 {
			result.Alive[sid] = count.Val()
		}
	}
	return result, nil
}

func (s *Service) ClaimServerDevice(ctx context.Context, req *dto.ClaimServerDeviceRequest) (*dto.ClaimServerDeviceResponse, error) {
	select {
	case s.deviceClaims <- struct{}{}:
		defer func() { <-s.deviceClaims }()
	default:
		return nil, ErrServerBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ip, err := netip.ParseAddr(req.IP)
	if err != nil || ip.Zone() != "" || ip.IsUnspecified() || req.SID <= 0 || req.ServerId <= 0 || req.Protocol == "" {
		return nil, errors.New("invalid device claim")
	}
	index, err := s.servedUserIndex(ctx, req.ServerId, req.Protocol)
	if err != nil {
		return nil, err
	}
	limit, served := index.limits[req.SID]
	if !served {
		return nil, errors.New("subscription is not served by this server protocol")
	}
	ttl := int64(node.Expiry.Seconds())
	allowed, err := claimDeviceScript.Run(ctx, s.deps.Redis, []string{onlineDeviceKey(req.SID)}, ip.Unmap().String(), limit, ttl).Int()
	if err != nil {
		return nil, err
	}
	return &dto.ClaimServerDeviceResponse{Allowed: allowed == 1, TTLSeconds: ttl}, nil
}

func (s *Service) renewOnlineDevices(ctx context.Context, online node.OnlineUserSubscribe) error {
	if len(online) == 0 {
		return nil
	}
	pipe := s.deps.Redis.Pipeline()
	for sid, ips := range online {
		key := onlineDeviceKey(sid)
		args := make([]any, 1, len(ips)+1)
		args[0] = int64(node.Expiry.Seconds())
		for _, ip := range ips {
			args = append(args, ip)
		}
		onlinecache.RenewScript.Eval(ctx, pipe, []string{key}, args...)
	}
	_, err := pipe.Exec(ctx)
	return err
}
