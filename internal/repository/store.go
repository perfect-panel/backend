package repository

import (
	"context"

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
	Log() LogRepo
	Node() NodeRepo
	Order() OrderRepo
	Payment() PaymentRepo
	Subscribe() SubscribeRepo
	System() SystemRepo
	Task() TaskRepo
	Ticket() TicketRepo
	TrafficLog() TrafficRepo
	User() UserRepo

	// DB returns the underlying *gorm.DB, used internally by plugins etc.
	DB() *gorm.DB

	InTx(ctx context.Context, fn func(store Store) error) error
}

var _ Store = (*GormStore)(nil)

// GormStore is the Store implementation backed by GORM + Redis.
type GormStore struct {
	db    *gorm.DB
	redis *redis.Client

	ads          AdsRepo
	announcement AnnouncementRepo
	auth         AuthRepo
	client       ClientRepo
	coupon       CouponRepo
	document     DocumentRepo
	log          LogRepo
	node         NodeRepo
	order        OrderRepo
	payment      PaymentRepo
	subscribe    SubscribeRepo
	system       SystemRepo
	task         TaskRepo
	ticket       TicketRepo
	trafficLog   TrafficRepo
	user         UserRepo
}

// DB returns the underlying *gorm.DB (used internally by plugins etc.).
func (s *GormStore) DB() *gorm.DB { return s.db }

// NewGormStore creates a new GormStore with all domain repositories initialized.
func NewGormStore(db *gorm.DB, rds *redis.Client) *GormStore {
	return &GormStore{
		db:           db,
		redis:        rds,
		ads:          newAdsRepo(db, rds),
		announcement: newAnnouncementRepo(db, rds),
		auth:         newAuthRepo(db, rds),
		client:       newClientRepo(db),
		coupon:       newCouponRepo(db, rds),
		document:     newDocumentRepo(db, rds),
		log:          newLogRepo(db),
		node:         newNodeRepo(db, rds),
		order:        newOrderRepo(db, rds),
		payment:      newPaymentRepo(db, rds),
		subscribe:    newSubscribeRepo(db, rds),
		system:       newSystemRepo(db, rds),
		task:         newTaskRepo(db),
		ticket:       newTicketRepo(db, rds),
		trafficLog:   newTrafficRepo(db),
		user:         newUserRepo(db, rds),
	}
}

func (s *GormStore) Ads() AdsRepo                   { return s.ads }
func (s *GormStore) Announcement() AnnouncementRepo { return s.announcement }
func (s *GormStore) Auth() AuthRepo                 { return s.auth }
func (s *GormStore) Client() ClientRepo             { return s.client }
func (s *GormStore) Coupon() CouponRepo             { return s.coupon }
func (s *GormStore) Document() DocumentRepo         { return s.document }
func (s *GormStore) Log() LogRepo                   { return s.log }
func (s *GormStore) Node() NodeRepo                 { return s.node }
func (s *GormStore) Order() OrderRepo               { return s.order }
func (s *GormStore) Payment() PaymentRepo           { return s.payment }
func (s *GormStore) Subscribe() SubscribeRepo       { return s.subscribe }
func (s *GormStore) System() SystemRepo             { return s.system }
func (s *GormStore) Task() TaskRepo                 { return s.task }
func (s *GormStore) Ticket() TicketRepo             { return s.ticket }
func (s *GormStore) TrafficLog() TrafficRepo        { return s.trafficLog }
func (s *GormStore) User() UserRepo                 { return s.user }

// InTx runs fn within a database transaction. A new GormStore backed by the
// transaction is passed to fn, so all repository operations inside fn share
// the same transaction.
func (s *GormStore) InTx(ctx context.Context, fn func(store Store) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(NewGormStore(tx, s.redis))
	})
}
