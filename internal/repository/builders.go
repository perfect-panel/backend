package repository

import (
	"context"
	"time"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository/kernel"
)

// The store is assembled from per-module repository bundles (ADR-001 step-6
// preparation: each module owns its persistence implementation and exports a
// builder; this package keeps only the contracts and the assembly).
//
// A builder runs once for the root connection and once per transaction with
// the tx-scoped connection. Anything that must survive across transactions
// (cache-retry singletons and similar) belongs in the closure that produced
// the builder, not in the bundle.

// ModuleConn is the per-connection context handed to a repo builder. The
// kernel package declares it, with the platform bundle below, so the platform
// module builds its repositories without depending on this package.
type ModuleConn = kernel.ModuleConn

// SubscriptionCacheBridge is the identity bundle's window onto the
// subscription domain's cache concerns: the user-deletion cascade collects
// the user's subscription rows, and the UserCache facade delegates the
// subscription cache operations. The subscription bundle provides it.
type SubscriptionCacheBridge interface {
	QueryUserSubscribe(ctx context.Context, userId int64, status ...int64) ([]*usersub.SubscribeDetails, error)
	ClearSubscribeCache(ctx context.Context, data ...*usersub.Subscribe) error
	UpdateUserSubscribeCache(ctx context.Context, data *usersub.Subscribe) error
}

// SubscriptionUserFilter narrows SubscriptionUserIDs. Zero-value fields do
// not constrain; a nil Statuses matches any subscription row.
type SubscriptionUserFilter struct {
	UserSubscribeID *int64
	SubscribeID     *int64
	// Token matches the subscription token or UUID.
	Token    string
	Statuses []int64
}

// SubscriptionScopeBridge is the identity bundle's window onto subscription
// membership: the admin user filter and the email-recipient scopes resolve
// "which users hold matching subscriptions" to an ID list here instead of
// querying the subscription table from identity SQL. The subscription
// bundle provides it.
type SubscriptionScopeBridge interface {
	SubscriptionUserIDs(ctx context.Context, filter SubscriptionUserFilter) ([]int64, error)
}

// OrderStatsBridge is the identity bundle's window onto billing's order
// statistics: user-statistics dashboards merge these per-bucket counts with
// registration counts in Go. The billing bundle provides it.
type OrderStatsBridge interface {
	// OrderUserCountsByBucket counts distinct ordering users per date bucket
	// ("day" or "month"); isNew selects first-purchase orders.
	OrderUserCountsByBucket(ctx context.Context, isNew bool, since time.Time, until *time.Time, bucket string) (map[string]int64, error)
}

// NodeCacheKeyBridge is the subscription bundle's window onto network's
// node-facing caches: a plan write that changes which subscriptions the
// servers serve invalidates, through it, the user lists of the servers
// carrying the plan's nodes and node tags. The network bundle provides it
// and runs the invalidation under its cache generation fence, which a plain
// DEL of the list keys from the subscription bundle bypassed.
type NodeCacheKeyBridge interface {
	ClearNodeUserListCaches(ctx context.Context, nodeIDs []int64, tags []string) error
}

// IdentityBridges collects the identity bundle's cross-domain windows.
type IdentityBridges struct {
	SubscriptionCache SubscriptionCacheBridge
	SubscriptionScope SubscriptionScopeBridge
	OrderStats        OrderStatsBridge
}

// PlatformRepos is the shared-kernel bundle.
type (
	PlatformRepos   = kernel.PlatformRepos
	PlatformBuilder = kernel.PlatformBuilder
)

// BillingRepos is the billing domain bundle.
type BillingRepos struct {
	Orders      OrderRepo
	OrderEvents OrderEventRepo
	Payments    PaymentRepo
	Coupons     CouponRepo
	Withdrawals UserWithdrawalRepo
	Wallets     WalletRepo
	// OrderStats feeds the identity bundle's user-statistics merge.
	OrderStats OrderStatsBridge
}

type BillingBuilder func(conn ModuleConn) BillingRepos

// SubscriptionRepos is the subscription domain bundle.
type SubscriptionRepos struct {
	Entitlements EntitlementRepo
	Plans        SubscribeRepo
	UserSubs     UserSubscriptionRepo
	Traffic      SubscriptionTrafficRepo
	// Clients holds the subscribe_application rows delivery renders for.
	Clients ClientRepo
	// CacheBridge feeds the identity bundle's cross-domain cache cascade.
	CacheBridge SubscriptionCacheBridge
	// ScopeBridge feeds the identity bundle's subscription-membership
	// filters.
	ScopeBridge SubscriptionScopeBridge
}

type SubscriptionBuilder func(conn ModuleConn, nodes NodeCacheKeyBridge) SubscriptionRepos

// IdentityRepos is the identity domain bundle. UserCache is the cross-domain
// cache facade (its subscription keys come through the injected reader).
type IdentityRepos struct {
	Users     UserRepo
	UserAuths UserAuthRepo
	Devices   UserDeviceRepo
	UserCache UserCacheRepo
	Auths     AuthRepo
}

type IdentityBuilder func(conn ModuleConn, bridges IdentityBridges) IdentityRepos

// NetworkRepos is the network domain bundle.
type NetworkRepos struct {
	Nodes   NodeRepo
	Traffic TrafficRepo
	// NodeKeys feeds the subscription bundle's plan cache invalidation.
	NodeKeys NodeCacheKeyBridge
}

type NetworkBuilder func(conn ModuleConn) NetworkRepos

// SupportRepos is the support domain bundle.
type SupportRepos struct {
	Tickets       TicketRepo
	Announcements AnnouncementRepo
	Ads           AdsRepo
	Documents     DocumentRepo
}

type SupportBuilder func(conn ModuleConn) SupportRepos

// NotificationRepos is the notification domain bundle.
type NotificationRepos struct {
	TelegramTopics TelegramTopicRepo
}

type NotificationBuilder func(conn ModuleConn) NotificationRepos

// Builders carries every module's repo builder for store assembly.
type Builders struct {
	Platform     PlatformBuilder
	Billing      BillingBuilder
	Subscription SubscriptionBuilder
	Identity     IdentityBuilder
	Network      NetworkBuilder
	Support      SupportBuilder
	Notification NotificationBuilder
}
