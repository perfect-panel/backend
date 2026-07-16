package registerpolicy

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/redis/go-redis/v9"
)

func TestTakeIPPermit(t *testing.T) {
	server := miniredis.RunT(t)
	svcCtx := &svc.ServiceContext{
		Redis: redis.NewClient(&redis.Options{Addr: server.Addr()}),
		Config: config.Config{Register: config.RegisterConfig{
			EnableIpRegisterLimit:   true,
			IpRegisterLimit:         2,
			IpRegisterLimitDuration: 10,
		}},
	}

	for i := 0; i < 2; i++ {
		if err := TakeIPPermit(context.Background(), svcCtx, "192.0.2.8"); err != nil {
			t.Fatalf("permit %d: %v", i+1, err)
		}
	}
	if err := TakeIPPermit(context.Background(), svcCtx, "192.0.2.8"); err == nil {
		t.Fatal("expected third registration to exceed quota")
	}
	if err := TakeIPPermit(context.Background(), svcCtx, "192.0.2.9"); err != nil {
		t.Fatalf("different IP should have its own quota: %v", err)
	}
}

func TestEnsureRegistrationOpenForEmail(t *testing.T) {
	svcCtx := &svc.ServiceContext{Config: config.Config{Email: config.EmailConfig{Enable: true}}}
	if err := EnsureRegistrationOpen(context.Background(), svcCtx, MethodEmail); err != nil {
		t.Fatalf("enabled registration rejected: %v", err)
	}
	svcCtx.Config.Register.StopRegister = true
	if err := EnsureRegistrationOpen(context.Background(), svcCtx, MethodEmail); err == nil {
		t.Fatal("stopped registration was accepted")
	}
}
