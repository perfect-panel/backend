package adminserver

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/network/internal/repo"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// nodeRepoStore serves the node repository over SQLite; the node update
// opens no transaction.
type nodeRepoStore struct{ nodes repository.NodeRepo }

func (s nodeRepoStore) Node() repository.NodeRepo { return s.nodes }

func (nodeRepoStore) InNetworkTx(context.Context, func(repository.NetworkStore) error) error {
	return errors.New("the node update opens no transaction")
}

func newNodeRepoStore(t *testing.T) (*gorm.DB, nodeRepoStore) {
	t.Helper()
	logtest.Discard(t)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	// Closing the pool discards the shared-cache database, so a repeated
	// run (-count) starts from an empty one.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&node.Server{}, &node.Node{}); err != nil {
		t.Fatal(err)
	}
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	return db, nodeRepoStore{nodes: repo.NewNodeRepo(db, cache)}
}

// An update without the enabled switch keeps the stored one (writing NULL
// failed on the NOT NULL column); an unknown node is a query error that
// keeps its cause.
func TestUpdateNodeKeepsTheSwitchTheRequestOmits(t *testing.T) {
	db, store := newNodeRepoStore(t)
	disabled := false
	server := &node.Server{Name: "edge"}
	if err := db.Create(server).Error; err != nil {
		t.Fatal(err)
	}
	stored := &node.Node{Name: "hk", ServerId: server.Id, Port: 443, Protocol: "vless", Enabled: &disabled}
	if err := db.Create(stored).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(Deps{Store: store})

	err := svc.UpdateNode(context.Background(), &dto.UpdateNodeRequest{
		Id: stored.Id, Name: "hk-2", ServerId: server.Id, Port: 8443, Protocol: "vless",
	})
	if err != nil {
		t.Fatalf("UpdateNode without enabled: %v", err)
	}
	var got node.Node
	if err := db.First(&got, stored.Id).Error; err != nil {
		t.Fatal(err)
	}
	if got.Name != "hk-2" || got.Port != 8443 || got.Enabled == nil || *got.Enabled {
		t.Fatalf("node = %+v, want the new settings and the node still disabled", got)
	}

	err = svc.UpdateNode(context.Background(), &dto.UpdateNodeRequest{Id: 404, Name: "ghost", ServerId: server.Id})
	if xerr.CodeOf(err) != xerr.DatabaseQueryError || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("UpdateNode of an unknown node = %v, want a query error wrapping not found", err)
	}
}
