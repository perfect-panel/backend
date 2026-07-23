package common

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/server/internal/model/entity/system"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/logger/logtest"
)

type globalConfigTestStore struct {
	repository.Store
	system repository.SystemRepo
}

func (s globalConfigTestStore) System() repository.SystemRepo { return s.system }

type failingGlobalConfigSystemRepo struct {
	repository.SystemRepo
	err   error
	calls int
}

func (r *failingGlobalConfigSystemRepo) GetCurrencyConfig(context.Context) ([]*system.System, error) {
	r.calls++
	return nil, r.err
}

func TestGetGlobalConfigUsesInjectedStore(t *testing.T) {
	logtest.Discard(t)
	failure := errors.New("currency config unavailable")
	systems := &failingGlobalConfigSystemRepo{err: failure}
	logic := NewGetGlobalConfigLogic(context.Background(), GetGlobalConfigDependencies{
		Store: globalConfigTestStore{system: systems},
	})

	_, err := logic.GetGlobalConfig()
	if err == nil {
		t.Fatal("GetGlobalConfig error = nil, want currency config error")
	}
	if systems.calls != 1 {
		t.Fatalf("GetCurrencyConfig calls = %d, want 1", systems.calls)
	}
}
