package config

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func TestRedisURI(t *testing.T) {
	base := RedisConfig{QueueDB: 7, KeyPrefix: "from-file"}
	got, err := base.WithURI("redis://panel:p%40ss@[::1]/3?prefix=tenant%3A")
	if err != nil || got.Host != "[::1]:6379" || got.Username != "panel" || got.Pass != "p@ss" || got.DB != 3 || got.QueueDB != 7 || got.KeyPrefix != "tenant:" {
		t.Fatal("ACL URI did not preserve the configured credentials, databases and prefix", err)
	}
	got, err = base.WithURI("redis://:secret@localhost")
	if err != nil || got.Username != "" || got.Pass != "secret" || got.DB != 0 || got.KeyPrefix != base.KeyPrefix {
		t.Fatal("password-only URI did not preserve file options", err)
	}
}

func TestRedisPing(t *testing.T) {
	server := miniredis.RunT(t)
	server.RequireUserAuth("panel", "pw")
	c := RedisConfig{Host: server.Addr(), Username: "panel", Pass: "pw", KeyPrefix: "test"}
	if err := c.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.Ping(ctx); err == nil {
		t.Fatal("a ping with an ended context succeeded")
	}
}
