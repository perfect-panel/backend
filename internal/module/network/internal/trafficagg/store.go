package trafficagg

import (
	"context"
	"time"

	trafficEntity "github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/platform/entity/inbox"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
)

// Store is the network persistence of the pipeline: the servers' report
// times, and the traffic log with the bucket markers of the idempotent
// inbox. NewStore adapts the application store.
type Store interface {
	// BatchUpdateServerLastReportedAt persists the servers' latest reports.
	BatchUpdateServerLastReportedAt(ctx context.Context, reports map[int64]time.Time) error
	// FindInboxRecord returns the inbox marker of a committed step, or nil
	// when the step has not committed yet.
	FindInboxRecord(ctx context.Context, consumer, eventKey string) (*inbox.Record, error)
	// InTrafficLogTx runs fn in a network transaction.
	InTrafficLogTx(ctx context.Context, fn func(TrafficLogTx) error) error
}

// TrafficLogTx is what a bucket flush writes in its network transaction:
// the traffic log rows and the bucket's inbox marker.
type TrafficLogTx interface {
	FinishTrafficReport(context.Context, string) (bool, error)
	// InsertTrafficLogs inserts the rows in batches of batchSize.
	InsertTrafficLogs(ctx context.Context, logs []*trafficEntity.TrafficLog, batchSize int) error
	// InsertInboxRecord marks the step processed. A second marker for the
	// same step fails, rolling the transaction back.
	InsertInboxRecord(ctx context.Context, consumer, eventKey string) error
}

// SubscriptionReader is the subscription read port (the subscription
// facade): the subscriptions a bucket charges, for their owners.
type SubscriptionReader interface {
	SubscriptionsByIDs(ctx context.Context, ids []int64) ([]*usersub.Subscribe, error)
}

// AppStore is the part of the application store NewStore adapts.
type AppStore interface {
	Node() repository.NodeRepo
	Inbox() repository.InboxRepo
	repository.NetworkTransactor
}

// NewStore adapts the application store to the pipeline's Store.
func NewStore(store AppStore) Store {
	return appStore{store: store}
}

type appStore struct {
	store AppStore
}

func (s appStore) BatchUpdateServerLastReportedAt(ctx context.Context, reports map[int64]time.Time) error {
	return s.store.Node().BatchUpdateServerLastReportedAt(ctx, reports)
}

func (s appStore) FindInboxRecord(ctx context.Context, consumer, eventKey string) (*inbox.Record, error) {
	return s.store.Inbox().Find(ctx, consumer, eventKey)
}

func (s appStore) InTrafficLogTx(ctx context.Context, fn func(TrafficLogTx) error) error {
	return s.store.InNetworkTx(ctx, func(tx repository.NetworkStore) error {
		return fn(trafficLogTx{store: tx})
	})
}

// trafficLogTx is the traffic log over a network-domain transaction.
type trafficLogTx struct {
	store repository.NetworkStore
}

func (t trafficLogTx) InsertTrafficLogs(ctx context.Context, logs []*trafficEntity.TrafficLog, batchSize int) error {
	return t.store.TrafficLog().InsertBatch(ctx, logs, batchSize)
}

func (t trafficLogTx) InsertInboxRecord(ctx context.Context, consumer, eventKey string) error {
	return t.store.Inbox().Insert(ctx, consumer, eventKey, "")
}

func (t trafficLogTx) FinishTrafficReport(ctx context.Context, key string) (bool, error) {
	return t.store.Inbox().FinishTrafficReport(ctx, key)
}
