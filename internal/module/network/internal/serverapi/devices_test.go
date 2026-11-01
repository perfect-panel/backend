package serverapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dto "github.com/perfect-panel/server/internal/module/network/contract"
	"github.com/perfect-panel/server/internal/module/network/entity/node"
	"github.com/redis/go-redis/v9"
)

func deviceTestService(t *testing.T, limit int64) (*Service, *redis.Client) {
	t.Helper()
	deps, _, client := newScopeDeps(t)
	deps.Online = &onlineRecorder{}
	data, _ := json.Marshal(dto.GetServerUserListResponse{Users: []dto.ServerUser{{Id: 21, UUID: "user-21", DeviceLimit: limit}}})
	for _, id := range []int64{4, 5} {
		if err := client.Set(t.Context(), serverUserListCacheKey(id, "vless"), data, time.Hour).Err(); err != nil {
			t.Fatal(err)
		}
	}
	return NewService(deps), client
}

func claimRequest(server int64, ip string) *dto.ClaimServerDeviceRequest {
	return &dto.ClaimServerDeviceRequest{ServerCommon: dto.ServerCommon{ServerId: server, Protocol: "vless"}, SID: 21, IP: ip}
}

func TestDeviceClaimsAreAtomicAcrossServers(t *testing.T) {
	svc, _ := deviceTestService(t, 1)
	var accepted atomic.Int32
	var winner atomic.Value
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ip := fmt.Sprintf("10.0.0.%d", i+1)
			resp, err := svc.ClaimServerDevice(context.Background(), claimRequest(int64(4+i%2), ip))
			if err != nil {
				t.Error(err)
				return
			}
			if resp.Allowed {
				accepted.Add(1)
				winner.Store(ip)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("admitted %d different IPs for one slot", accepted.Load())
	}
	resp, err := svc.ClaimServerDevice(t.Context(), claimRequest(5, "::ffff:"+winner.Load().(string)))
	if err != nil || !resp.Allowed {
		t.Fatalf("same IP across nodes: %+v, %v", resp, err)
	}
	alive, err := svc.GetServerAliveList(t.Context(), &dto.GetServerUserListRequest{ServerCommon: dto.ServerCommon{ServerId: 4, Protocol: "vless"}})
	if err != nil || alive.Alive[21] != 1 {
		t.Fatalf("counts: %+v, %v", alive, err)
	}
}

func TestDeviceCountsExpireRenewAndRespectScope(t *testing.T) {
	svc, client := deviceTestService(t, 2)
	resp, err := svc.ClaimServerDevice(t.Context(), claimRequest(4, "10.0.0.1"))
	if err != nil || !resp.Allowed || resp.TTLSeconds != 300 {
		t.Fatalf("claim: %+v, %v", resp, err)
	}
	// Expire one member while the key itself still exists, as with mixed-age leases.
	client.ZAdd(t.Context(), onlineDeviceKey(21), redis.Z{Member: "10.0.0.1", Score: 1})
	client.ZAdd(t.Context(), onlineDeviceKey(99), redis.Z{Member: "10.0.0.9", Score: float64(time.Now().Add(time.Hour).Unix())})
	query := &dto.GetServerUserListRequest{ServerCommon: dto.ServerCommon{ServerId: 4, Protocol: "vless"}}
	alive, err := svc.GetServerAliveList(t.Context(), query)
	if err != nil || len(alive.Alive) != 0 {
		t.Fatalf("expired or unserved counts: %+v, %v", alive, err)
	}
	if err := svc.PushOnlineUsers(t.Context(), onlineRequest(dto.OnlineUser{SID: 21, IP: "10.0.0.1"}, dto.OnlineUser{SID: 21, IP: "::ffff:10.0.0.1"})); err != nil {
		t.Fatal(err)
	}
	alive, err = svc.GetServerAliveList(t.Context(), query)
	if err != nil || alive.Alive[21] != 1 || len(alive.Alive) != 1 {
		t.Fatalf("renewed counts: %+v, %v", alive, err)
	}
	if ttl := client.TTL(t.Context(), onlineDeviceKey(21)).Val(); ttl <= 0 || ttl > node.Expiry {
		t.Fatalf("lease key TTL: %v", ttl)
	}
	foreign := claimRequest(4, "10.0.0.2")
	foreign.SID = 99
	if _, err := svc.ClaimServerDevice(t.Context(), foreign); err == nil {
		t.Fatal("unserved subscription admitted")
	}
	wrongProtocol := claimRequest(4, "10.0.0.2")
	wrongProtocol.Protocol = "trojan"
	if _, err := svc.ClaimServerDevice(t.Context(), wrongProtocol); err == nil {
		t.Fatal("wrong protocol admitted")
	}
	for _, ip := range []string{"bad", "", "::", "fe80::1%en0"} {
		if _, err := svc.ClaimServerDevice(t.Context(), claimRequest(4, ip)); err == nil {
			t.Fatalf("invalid source accepted: %q", ip)
		}
	}
}

func TestDeviceZeroLimitAndRedisFailure(t *testing.T) {
	svc, client := deviceTestService(t, 0)
	for i := 1; i <= 3; i++ {
		resp, err := svc.ClaimServerDevice(t.Context(), claimRequest(4, fmt.Sprintf("10.0.0.%d", i)))
		if err != nil || !resp.Allowed {
			t.Fatalf("unlimited: %+v, %v", resp, err)
		}
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClaimServerDevice(t.Context(), claimRequest(4, "10.0.0.4")); err == nil {
		t.Fatal("cache failure admitted device")
	}
	if _, err := svc.GetServerAliveList(t.Context(), &dto.GetServerUserListRequest{ServerCommon: dto.ServerCommon{ServerId: 4, Protocol: "vless"}}); err == nil {
		t.Fatal("cache failure became empty count")
	}
}

func TestHeartbeatDoesNotShortenNewerDeviceLease(t *testing.T) {
	svc, client := deviceTestService(t, 1)
	now, err := client.Time(t.Context()).Result()
	if err != nil {
		t.Fatal(err)
	}
	newerExpiry := float64(now.Add(node.Expiry + time.Minute).Unix())
	if err := client.ZAdd(t.Context(), onlineDeviceKey(21), redis.Z{Member: "10.0.0.1", Score: newerExpiry}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := svc.renewOnlineDevices(t.Context(), node.OnlineUserSubscribe{21: {"10.0.0.1"}}); err != nil {
		t.Fatal(err)
	}
	if got := client.ZScore(t.Context(), onlineDeviceKey(21), "10.0.0.1").Val(); got != newerExpiry {
		t.Fatalf("heartbeat shortened lease: %v -> %v", newerExpiry, got)
	}
}
