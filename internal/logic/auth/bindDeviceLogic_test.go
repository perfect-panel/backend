package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/logger/logtest"
)

type bindDeviceTestStore struct {
	repository.Store
	devices repository.UserDeviceRepo
}

func (s bindDeviceTestStore) UserDevice() repository.UserDeviceRepo { return s.devices }

type failingBindDeviceRepo struct {
	repository.UserDeviceRepo
	err   error
	calls int
}

func (r *failingBindDeviceRepo) FindOneDeviceByIdentifier(context.Context, string) (*user.Device, error) {
	r.calls++
	return nil, r.err
}

func TestBindDeviceUsesInjectedStore(t *testing.T) {
	logtest.Discard(t)
	queryErr := errors.New("device lookup failed")
	devices := &failingBindDeviceRepo{err: queryErr}
	logic := NewBindDeviceLogic(context.Background(), BindDeviceDependencies{
		Store: bindDeviceTestStore{devices: devices},
	})

	err := logic.BindDeviceToUser("device-1", "192.0.2.1", "test-agent", 1)
	if err == nil {
		t.Fatal("BindDeviceToUser error = nil, want lookup error")
	}
	if devices.calls != 1 {
		t.Fatalf("FindOneDeviceByIdentifier calls = %d, want 1", devices.calls)
	}
}
