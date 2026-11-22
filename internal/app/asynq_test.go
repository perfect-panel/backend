package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/pkg/conf"
)

// The task queue's Redis database used to be a constant 5, so two
// deployments sharing one Redis consumed each other's tasks whatever their
// Redis.DB said. It follows Redis.QueueDB now, which a configuration file
// without the key still gets as 5, keeping an upgrade's queued tasks.
func TestQueueRedisOptFollowsTheConfiguredQueueDatabase(t *testing.T) {
	var c config.Config
	c.Redis.Host = "redis:6379"
	c.Redis.Pass = "secret"
	c.Redis.DB = 1
	c.Redis.QueueDB = 7

	opt := QueueRedisOpt(c)

	if opt.Addr != "redis:6379" || opt.Password != "secret" || opt.DB != 7 {
		t.Fatalf("queue connection = %+v, want redis:6379 with the password and database 7", opt)
	}

	path := filepath.Join(t.TempDir(), "ppanel.yaml")
	if err := os.WriteFile(path, []byte("Redis:\n  Host: redis:6379\n  DB: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var loaded config.Config
	if err := conf.Load(path, &loaded); err != nil {
		t.Fatal(err)
	}
	if opt := QueueRedisOpt(loaded); opt.DB != 5 || loaded.Redis.DB != 3 {
		t.Fatalf("queue database = %d (Redis.DB %d), want the historical 5 for a file without QueueDB", opt.DB, loaded.Redis.DB)
	}
}
