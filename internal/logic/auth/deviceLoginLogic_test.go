package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"gorm.io/gorm"
)

type fakeDeviceLoginPolicy struct {
	registrationMethods []string
	err                 error
}

func (p *fakeDeviceLoginPolicy) EnsureRegistrationOpen(_ context.Context, method string) error {
	p.registrationMethods = append(p.registrationMethods, method)
	return p.err
}

func (p *fakeDeviceLoginPolicy) VerifyHuman(context.Context, string, string) error { return nil }
func (p *fakeDeviceLoginPolicy) TakeIPPermit(context.Context, string) error        { return nil }

type missingDeviceRepo struct {
	repository.UserDeviceRepo
}

func (missingDeviceRepo) FindOneDeviceByIdentifier(context.Context, string) (*user.Device, error) {
	return nil, gorm.ErrRecordNotFound
}

type deviceLoginTestStore struct {
	repository.Store
	devices repository.UserDeviceRepo
}

func (s deviceLoginTestStore) UserDevice() repository.UserDeviceRepo { return s.devices }

func TestDeviceLoginUsesInjectedRegistrationPolicy(t *testing.T) {
	blocked := errors.New("registration blocked")
	policy := &fakeDeviceLoginPolicy{err: blocked}
	logic := NewDeviceLoginLogic(context.Background(), DeviceLoginDependencies{
		Store:  deviceLoginTestStore{devices: missingDeviceRepo{}},
		Config: DeviceLoginConfig{Enabled: true},
		Policy: policy,
	})

	_, err := logic.DeviceLogin(&dto.DeviceLoginRequest{Identifier: "new-device", IP: "192.0.2.1"})
	if !errors.Is(err, blocked) {
		t.Fatalf("DeviceLogin error = %v, want registration policy error", err)
	}
	if len(policy.registrationMethods) != 1 || policy.registrationMethods[0] != deviceRegistrationMethod {
		t.Fatalf("registration methods = %#v, want [device]", policy.registrationMethods)
	}
}
