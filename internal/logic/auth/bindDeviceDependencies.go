package auth

import (
	"context"

	"github.com/perfect-panel/server/internal/repository"
)

// BindDeviceStore is the persistence surface used by device binding. It
// excludes unrelated application repositories.
type BindDeviceStore interface {
	User() repository.UserRepo
	UserAuth() repository.UserAuthRepo
	UserDevice() repository.UserDeviceRepo
	InTx(ctx context.Context, fn func(repository.Store) error) error
}

// BindDeviceDependencies explicitly declares the collaborators of device
// binding instead of passing ServiceContext to business logic.
type BindDeviceDependencies struct {
	Store BindDeviceStore
}
