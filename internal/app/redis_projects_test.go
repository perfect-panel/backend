package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/redis/go-redis/v9"
)

func TestRedisProjectsShareDatabase(t *testing.T) {
	server := miniredis.RunT(t)
	server.RequireUserAuth("alpha", "pw-a")
	server.RequireUserAuth("beta", "pw-b")
	checkRedisProjects(t, server.Addr())
}

func checkRedisProjects(t *testing.T, addr string) {
	t.Helper()
	ctx := t.Context()
	var inspectors []*asynq.Inspector
	var configs []config.Config
	for _, project := range []struct{ name, prefix, pass string }{
		{"alpha", "alpha:", "pw-a"}, {"beta", "beta", "pw-b"},
	} {
		cfg := config.Config{Redis: config.RedisConfig{
			Host: addr, Username: project.name, Pass: project.pass, KeyPrefix: project.prefix, DB: 5, QueueDB: 5,
		}}
		configs = append(configs, cfg)
		rds := cfg.Redis.NewClient()
		t.Cleanup(func() { _ = rds.Close() })
		_, err := rds.TxPipelined(ctx, func(p redis.Pipeliner) error {
			p.Set(ctx, "session", project.name, time.Minute)
			p.RenameNX(ctx, "session", "active-session")
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		client := NewAsynqClient(cfg)
		t.Cleanup(func() { _ = client.Close() })
		opts := []asynq.Option{asynq.TaskID("same-id"), asynq.Retention(time.Minute)}
		if project.name == "alpha" {
			opts = append(opts, asynq.ProcessIn(time.Second))
		}
		_, err = client.EnqueueContext(ctx, asynq.NewTask("check-project", []byte(project.name)), opts...)
		if err != nil {
			t.Fatal(err)
		}
		inspect := NewAsynqInspector(cfg)
		t.Cleanup(func() { _ = inspect.Close() })
		inspectors = append(inspectors, inspect)
	}

	worker := asynq.NewServer(QueueRedisOpt(configs[0]), asynq.Config{
		Concurrency: 1, LogLevel: asynq.ErrorLevel, TaskCheckInterval: 50 * time.Millisecond,
		DelayedTaskCheckInterval: 50 * time.Millisecond,
		RetryDelayFunc:           func(int, error, *asynq.Task) time.Duration { return time.Second },
	})
	calls := make(chan string, 2)
	if err := worker.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		if n, _ := asynq.GetRetryCount(ctx); n == 0 {
			return errors.New("exercise project-scoped retry")
		}
		calls <- string(task.Payload())
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Shutdown)
	select {
	case got := <-calls:
		if got != "alpha" {
			t.Fatalf("alpha worker handled %q", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("alpha worker did not receive its task")
	}
	info, err := inspectors[1].GetTaskInfo("default", "same-id")
	if err != nil || info.State != asynq.TaskStatePending || string(info.Payload) != "beta" {
		t.Fatalf("beta task = %+v, %v", info, err)
	}
	for _, cfg := range configs {
		rds := cfg.Redis.NewClient()
		t.Cleanup(func() { _ = rds.Close() })
		got, err := rds.Get(ctx, "active-session").Result()
		if err != nil || got != cfg.Redis.Username {
			t.Fatalf("project %s session = %q, %v", cfg.Redis.Username, got, err)
		}
	}
}
