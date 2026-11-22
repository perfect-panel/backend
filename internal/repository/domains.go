package repository

import (
	"context"

	walletEntity "github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"github.com/perfect-panel/backend/internal/repository/kernel"
)

// Domain store views (ADR-001 step 2). A scoped transaction hands the closure
// one of these views instead of the full Store, so a cross-domain write
// inside a single-domain transaction no longer compiles. Log() appears in
// every view because audit logging is an exempted cross-cutting concern, and
// Inbox() because the idempotent-consumer markers must commit with the
// domain's own mutations.

// WalletRepo is the billing domain's repository of the user_wallet table,
// the single source of every account's balance, gift amount and commission.
// Other modules read and move wallets through the billing facade.
type WalletRepo interface {
	// FindOneForUpdate locks and returns the wallet row, seeding it from a
	// zero state when the account has none yet.
	FindOneForUpdate(ctx context.Context, userId int64) (*walletEntity.Wallet, error)
	// FindWallet and FindWalletsByUserIds are the plain (non-locking) reads
	// of the wallet table for display composition; a missing row reads as
	// nil (single) or is absent from the map (batch).
	FindWallet(ctx context.Context, userId int64) (*walletEntity.Wallet, error)
	FindWalletsByUserIds(ctx context.Context, userIds []int64) (map[int64]*walletEntity.Wallet, error)
	// UpdateBalanceFields persists the balance and gift columns;
	// UpdateCommission persists the commission column.
	UpdateBalanceFields(ctx context.Context, data *walletEntity.Wallet) error
	UpdateCommission(ctx context.Context, data *walletEntity.Wallet) error
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
	Outbox() OutboxRepo
	Log() LogRepo
}

// SubscriptionStore is the subscription domain's transactional surface:
// plans, user subscriptions and their traffic quota state.
type SubscriptionStore interface {
	Entitlement() EntitlementRepo
	Subscribe() SubscribeRepo
	UserSubscription() UserSubscriptionRepo
	SubscriptionTraffic() SubscriptionTrafficRepo
	Inbox() InboxRepo
	Outbox() OutboxRepo
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
	Outbox() OutboxRepo
	Log() LogRepo
}

// NetworkStore is the network domain's transactional surface: nodes and
// traffic statistics.
type NetworkStore interface {
	Node() NodeRepo
	TrafficLog() TrafficRepo
	Inbox() InboxRepo
	Outbox() OutboxRepo
	Log() LogRepo
}

// PlatformStore is the platform domain's transactional surface: system
// settings, scheduled-task bookkeeping and audit/message logs. It is the
// shared kernel's, re-exported from the kernel package.
type PlatformStore = kernel.PlatformStore

// Transaction capabilities can be injected without granting the complete
// application store or unrelated domain transactions.
type BillingTransactor interface {
	InBillingTx(context.Context, func(BillingStore) error) error
}

type SubscriptionTransactor interface {
	InSubscriptionTx(context.Context, func(SubscriptionStore) error) error
}

type IdentityTransactor interface {
	InIdentityTx(context.Context, func(IdentityStore) error) error
}

type NetworkTransactor interface {
	InNetworkTx(context.Context, func(NetworkStore) error) error
}

type PlatformTransactor = kernel.PlatformTransactor

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
func (s *GormStore) Wallet() WalletRepo { return s.billing.Wallets }

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
