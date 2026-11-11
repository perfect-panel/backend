package serverapi

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	dto "github.com/perfect-panel/server/internal/module/network/contract"
	"github.com/perfect-panel/server/internal/module/network/entity/node"
	"github.com/redis/go-redis/v9"
)

type beforeClaimHook struct {
	once   sync.Once
	repeat bool
	before func(context.Context) error
}

func (h *beforeClaimHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *beforeClaimHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *beforeClaimHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "evalsha" || cmd.Name() == "eval" {
			var err error
			if h.repeat {
				err = h.before(ctx)
			} else {
				h.once.Do(func() { err = h.before(ctx) })
			}
			if err != nil {
				return err
			}
		}
		return next(ctx, cmd)
	}
}

func TestDeviceClaimBoundsRepeatedInvalidationWithoutGrantingLease(t *testing.T) {
	svc, client := deviceTestService(t, 2)
	if _, err := svc.ClaimServerDevice(t.Context(), claimRequest(4, "10.0.0.1")); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	client.AddHook(&beforeClaimHook{repeat: true, before: func(ctx context.Context) error {
		attempts++
		return client.Incr(ctx, fmt.Sprintf(node.ServerCacheGenerationKey, 4)).Err()
	}})
	if _, err := svc.ClaimServerDevice(t.Context(), claimRequest(4, "10.0.0.2")); !errors.Is(err, ErrServerBusy) {
		t.Fatalf("continuous invalidation was not retryable: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("lease publication attempts=%d, want three bounded attempts", attempts)
	}
	if err := client.ZScore(t.Context(), onlineDeviceKey(21), "10.0.0.2").Err(); !errors.Is(err, redis.Nil) {
		t.Fatalf("continuous invalidation granted a stale lease: %v", err)
	}
}

func TestDeviceClaimFencesInvalidationAtLeasePublication(t *testing.T) {
	for _, change := range []string{"revoked", "limit-reduced", "unrelated-change"} {
		t.Run(change, func(t *testing.T) {
			svc, client := deviceTestService(t, 2)
			if _, err := svc.ClaimServerDevice(t.Context(), claimRequest(4, "10.0.0.1")); err != nil {
				t.Fatal(err)
			}
			client.AddHook(&beforeClaimHook{before: func(ctx context.Context) error {
				user := dto.ServerUser{Id: 21, DeviceLimit: 2}
				switch change {
				case "revoked":
					user.Id = 99
				case "limit-reduced":
					user.DeviceLimit = 1
				}
				cacheIndexUsers(t, client, 4, "vless", time.Minute, user)
				return client.Incr(ctx, fmt.Sprintf(node.ServerCacheGenerationKey, 4)).Err()
			}})
			response, err := svc.ClaimServerDevice(t.Context(), claimRequest(4, "10.0.0.2"))
			switch change {
			case "revoked":
				if err == nil {
					t.Fatalf("revoked SID received a lease: %+v", response)
				}
			case "limit-reduced":
				if err != nil || response.Allowed {
					t.Fatalf("new device bypassed the reduced limit: %+v %v", response, err)
				}
			default:
				if err != nil || !response.Allowed {
					t.Fatalf("unrelated invalidation prevented a fresh claim: %+v %v", response, err)
				}
			}
			if change != "unrelated-change" {
				if err := client.ZScore(t.Context(), onlineDeviceKey(21), "10.0.0.2").Err(); !errors.Is(err, redis.Nil) {
					t.Fatalf("stale grant was written to Redis: %v", err)
				}
			}
		})
	}
}

func TestSharedUserListBuildTimeoutIsRetryable(t *testing.T) {
	for _, endpoint := range []string{"device", "user-list"} {
		t.Run(endpoint, func(t *testing.T) {
			deps, _, _ := newScopeDeps(t)
			deps.Servers = serverReaderFunc(func(context.Context, int64) (*node.Server, error) {
				return nil, context.DeadlineExceeded
			})
			svc := NewService(deps)
			var err error
			if endpoint == "device" {
				_, err = svc.ClaimServerDevice(t.Context(), claimRequest(4, "10.0.0.1"))
			} else {
				_, _, err = svc.GetServerUserList(t.Context(), &dto.GetServerUserListRequest{ServerCommon: dto.ServerCommon{ServerId: 4, Protocol: "vless"}}, RequestMeta{})
			}
			if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrServerBusy) {
				t.Fatalf("shared build timeout lost retry classification: %v", err)
			}
		})
	}
}

func TestServedIndexRetriesInvalidationAtDelivery(t *testing.T) {
	deps, _, client := newScopeDeps(t)
	cacheIndexUsers(t, client, 4, "vless", time.Minute, dto.ServerUser{Id: 21, DeviceLimit: 2})
	base := deps.Caches
	reads := 0
	deps.Caches = generationReader{ServerCache: base, read: func(ctx context.Context, id int64) (int64, error) {
		reads++
		// Initial lookup, shared fill lookup and fill fence precede the
		// delivery fence. Invalidate at the latter, before the waiter returns.
		if reads == 4 {
			cacheIndexUsers(t, client, 4, "vless", time.Minute, dto.ServerUser{Id: 99, DeviceLimit: 1})
			if err := client.Incr(ctx, fmt.Sprintf(node.ServerCacheGenerationKey, id)).Err(); err != nil {
				return 0, err
			}
		}
		return base.ServerCacheGeneration(ctx, id)
	}}
	index, err := NewService(deps).servedUserIndex(t.Context(), 4, "vless")
	if err != nil {
		t.Fatalf("normal invalidation at delivery was not retried: %v", err)
	}
	if _, stale := index.ids[21]; stale || index.limits[99] != 1 {
		t.Fatalf("stale snapshot delivered: %+v", index)
	}
}
