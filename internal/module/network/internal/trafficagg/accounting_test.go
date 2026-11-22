package trafficagg

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	trafficEntity "github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/platform/entity/inbox"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
)

var errAccounting = errors.New("accounting write failed")

// usageBucketConsumer is the subscription half's persisted inbox consumer.
const usageBucketConsumer = "subscription.traffic_bucket"

// accountingStore backs both domain halves of a bucket flush: the network
// store with its traffic-log transaction, and the subscription usage with
// its own transaction. fail names the write that fails: "usage", "logs" or
// the consumer whose inbox marker cannot be written.
type accountingStore struct {
	marks            map[string]bool
	upload, download int64
	logs             int
	fail             string
	// applies counts the ApplyBucketOnce calls that charged a bucket;
	// applyHook, when set, runs inside ApplyBucketOnce before the charge,
	// while a flush is between taking its bucket over and cleaning it up.
	applies   int
	applyHook func()
	// reportTimes counts the server report times persisted.
	reportTimes int
}

var (
	_ Store                     = (*accountingStore)(nil)
	_ TrafficLogTx              = (*accountingStore)(nil)
	_ SubscriptionReader        = (*accountingStore)(nil)
	_ subscription.TrafficUsage = (*accountingStore)(nil)
)

func (s *accountingStore) BatchUpdateServerLastReportedAt(_ context.Context, reports map[int64]time.Time) error {
	s.reportTimes += len(reports)
	return nil
}

func (s *accountingStore) FindInboxRecord(_ context.Context, consumer, key string) (*inbox.Record, error) {
	if s.marks[consumer+"|"+key] {
		return &inbox.Record{Consumer: consumer, EventKey: key}, nil
	}
	return nil, nil
}

// InTrafficLogTx models the network transaction's rollback, including an
// inbox failure after the log write.
func (s *accountingStore) InTrafficLogTx(_ context.Context, fn func(TrafficLogTx) error) error {
	beforeLogs, beforeMarks := s.logs, maps.Clone(s.marks)
	if err := fn(s); err != nil {
		s.logs, s.marks = beforeLogs, beforeMarks
		return err
	}
	return nil
}

func (s *accountingStore) InsertTrafficLogs(_ context.Context, logs []*trafficEntity.TrafficLog, _ int) error {
	if s.fail == "logs" {
		return errAccounting
	}
	s.logs += len(logs)
	return nil
}

func (s *accountingStore) InsertInboxRecord(_ context.Context, consumer, key string) error {
	if s.fail == consumer {
		return errAccounting
	}
	if s.marks[consumer+"|"+key] {
		return errors.New("duplicate inbox record")
	}
	s.marks[consumer+"|"+key] = true
	return nil
}

func (s *accountingStore) SubscriptionsByIDs(context.Context, []int64) ([]*usersub.Subscribe, error) {
	return []*usersub.Subscribe{{Id: 2, UserId: 7}}, nil
}

// ApplyBucketOnce models the subscription half: the usage and its inbox
// marker commit together or not at all, and a committed bucket is skipped.
// The two domain transactions deliberately commit independently.
func (s *accountingStore) ApplyBucketOnce(_ context.Context, bucket string, deltas []trafficEntity.SubscribeTrafficDelta) error {
	if s.marks[usageBucketConsumer+"|"+bucket] {
		return nil
	}
	if s.applyHook != nil {
		s.applyHook()
	}
	if s.fail == "usage" || s.fail == usageBucketConsumer {
		return errAccounting
	}
	s.applies++
	for _, delta := range deltas {
		s.upload += delta.Upload
		s.download += delta.Download
	}
	s.marks[usageBucketConsumer+"|"+bucket] = true
	return nil
}

func TestTrafficAccountingRetryAcrossDomainCommits(t *testing.T) {
	const bucket = "202609050900"
	for _, failure := range []string{"usage", usageBucketConsumer, "logs", networkTrafficBucketConsumer} {
		t.Run(failure, func(t *testing.T) {
			store := &accountingStore{marks: map[string]bool{}, fail: failure}
			aggregator := New(Deps{Store: store, Subscriptions: store, Usage: store})
			deltas := []trafficDelta{{ServerId: 1, SubscribeId: 2, Upload: 3, Download: 5}}
			if err := aggregator.persistBucket(context.Background(), bucket, deltas); !errors.Is(err, errAccounting) {
				t.Fatalf("first attempt: %v", err)
			}
			usageCommitted := failure == "logs" || failure == networkTrafficBucketConsumer
			if usageCommitted {
				if store.upload != 3 || store.download != 5 || !store.marks[usageBucketConsumer+"|"+bucket] {
					t.Fatal("network failure lost the committed subscription transaction")
				}
			} else if store.upload != 0 || store.download != 0 || len(store.marks) != 0 {
				t.Fatal("failed subscription transaction was not rolled back")
			}
			if store.logs != 0 || store.marks[networkTrafficBucketConsumer+"|"+bucket] {
				t.Fatal("failed pipeline committed network logs")
			}
			store.fail = ""
			for range 2 {
				if err := aggregator.persistBucket(context.Background(), bucket, deltas); err != nil {
					t.Fatalf("retry: %v", err)
				}
			}
			if store.upload != 3 || store.download != 5 || store.logs != 1 || len(store.marks) != 2 {
				t.Fatalf("replay duplicated accounting: upload=%d download=%d logs=%d marks=%v", store.upload, store.download, store.logs, store.marks)
			}
		})
	}
}
