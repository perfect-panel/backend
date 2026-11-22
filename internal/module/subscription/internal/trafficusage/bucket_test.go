package trafficusage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/subtest"
)

// A bucket charges its usage once: a transaction that fails to commit leaves
// neither the usage nor the marker, and replays of a committed bucket are
// skipped.
func TestApplyBucketOnceChargesABucketOnce(t *testing.T) {
	f := subtest.New(t)
	sub := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, Status: usersub.SubscribeStatusActive, ExpireTime: time.Now().Add(24 * time.Hour)})
	usage := New(f.Store)
	ctx := context.Background()
	const bucket = "202609050900"
	deltas := []traffic.SubscribeTrafficDelta{{SubscribeId: sub.Id, Upload: 3, Download: 5}}

	f.Store.FailNextCommits(1)
	if err := usage.ApplyBucketOnce(ctx, bucket, deltas); !errors.Is(err, subtest.ErrInjectedRollback) {
		t.Fatalf("failed commit: %v", err)
	}
	if got := f.Load(t, sub.Id); got.Upload != 0 || got.Download != 0 {
		t.Fatalf("rolled-back bucket charged %d/%d", got.Upload, got.Download)
	}
	if mark, err := f.Store.Inbox().Find(ctx, BucketConsumer, bucket); err != nil || mark != nil {
		t.Fatalf("rolled-back bucket marker = %v, %v", mark, err)
	}
	for range 2 {
		if err := usage.ApplyBucketOnce(ctx, bucket, deltas); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	if got := f.Load(t, sub.Id); got.Upload != 3 || got.Download != 5 {
		t.Fatalf("replayed bucket charged %d/%d, want 3/5", got.Upload, got.Download)
	}
	if mark, err := f.Store.Inbox().Find(ctx, BucketConsumer, bucket); err != nil || mark == nil {
		t.Fatalf("committed bucket marker = %v, %v", mark, err)
	}
}
