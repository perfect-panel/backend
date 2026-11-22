package repo

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/redis/go-redis/v9"
)

func TestClearServerCacheUsesRegisteredKeys(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	repo := NewNodeRepo(nil, client)
	serverID := int64(42)
	userKey := fmt.Sprintf("%s%d:trojan", node.ServerUserListCacheKey, serverID)
	configKey := fmt.Sprintf("%s%d:trojan", node.ServerConfigCacheKey, serverID)
	for _, key := range []string{userKey, configKey} {
		if err := repo.SetServerCache(ctx, serverID, key, "cached", 0); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	if err := repo.ClearServerCache(ctx, serverID); err != nil {
		t.Fatalf("clear server cache: %v", err)
	}
	for _, key := range []string{userKey, configKey, fmt.Sprintf(node.ServerCacheIndexKey, serverID)} {
		if server.Exists(key) {
			t.Fatalf("cache key %q still exists", key)
		}
	}
}

func TestSetServerCacheRejectsStaleGeneration(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	repo := NewNodeRepo(nil, client)
	serverID := int64(42)
	key := fmt.Sprintf("%s%d:trojan", node.ServerConfigCacheKey, serverID)

	if err := repo.ClearServerCache(ctx, serverID); err != nil {
		t.Fatalf("advance server cache generation: %v", err)
	}
	generation, err := repo.ServerCacheGeneration(ctx, serverID)
	if err != nil {
		t.Fatalf("read server cache generation: %v", err)
	}
	if generation != 1 {
		t.Fatalf("generation = %d, want 1", generation)
	}

	if err := repo.SetServerCache(ctx, serverID, key, "stale", generation-1); err != nil {
		t.Fatalf("set stale cache: %v", err)
	}
	if server.Exists(key) {
		t.Fatalf("stale generation repopulated %q", key)
	}

	if err := repo.SetServerCache(ctx, serverID, key, "fresh", generation); err != nil {
		t.Fatalf("set current cache: %v", err)
	}
	if !server.Exists(key) {
		t.Fatalf("current generation did not populate %q", key)
	}
}

func TestClearServerCacheScopesIndexedAndLegacyKeys(t *testing.T) {
	server := miniredis.RunT(t)
	prefix := "panel*"
	client := (config.RedisConfig{Host: server.Addr(), KeyPrefix: prefix}).NewClient()
	t.Cleanup(func() { _ = client.Close() })
	ctx := t.Context()
	r := NewNodeRepo(nil, client)
	const key = "server:user:42"
	const variant = key + ":trojan"
	const other = "panel-b:" + variant
	if err := server.Set(other, "other"); err != nil {
		t.Fatal(err)
	}
	if err := server.Set(key, "unprefixed"); err != nil {
		t.Fatal(err)
	}
	for _, indexed := range []bool{false, true} {
		if err := server.Set(prefix+":"+key, "legacy"); err != nil {
			t.Fatal(err)
		}
		if indexed {
			generation, err := r.ServerCacheGeneration(ctx, 42)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.SetServerCache(ctx, 42, variant, "list", generation); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := server.Set(prefix+":"+variant, "legacy variant"); err != nil {
				t.Fatal(err)
			}
		}
		if err := r.ClearServerCache(ctx, 42); err != nil {
			t.Fatal(err)
		}
		if server.Exists(prefix+":"+key) || server.Exists(prefix+":"+variant) || !server.Exists(other) || !server.Exists(key) {
			t.Fatalf("indexed=%v left incorrect keys %v", indexed, server.Keys())
		}
	}
}

// beforeCacheClear runs a second writer after discovery and before EVAL.
type beforeCacheClear struct{ fill func(context.Context) error }

func (h *beforeCacheClear) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *beforeCacheClear) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *beforeCacheClear) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "eval" && h.fill != nil {
			fill := h.fill
			h.fill = nil
			if err := fill(ctx); err != nil {
				return err
			}
		}
		return next(ctx, cmd)
	}
}

func TestClearServerCacheIncludesConcurrentFill(t *testing.T) {
	addr := os.Getenv("PPANEL_TEST_DRAGONFLY_ADDR")
	if addr == "" {
		addr = miniredis.RunT(t).Addr()
	}
	for _, tc := range []struct{ prefixed, legacy bool }{
		{false, false}, {true, false}, {false, true}, {true, true},
	} {
		t.Run(fmt.Sprintf("prefix=%t/legacy=%t", tc.prefixed, tc.legacy), func(t *testing.T) {
			id := time.Now().UnixNano()
			cfg := config.RedisConfig{Host: addr}
			if tc.prefixed {
				cfg.KeyPrefix = "cache-test*"
			}
			client, writer := cfg.NewClient(), cfg.NewClient()
			t.Cleanup(func() { _ = client.Close() })
			t.Cleanup(func() { _ = writer.Close() })
			ctx := t.Context()
			r, w := NewNodeRepo(nil, client), NewNodeRepo(nil, writer)
			userKey := fmt.Sprintf("%s%d:vless", node.ServerUserListCacheKey, id)
			configKey := fmt.Sprintf("%s%d:trojan", node.ServerConfigCacheKey, id)
			genKey := fmt.Sprintf(node.ServerCacheGenerationKey, id)
			idxKey := fmt.Sprintf(node.ServerCacheIndexKey, id)
			t.Cleanup(func() { _ = writer.Del(context.Background(), genKey, idxKey, userKey, configKey).Err() })
			var err error
			if tc.legacy {
				err = writer.Set(ctx, userKey, "old", time.Minute).Err()
			} else {
				err = r.SetServerCache(ctx, id, userKey, "old", 0)
			}
			if err != nil {
				t.Fatal(err)
			}
			hook := &beforeCacheClear{fill: func(ctx context.Context) error {
				return w.SetServerCache(ctx, id, configKey, "concurrent", 0)
			}}
			client.AddHook(hook)
			if err := r.ClearServerCache(ctx, id); err != nil {
				t.Fatal(err)
			}
			if hook.fill != nil {
				t.Fatal("concurrent fill did not run")
			}
			if generation, err := w.ServerCacheGeneration(ctx, id); err != nil || generation != 1 {
				t.Fatalf("generation = %d, %v; want 1", generation, err)
			}
			if err := w.SetServerCache(ctx, id, configKey, "stale refill", 0); err != nil {
				t.Fatal(err)
			}
			if n, err := writer.Exists(ctx, userKey, configKey, idxKey).Result(); err != nil || n != 0 {
				t.Fatalf("remaining cache keys = %d, %v", n, err)
			}
			if err := w.SetServerCache(ctx, id, configKey, "fresh", 1); err != nil {
				t.Fatal(err)
			}
			if got, err := writer.Get(ctx, configKey).Result(); err != nil || got != "fresh" {
				t.Fatalf("fresh cache = %q, %v", got, err)
			}
		})
	}
}

func TestClearServerCacheRejectsForeignIndexMember(t *testing.T) {
	server := miniredis.RunT(t)
	client := (config.RedisConfig{Host: server.Addr(), KeyPrefix: "panel"}).NewClient()
	t.Cleanup(func() { _ = client.Close() })
	r := NewNodeRepo(nil, client)
	const id = 42
	const variant = "server:user:42:trojan"
	const foreignKey = "other:" + variant
	if err := r.SetServerCache(t.Context(), id, variant, "own", 0); err != nil {
		t.Fatal(err)
	}
	if err := server.Set(foreignKey, "other"); err != nil {
		t.Fatal(err)
	}
	indexKey := fmt.Sprintf("panel:"+node.ServerCacheIndexKey, id)
	if _, err := server.SAdd(indexKey, foreignKey); err != nil {
		t.Fatal(err)
	}
	if err := r.ClearServerCache(t.Context(), id); err == nil {
		t.Fatal("accepted an index member outside the project")
	}
	if !server.Exists(foreignKey) || !server.Exists("panel:"+variant) || !server.Exists(indexKey) {
		t.Fatalf("clear changed keys despite the invalid index member: %v", server.Keys())
	}
	if gen, err := r.ServerCacheGeneration(t.Context(), id); gen != 0 || err != nil {
		t.Fatalf("generation = %d, %v; want 0", gen, err)
	}
}
