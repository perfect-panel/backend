// Package repository holds the modules' persistence contracts and the store
// assembled from them: the repository interfaces, the per-domain store views
// and scoped transactions, and GormStore, which builds every module's
// repositories over one shared connection pool with the builders the modules
// export. The implementations live in the owning modules, and each module
// uses only the contracts it owns (ADR-001), so moving a module to its own
// database only means pointing its builder at another connection.
package repository

import (
	"context"
	"time"

	"github.com/perfect-panel/backend/pkg/cache"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Store is the central data access facade, providing access to all domain repositories
// and transaction support.
type Store interface {
	Ads() AdsRepo
	Announcement() AnnouncementRepo
	Auth() AuthRepo
	Client() ClientRepo
	Coupon() CouponRepo
	Document() DocumentRepo
	Inbox() InboxRepo
	// Outbox appends domain events that commit with the domain transaction.
	Outbox() OutboxRepo
	Log() LogRepo
	Node() NodeRepo
	Order() OrderRepo
	OrderEvent() OrderEventRepo
	Payment() PaymentRepo
	Subscribe() SubscribeRepo
	Entitlement() EntitlementRepo
	System() SystemRepo
	Task() TaskRepo
	// TelegramTopic maps forum topics in the admin Telegram group to the
	// conversation each carries.
	TelegramTopic() TelegramTopicRepo
	Ticket() TicketRepo
	TrafficLog() TrafficRepo
	User() UserRepo
	UserAuth() UserAuthRepo
	UserSubscription() UserSubscriptionRepo
	UserDevice() UserDeviceRepo
	UserWithdrawal() UserWithdrawalRepo
	SubscriptionTraffic() SubscriptionTrafficRepo
	UserCache() UserCacheRepo
	// Wallet exposes the billing view of the user table's money columns
	// (ADR-001 step 5 data debt).
	Wallet() WalletRepo

	InTx(ctx context.Context, fn func(store Store) error) error

	// Domain-scoped transactions (ADR-001 step 2): the closure receives only
	// its domain's store view, so cross-domain writes fail to compile.
	InBillingTx(ctx context.Context, fn func(BillingStore) error) error
	InSubscriptionTx(ctx context.Context, fn func(SubscriptionStore) error) error
	InIdentityTx(ctx context.Context, fn func(IdentityStore) error) error
	InNetworkTx(ctx context.Context, fn func(NetworkStore) error) error
	InPlatformTx(ctx context.Context, fn func(PlatformStore) error) error
}

var _ Store = (*GormStore)(nil)

// GormStore is the Store implementation assembled from per-module repo
// bundles over one shared connection pool.
type GormStore struct {
	db            *gorm.DB
	redis         *redis.Client
	invalidations *cache.InvalidationQueue
	retrier       *cache.InvalidationRetrier
	builders      Builders

	platform     PlatformRepos
	billing      BillingRepos
	subscription SubscriptionRepos
	identity     IdentityRepos
	network      NetworkRepos
	support      SupportRepos
	notification NotificationRepos
}

// NewGormStoreWithBuilders assembles the store from the given per-module
// repo builders.
func NewGormStoreWithBuilders(db *gorm.DB, rds *redis.Client, builders Builders) *GormStore {
	return newGormStore(db, rds, nil, cache.NewInvalidationRetrier(rds), builders)
}

func newGormStore(db *gorm.DB, rds *redis.Client, invalidations *cache.InvalidationQueue, retrier *cache.InvalidationRetrier, builders Builders) *GormStore {
	conn := ModuleConn{DB: db, Redis: rds, Invalidations: invalidations}
	s := &GormStore{
		db:            db,
		redis:         rds,
		invalidations: invalidations,
		retrier:       retrier,
		builders:      builders,
	}
	s.platform = builders.Platform(conn)
	s.network = builders.Network(conn)
	s.subscription = builders.Subscription(conn, s.network.NodeKeys)
	s.billing = builders.Billing(conn)
	s.identity = builders.Identity(conn, IdentityBridges{
		SubscriptionCache: s.subscription.CacheBridge,
		SubscriptionScope: s.subscription.ScopeBridge,
		OrderStats:        s.billing.OrderStats,
	})
	s.support = builders.Support(conn)
	s.notification = builders.Notification(conn)
	return s
}

func (s *GormStore) Ads() AdsRepo                                 { return s.support.Ads }
func (s *GormStore) Announcement() AnnouncementRepo               { return s.support.Announcements }
func (s *GormStore) Auth() AuthRepo                               { return s.identity.Auths }
func (s *GormStore) Client() ClientRepo                           { return s.subscription.Clients }
func (s *GormStore) Coupon() CouponRepo                           { return s.billing.Coupons }
func (s *GormStore) Document() DocumentRepo                       { return s.support.Documents }
func (s *GormStore) Inbox() InboxRepo                             { return s.platform.Inbox }
func (s *GormStore) Outbox() OutboxRepo                           { return s.platform.Outbox }
func (s *GormStore) Log() LogRepo                                 { return s.platform.Logs }
func (s *GormStore) Node() NodeRepo                               { return s.network.Nodes }
func (s *GormStore) Order() OrderRepo                             { return s.billing.Orders }
func (s *GormStore) OrderEvent() OrderEventRepo                   { return s.billing.OrderEvents }
func (s *GormStore) Payment() PaymentRepo                         { return s.billing.Payments }
func (s *GormStore) Subscribe() SubscribeRepo                     { return s.subscription.Plans }
func (s *GormStore) Entitlement() EntitlementRepo                 { return s.subscription.Entitlements }
func (s *GormStore) System() SystemRepo                           { return s.platform.System }
func (s *GormStore) Task() TaskRepo                               { return s.platform.Tasks }
func (s *GormStore) TelegramTopic() TelegramTopicRepo             { return s.notification.TelegramTopics }
func (s *GormStore) Ticket() TicketRepo                           { return s.support.Tickets }
func (s *GormStore) TrafficLog() TrafficRepo                      { return s.network.Traffic }
func (s *GormStore) User() UserRepo                               { return s.identity.Users }
func (s *GormStore) UserAuth() UserAuthRepo                       { return s.identity.UserAuths }
func (s *GormStore) UserSubscription() UserSubscriptionRepo       { return s.subscription.UserSubs }
func (s *GormStore) UserDevice() UserDeviceRepo                   { return s.identity.Devices }
func (s *GormStore) UserWithdrawal() UserWithdrawalRepo           { return s.billing.Withdrawals }
func (s *GormStore) SubscriptionTraffic() SubscriptionTrafficRepo { return s.subscription.Traffic }
func (s *GormStore) UserCache() UserCacheRepo                     { return s.identity.UserCache }

// InTx runs fn within a database transaction. A new GormStore backed by the
// transaction is passed to fn, so all repository operations inside fn share
// the same transaction.
func (s *GormStore) InTx(ctx context.Context, fn func(store Store) error) error {
	invalidations := s.invalidations
	owner := invalidations == nil
	if owner {
		invalidations = cache.NewInvalidationQueue()
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(newGormStore(tx, s.redis, invalidations, s.retrier, s.builders))
	})
	if err != nil || !owner {
		return err
	}
	s.flushInvalidations(ctx, invalidations)
	return nil
}

func (s *GormStore) flushInvalidations(ctx context.Context, invalidations *cache.InvalidationQueue) {
	flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	err := invalidations.Flush(flushCtx, s.redis)
	cancel()
	if err == nil {
		return
	}
	logger.Errorf("cache invalidation after transaction commit failed; queued for retry: %v", err)
	s.retrier.Enqueue(invalidations.Keys()...)
}
