package repo

import (
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/module/network/entity/node"
)

func TestOnlineReportsMergeBatchesAndExpireAddresses(t *testing.T) {
	_, cache, repo := newNodeRepoWithCache(t)
	now := time.Now()
	cache.SetTime(now)
	if err := repo.UpdateOnlineUserSubscribe(t.Context(), 4, "vless", node.OnlineUserSubscribe{21: {"127.0.0.1"}}); err != nil {
		t.Fatal(err)
	}
	cache.SetTime(now.Add(time.Minute))
	if err := repo.UpdateOnlineUserSubscribe(t.Context(), 4, "vless", node.OnlineUserSubscribe{21: {"127.0.0.2"}, 22: {"::1"}}); err != nil {
		t.Fatal(err)
	}
	data, err := repo.OnlineUserSubscribe(t.Context(), 4, "vless")
	if err != nil || len(data[21]) != 2 || len(data[22]) != 1 {
		t.Fatalf("batched online devices: %v, %v", data, err)
	}
	cache.SetTime(now.Add(node.Expiry))
	data, err = repo.OnlineUserSubscribe(t.Context(), 4, "vless")
	if err != nil || len(data[21]) != 1 || data[21][0] != "127.0.0.2" {
		t.Fatalf("expired devices: %v, %v", data, err)
	}
	data, err = repo.OnlineUserSubscribe(t.Context(), 4, "trojan")
	if err != nil || len(data) != 0 {
		t.Fatalf("protocol scope: %v, %v", data, err)
	}
}
