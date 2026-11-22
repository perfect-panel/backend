package adminserver

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/network/internal/repo"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// txNodeStore serves the node repository over SQLite with a real network
// transaction.
type txNodeStore struct {
	db    *gorm.DB
	cache *redis.Client
}

func (s txNodeStore) Node() repository.NodeRepo { return repo.NewNodeRepo(s.db, s.cache) }

func (s txNodeStore) InNetworkTx(ctx context.Context, fn func(repository.NetworkStore) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(txNetworkStore{nodes: repo.NewNodeRepo(tx, s.cache)})
	})
}

type txNetworkStore struct{ nodes repository.NodeRepo }

func (s txNetworkStore) Node() repository.NodeRepo        { return s.nodes }
func (txNetworkStore) TrafficLog() repository.TrafficRepo { return nil }
func (txNetworkStore) Inbox() repository.InboxRepo        { return nil }
func (txNetworkStore) Outbox() repository.OutboxRepo      { return nil }
func (txNetworkStore) Log() repository.LogRepo            { return nil }

func newTxNodeStore(t *testing.T) (*gorm.DB, txNodeStore) {
	t.Helper()
	logtest.Discard(t)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: gormlogger.Discard})
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
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	return db, txNodeStore{db: db, cache: cache}
}

// Deleting a server takes its nodes and its configuration override with it;
// the nodes carry no foreign key, so left behind they would fail every
// render that selects them. Other servers keep their nodes.
func TestDeleteServerRemovesItsNodesAndOverride(t *testing.T) {
	db, store := newTxNodeStore(t)
	ctx := context.Background()
	doomed, kept := &node.Server{Name: "doomed"}, &node.Server{Name: "kept"}
	for _, server := range []*node.Server{doomed, kept} {
		if err := db.Create(server).Error; err != nil {
			t.Fatal(err)
		}
	}
	enabled := true
	nodes := []*node.Node{
		{Name: "doomed-1", ServerId: doomed.Id, Port: 443, Protocol: "vless", Enabled: &enabled},
		{Name: "doomed-2", ServerId: doomed.Id, Port: 8443, Protocol: "trojan", Enabled: &enabled},
		{Name: "kept-1", ServerId: kept.Id, Port: 443, Protocol: "vless", Enabled: &enabled},
	}
	for _, item := range nodes {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	dns := "8.8.8.8"
	if err := db.Create(&node.ServerConfigOverride{ServerId: doomed.Id, DNS: &dns}).Error; err != nil {
		t.Fatal(err)
	}

	if err := NewService(Deps{Store: store}).DeleteServer(ctx, &dto.DeleteServerRequest{Id: doomed.Id}); err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}

	var servers, remaining, overrides int64
	db.Model(&node.Server{}).Count(&servers)
	db.Model(&node.Node{}).Where("server_id = ?", doomed.Id).Count(&remaining)
	db.Model(&node.ServerConfigOverride{}).Where("server_id = ?", doomed.Id).Count(&overrides)
	if servers != 1 || remaining != 0 || overrides != 0 {
		t.Fatalf("after the delete: %d servers, %d nodes of the deleted server, %d overrides", servers, remaining, overrides)
	}
	var keptNodes []*node.Node
	if err := db.Where("server_id = ?", kept.Id).Find(&keptNodes).Error; err != nil || len(keptNodes) != 1 || keptNodes[0].Name != "kept-1" {
		t.Fatalf("the other server's nodes = %+v, %v", keptNodes, err)
	}
}
