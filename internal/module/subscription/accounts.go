package subscription

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
)

// Accounts is the module's port onto the identity domain: the owner accounts,
// devices and email bindings its use cases read, and the account-cache
// invalidation after they changed what an account shows. The composition
// root backs it with the identity facade (ADR-001 rule 4).
type Accounts interface {
	// FindOne returns the account with its devices and auth methods,
	// soft-deleted ones included.
	FindOne(ctx context.Context, id int64) (*user.User, error)
	// FindAccountState returns the account gate (enabled, deleted),
	// soft-deleted accounts included.
	FindAccountState(ctx context.Context, id int64) (*user.AccountState, error)
	// FindUsersByIds returns the live accounts among ids.
	FindUsersByIds(ctx context.Context, ids []int64) ([]*user.User, error)
	// FindUserAuthMethodsByUserIds returns the method bindings of the
	// accounts; soft-deleted accounts resolve to none.
	FindUserAuthMethodsByUserIds(ctx context.Context, method string, userIds []int64) ([]*user.AuthMethods, error)
	// QueryDevicePageList pages the account's devices, optionally only those
	// of one subscription.
	QueryDevicePageList(ctx context.Context, userid, subscribeId int64, page, size int) ([]*user.Device, int64, error)
	// ClearUserCache drops the id-keyed cached projections of the accounts;
	// ClearUserCacheOf every projection the loaded accounts derive.
	ClearUserCache(ctx context.Context, userIDs ...int64) error
	ClearUserCacheOf(ctx context.Context, users ...*user.User) error
}
