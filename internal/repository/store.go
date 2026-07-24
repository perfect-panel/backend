package repository

import (
	"context"
	"time"

	"github.com/perfect-panel/server/pkg/cache"
	"github.com/perfect-panel/server/pkg/logger"
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
	Log() LogRepo
	Node() NodeRepo
	Order() OrderRepo
	OrderEvent() OrderEventRepo
	Payment() PaymentRepo
	Subscribe() SubscribeRepo
	System() SystemRepo
	Task() TaskRepo
	Ticket() TicketRepo
	TrafficLog() TrafficRepo
	User() UserRepo
	UserAuth() UserAuthRepo
	UserSubscription() UserSubscriptionRepo
	UserDevice() UserDeviceRepo
	UserWithdrawal() UserWithdrawalRepo
	SubscriptionTraffic() SubscriptionTrafficRepo
	UserCache() UserCacheRepo

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

// GormStore is the Store implementation backed by GORM + Redis.
type GormStore struct {
	db            *gorm.DB
	redis         *redis.Client
	invalidations *cache.InvalidationQueue
	retrier       *cache.InvalidationRetrier
	nodeRetrier   *serverCacheInvalidationRetrier

	ads          AdsRepo
	announcement AnnouncementRepo
	auth         AuthRepo
	client       ClientRepo
	coupon       CouponRepo
	document     DocumentRepo
	inbox        InboxRepo
	log          LogRepo
	node         NodeRepo
	order        OrderRepo
	orderEvent   OrderEventRepo
	payment      PaymentRepo
	subscribe    SubscribeRepo
	system       SystemRepo
	task         TaskRepo
	ticket       TicketRepo
	trafficLog   TrafficRepo
	user         *userRepo
}

// NewGormStore creates a new GormStore with all domain repositories initialized.
func NewGormStore(db *gorm.DB, rds *redis.Client) *GormStore {
	return newGormStore(db, rds, nil, cache.NewInvalidationRetrier(rds), newServerCacheInvalidationRetrier(rds))
}

func newGormStore(db *gorm.DB, rds *redis.Client, invalidations *cache.InvalidationQueue, retrier *cache.InvalidationRetrier, nodeRetrier *serverCacheInvalidationRetrier) *GormStore {
	return &GormStore{
		db:            db,
		redis:         rds,
		invalidations: invalidations,
		retrier:       retrier,
		nodeRetrier:   nodeRetrier,
		ads:           newAdsRepo(db, rds, invalidations),
		announcement:  newAnnouncementRepo(db, rds, invalidations),
		auth:          newAuthRepo(db, rds, invalidations),
		client:        newClientRepo(db),
		coupon:        newCouponRepo(db, rds, invalidations),
		document:      newDocumentRepo(db, rds, invalidations),
		inbox:         newInboxRepo(db),
		log:           newLogRepo(db),
		node:          newNodeRepo(db, rds, nodeRetrier),
		order:         newOrderRepo(db, rds, invalidations),
		orderEvent:    newOrderEventRepo(db),
		payment:       newPaymentRepo(db, rds, invalidations),
		subscribe:     newSubscribeRepo(db, rds, invalidations),
		system:        newSystemRepo(db, rds, invalidations),
		task:          newTaskRepo(db),
		ticket:        newTicketRepo(db, rds, invalidations),
		trafficLog:    newTrafficRepo(db),
		user:          newUserRepo(db, rds, invalidations),
	}
}

func newCachedConn(db *gorm.DB, rds *redis.Client, invalidations ...*cache.InvalidationQueue) cache.CachedConn {
	if len(invalidations) > 0 && invalidations[0] != nil {
		return cache.NewConn(db, rds, cache.WithInvalidationQueue(invalidations[0]))
	}
	return cache.NewConn(db, rds)
}

func (s *GormStore) Ads() AdsRepo                   { return s.ads }
func (s *GormStore) Announcement() AnnouncementRepo { return s.announcement }
func (s *GormStore) Auth() AuthRepo                 { return s.auth }
func (s *GormStore) Client() ClientRepo             { return s.client }
func (s *GormStore) Coupon() CouponRepo             { return s.coupon }
func (s *GormStore) Document() DocumentRepo         { return s.document }
func (s *GormStore) Inbox() InboxRepo               { return s.inbox }
func (s *GormStore) Log() LogRepo                   { return s.log }
func (s *GormStore) Node() NodeRepo                 { return s.node }
func (s *GormStore) Order() OrderRepo               { return s.order }
func (s *GormStore) OrderEvent() OrderEventRepo     { return s.orderEvent }
func (s *GormStore) Payment() PaymentRepo           { return s.payment }
func (s *GormStore) Subscribe() SubscribeRepo       { return s.subscribe }
func (s *GormStore) System() SystemRepo             { return s.system }
func (s *GormStore) Task() TaskRepo                 { return s.task }
func (s *GormStore) Ticket() TicketRepo             { return s.ticket }
func (s *GormStore) TrafficLog() TrafficRepo        { return s.trafficLog }
func (s *GormStore) User() UserRepo                 { return s.user }
func (s *GormStore) UserAuth() UserAuthRepo         { return s.user }
func (s *GormStore) UserSubscription() UserSubscriptionRepo {
	return s.user
}
func (s *GormStore) UserDevice() UserDeviceRepo                   { return s.user }
func (s *GormStore) UserWithdrawal() UserWithdrawalRepo           { return s.user }
func (s *GormStore) SubscriptionTraffic() SubscriptionTrafficRepo { return s.user }
func (s *GormStore) UserCache() UserCacheRepo                     { return s.user }

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
		return fn(newGormStore(tx, s.redis, invalidations, s.retrier, s.nodeRetrier))
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
