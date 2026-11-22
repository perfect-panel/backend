// Package trafficusage owns idempotent subscription usage accounting.
package trafficusage

import (
	"context"
	"strings"

	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/platform/entity/inbox"
	"github.com/perfect-panel/backend/internal/repository"
)

// BucketConsumer is persisted in the inbox; changing it replays old usage.
const BucketConsumer = "subscription.traffic_bucket"

type Store interface {
	Inbox() repository.InboxRepo
	InSubscriptionTx(context.Context, func(repository.SubscriptionStore) error) error
}

type Service struct {
	store Store
}

func New(store Store) *Service { return &Service{store: store} }

// ApplyBucketOnce commits usage and its inbox marker together. Network log
// retries can replay this operation without charging usage again.
func (s *Service) ApplyBucketOnce(ctx context.Context, bucket string, deltas []traffic.SubscribeTrafficDelta) error {
	store := s.store
	if key, identified := strings.CutPrefix(bucket, inbox.NodeTrafficReportPrefix); identified {
		return store.InSubscriptionTx(ctx, func(tx repository.SubscriptionStore) error {
			claimed, err := tx.Inbox().ClaimTrafficReportUsage(ctx, key)
			if err != nil || !claimed {
				return err
			}
			return tx.UserSubscription().BatchUpdateUserSubscribeWithTraffic(ctx, deltas)
		})
	}
	mark, err := store.Inbox().Find(ctx, BucketConsumer, bucket)
	if err != nil || mark != nil {
		return err
	}
	return store.InSubscriptionTx(ctx, func(tx repository.SubscriptionStore) error {
		if err := tx.UserSubscription().BatchUpdateUserSubscribeWithTraffic(ctx, deltas); err != nil {
			return err
		}
		return tx.Inbox().Insert(ctx, BucketConsumer, bucket, "")
	})
}
