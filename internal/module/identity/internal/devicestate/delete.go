// Package devicestate owns consistent device removal shared by user and admin
// entry points. Redis generations revoke sessions; the database transaction
// removes both copies of the device identity. It also keeps the presence the
// device WebSocket reports: the online flag and the online time of each
// connection.
package devicestate

import (
	"context"
	"errors"

	"github.com/perfect-panel/backend/internal/auth/devicesession"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Delete removes the device id with its identity and ends its sessions, in
// one identity transaction, and returns the removed device; a device that
// does not exist is no error and returns nil. A positive ownerID refuses a
// device of another account.
func Delete(ctx context.Context, store Store, client *redis.Client, id, ownerID int64) (*user.Device, error) {
	var removed *user.Device
	err := store.InIdentityTx(ctx, func(tx repository.IdentityStore) error {
		device, err := tx.UserDevice().FindDeviceForAuth(ctx, id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if ownerID > 0 && device.UserId != ownerID {
			return xerr.NewErrCode(xerr.InvalidAccess)
		}
		if err := devicesession.Revoke(ctx, client, device.Id); err != nil {
			return err
		}
		if err := tx.UserDevice().DeleteDevice(ctx, device.Id); err != nil {
			return err
		}
		if err := tx.UserAuth().DeleteUserAuthMethodByIdentifier(ctx, "device", device.Identifier); err != nil {
			return err
		}
		removed = device
		return nil
	})
	return removed, err
}

// Store is the persistence capability required by this package. It excludes
// unrelated repositories and application-wide transactions.
type Store interface {
	repository.IdentityTransactor
}
