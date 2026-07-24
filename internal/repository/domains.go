package repository

import (
	"context"

	"github.com/perfect-panel/server/internal/model/entity/user"
	"gorm.io/gorm"
)

// Domain store views (ADR-001 step 2). A scoped transaction hands the closure
// one of these views instead of the full Store, so a cross-domain write
// inside a single-domain transaction no longer compiles. Log() appears in
// every view because audit logging is an exempted cross-cutting concern, and
// Inbox() because the idempotent-consumer markers must commit with the
// domain's own mutations.

// WalletRepo is the billing domain's window onto the user table's money
// columns (Balance, GiftAmount, Commission). The columns living on the user
// row is recorded data debt: ADR-001 step 5 moves them into a wallet table
// owned by billing. Until then this view keeps wallet movements inside
// billing transactions without exposing the rest of the identity repository.
type WalletRepo interface {
	FindOneForUpdate(ctx context.Context, id int64) (*user.User, error)
	UpdateBalanceFields(ctx context.Context, data *user.User, tx ...*gorm.DB) error
	UpdateCommission(ctx context.Context, data *user.User, tx ...*gorm.DB) error
}

// BillingStore is the billing domain's transactional surface: orders,
// payments, coupons, withdrawals and wallet movements.
type BillingStore interface {
	Order() OrderRepo
	OrderEvent() OrderEventRepo
	Payment() PaymentRepo
	Coupon() CouponRepo
	UserWithdrawal() UserWithdrawalRepo
	Wallet() WalletRepo
	Inbox() InboxRepo
	Log() LogRepo
}

// SubscriptionStore is the subscription domain's transactional surface:
// plans, user subscriptions and their traffic quota state.
type SubscriptionStore interface {
	Subscribe() SubscribeRepo
	UserSubscription() UserSubscriptionRepo
	SubscriptionTraffic() SubscriptionTrafficRepo
	Inbox() InboxRepo
	Log() LogRepo
}

// IdentityStore is the identity domain's transactional surface: accounts,
// auth methods and devices.
type IdentityStore interface {
	User() UserRepo
	UserAuth() UserAuthRepo
	UserDevice() UserDeviceRepo
	Auth() AuthRepo
	Inbox() InboxRepo
	Log() LogRepo
}

// NetworkStore is the network domain's transactional surface: nodes and
// traffic statistics.
type NetworkStore interface {
	Node() NodeRepo
	TrafficLog() TrafficRepo
	Inbox() InboxRepo
	Log() LogRepo
}

// PlatformStore is the platform domain's transactional surface: system
// settings and audit/message logs.
type PlatformStore interface {
	System() SystemRepo
	Log() LogRepo
	Inbox() InboxRepo
}

// The full store satisfies every domain view; the scoped transactions below
// hand out the narrowed interface.
var (
	_ BillingStore      = (*GormStore)(nil)
	_ SubscriptionStore = (*GormStore)(nil)
	_ IdentityStore     = (*GormStore)(nil)
	_ NetworkStore      = (*GormStore)(nil)
	_ PlatformStore     = (*GormStore)(nil)
)

// Wallet exposes the billing view of the user table's money columns.
func (s *GormStore) Wallet() WalletRepo { return s.user }

// InBillingTx runs fn inside a transaction scoped to the billing domain.
func (s *GormStore) InBillingTx(ctx context.Context, fn func(BillingStore) error) error {
	return s.InTx(ctx, func(store Store) error {
		return fn(store.(BillingStore))
	})
}

// InSubscriptionTx runs fn inside a transaction scoped to the subscription domain.
func (s *GormStore) InSubscriptionTx(ctx context.Context, fn func(SubscriptionStore) error) error {
	return s.InTx(ctx, func(store Store) error {
		return fn(store.(SubscriptionStore))
	})
}

// InIdentityTx runs fn inside a transaction scoped to the identity domain.
func (s *GormStore) InIdentityTx(ctx context.Context, fn func(IdentityStore) error) error {
	return s.InTx(ctx, func(store Store) error {
		return fn(store.(IdentityStore))
	})
}

// InNetworkTx runs fn inside a transaction scoped to the network domain.
func (s *GormStore) InNetworkTx(ctx context.Context, fn func(NetworkStore) error) error {
	return s.InTx(ctx, func(store Store) error {
		return fn(store.(NetworkStore))
	})
}

// InPlatformTx runs fn inside a transaction scoped to the platform domain.
func (s *GormStore) InPlatformTx(ctx context.Context, fn func(PlatformStore) error) error {
	return s.InTx(ctx, func(store Store) error {
		return fn(store.(PlatformStore))
	})
}
