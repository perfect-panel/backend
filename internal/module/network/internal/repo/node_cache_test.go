package repo

import (
	"context"
	"fmt"
	"testing"

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
