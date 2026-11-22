package repo

import (
	"context"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newNodeRepoWithCache(t *testing.T) (*gorm.DB, *miniredis.Miniredis, *nodeRepo) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&node.Server{}, &node.Node{}, &node.ServerConfigOverride{}); err != nil {
		t.Fatal(err)
	}
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return db, server, NewNodeRepo(db, client).(*nodeRepo)
}

// The subscription bundle's invalidation of a plan's node scope goes
// through each scoped server's ClearServerCache: the generation moves, so a
// list rebuilt from the pre-write rows is rejected, and the indexed keys go.
// Servers outside the scope are untouched.
func TestClearNodeUserListCachesFencesEveryScopedServer(t *testing.T) {
	db, cache, repo := newNodeRepoWithCache(t)
	ctx := context.Background()
	servers := make([]*node.Server, 3)
	for i := range servers {
		servers[i] = &node.Server{Name: fmt.Sprintf("server-%d", i)}
		if err := db.Create(servers[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	enabled, disabled := true, false
	nodes := []*node.Node{
		{Name: "by-id", ServerId: servers[0].Id, Tags: "other", Enabled: &enabled, Sort: 1},
		{Name: "by-tag", ServerId: servers[1].Id, Tags: "core,edge", Enabled: &disabled, Sort: 2},
		{Name: "outside", ServerId: servers[2].Id, Tags: "other", Enabled: &enabled, Sort: 3},
	}
	if err := db.Create(nodes).Error; err != nil {
		t.Fatal(err)
	}
	userKey := func(serverID int64) string {
		return fmt.Sprintf("%s%d:vless", node.ServerUserListCacheKey, serverID)
	}
	for _, server := range servers {
		if err := repo.SetServerCache(ctx, server.Id, userKey(server.Id), "cached", 0); err != nil {
			t.Fatal(err)
		}
	}

	// A disabled node's server is in scope too: its list must not keep a
	// subscription the plan no longer serves.
	if err := repo.ClearNodeUserListCaches(ctx, []int64{nodes[0].Id}, []string{"core", ""}); err != nil {
		t.Fatalf("ClearNodeUserListCaches: %v", err)
	}
	for i, want := range []int64{1, 1, 0} {
		generation, err := repo.ServerCacheGeneration(ctx, servers[i].Id)
		if err != nil || generation != want {
			t.Fatalf("server %d generation = %d, %v; want %d", i, generation, err, want)
		}
		if exists := cache.Exists(userKey(servers[i].Id)); exists != (want == 0) {
			t.Fatalf("server %d user list cached = %v after the invalidation", i, exists)
		}
	}

	// A rebuild that read the generation before the invalidation cannot
	// cache its list; one that read it after can.
	if err := repo.SetServerCache(ctx, servers[0].Id, userKey(servers[0].Id), "stale", 0); err != nil {
		t.Fatal(err)
	}
	if cache.Exists(userKey(servers[0].Id)) {
		t.Fatal("a list rebuilt before the invalidation was cached over it")
	}
	if err := repo.SetServerCache(ctx, servers[0].Id, userKey(servers[0].Id), "fresh", 1); err != nil {
		t.Fatal(err)
	}
	if !cache.Exists(userKey(servers[0].Id)) {
		t.Fatal("a list rebuilt after the invalidation was not cached")
	}

	// An empty scope clears nothing.
	if err := repo.ClearNodeUserListCaches(ctx, nil, []string{""}); err != nil {
		t.Fatal(err)
	}
	if generation, _ := repo.ServerCacheGeneration(ctx, servers[0].Id); generation != 1 {
		t.Fatalf("an empty scope moved the generation to %d", generation)
	}
}
