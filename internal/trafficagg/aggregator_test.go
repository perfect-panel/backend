package trafficagg

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/redis/go-redis/v9"
)

func newTestAggregator(t *testing.T) (*Aggregator, *redis.Client) {
	t.Helper()
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })

	return New(&svc.ServiceContext{Redis: redisClient}), redisClient
}

func TestHandleBucketFlushFailureKeepsProcessingBeforeThreshold(t *testing.T) {
	aggregator, redisClient := newTestAggregator(t)
	ctx := context.Background()
	suffix := "202607200640"
	processingKey := processingBucketPrefix + suffix
	cause := errors.New("database unavailable")

	if err := redisClient.HSet(ctx, processingKey, trafficField(1, 2, trafficFieldDownload), "99").Err(); err != nil {
		t.Fatalf("seed processing bucket: %v", err)
	}
	if err := redisClient.ZAdd(ctx, bucketIndexKey, redis.Z{Score: bucketScore(suffix), Member: suffix}).Err(); err != nil {
		t.Fatalf("seed bucket index: %v", err)
	}
	if err := redisClient.Set(ctx, bucketFailureKey(suffix), bucketFailureThreshold-2, 0).Err(); err != nil {
		t.Fatalf("seed failure count: %v", err)
	}

	err := aggregator.handleBucketFlushFailure(ctx, suffix, processingKey, cause)
	if !errors.Is(err, cause) {
		t.Fatalf("handleBucketFlushFailure error = %v, want original cause", err)
	}

	failures, err := redisClient.Get(ctx, bucketFailureKey(suffix)).Int()
	if err != nil {
		t.Fatalf("get failure count: %v", err)
	}
	if failures != bucketFailureThreshold-1 {
		t.Fatalf("failure count = %d, want %d", failures, bucketFailureThreshold-1)
	}
	if exists, err := redisClient.Exists(ctx, processingKey).Result(); err != nil || exists != 1 {
		t.Fatalf("processing bucket exists = %d, err = %v; want exists", exists, err)
	}
	if _, err := redisClient.ZScore(ctx, bucketIndexKey, suffix).Result(); err != nil {
		t.Fatalf("bucket index should still contain suffix: %v", err)
	}
	if exists, err := redisClient.Exists(ctx, deadLetterBucketKey(suffix)).Result(); err != nil || exists != 0 {
		t.Fatalf("deadletter exists = %d, err = %v; want missing", exists, err)
	}
}

func TestHandleBucketFlushFailureMovesProcessingToDeadLetterAtThreshold(t *testing.T) {
	aggregator, redisClient := newTestAggregator(t)
	ctx := context.Background()
	suffix := "202607200640"
	processingKey := processingBucketPrefix + suffix
	deadLetterKey := deadLetterBucketKey(suffix)
	cause := errors.New("failed to encode int4")

	if err := redisClient.HSet(ctx, processingKey, map[string]interface{}{
		trafficField(1, 2, trafficFieldDownload): "99",
		trafficField(1, 2, trafficFieldUpload):   "7",
	}).Err(); err != nil {
		t.Fatalf("seed processing bucket: %v", err)
	}
	if err := redisClient.ZAdd(ctx, bucketIndexKey, redis.Z{Score: bucketScore(suffix), Member: suffix}).Err(); err != nil {
		t.Fatalf("seed bucket index: %v", err)
	}
	if err := redisClient.Set(ctx, bucketFailureKey(suffix), bucketFailureThreshold-1, 0).Err(); err != nil {
		t.Fatalf("seed failure count: %v", err)
	}

	if err := aggregator.handleBucketFlushFailure(ctx, suffix, processingKey, cause); err != nil {
		t.Fatalf("handleBucketFlushFailure: %v", err)
	}

	if exists, err := redisClient.Exists(ctx, processingKey).Result(); err != nil || exists != 0 {
		t.Fatalf("processing bucket exists = %d, err = %v; want missing", exists, err)
	}
	values, err := redisClient.HGetAll(ctx, deadLetterKey).Result()
	if err != nil {
		t.Fatalf("get deadletter bucket: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("deadletter field count = %d, want 2: %#v", len(values), values)
	}
	if got := values[trafficField(1, 2, trafficFieldDownload)]; got != "99" {
		t.Fatalf("deadletter download = %q, want 99", got)
	}
	if _, err := redisClient.ZScore(ctx, bucketIndexKey, suffix).Result(); !errors.Is(err, redis.Nil) {
		t.Fatalf("bucket index lookup error = %v, want redis.Nil", err)
	}
	if _, err := redisClient.ZScore(ctx, deadLetterIndexKey, deadLetterKey).Result(); err != nil {
		t.Fatalf("deadletter index should contain key: %v", err)
	}
	if exists, err := redisClient.Exists(ctx, bucketFailureKey(suffix)).Result(); err != nil || exists != 0 {
		t.Fatalf("failure key exists = %d, err = %v; want missing", exists, err)
	}

	meta, err := redisClient.HGetAll(ctx, deadLetterMetaKey(deadLetterKey)).Result()
	if err != nil {
		t.Fatalf("get deadletter meta: %v", err)
	}
	if got := meta["last_error"]; got != cause.Error() {
		t.Fatalf("meta last_error = %q, want %q", got, cause.Error())
	}
	if got := meta["failure_count"]; got != strconv.Itoa(bucketFailureThreshold) {
		t.Fatalf("meta failure_count = %q, want %d", got, bucketFailureThreshold)
	}
	if got := meta["field_count"]; got != "2" {
		t.Fatalf("meta field_count = %q, want 2", got)
	}
}

func TestMarkBucketProcessedAndCleanupClearsFailureCounter(t *testing.T) {
	aggregator, redisClient := newTestAggregator(t)
	ctx := context.Background()
	suffix := "202607200640"
	processingKey := processingBucketPrefix + suffix
	processedKey := processedBucketPrefix + suffix

	if err := redisClient.HSet(ctx, processingKey, trafficField(1, 2, trafficFieldDownload), "99").Err(); err != nil {
		t.Fatalf("seed processing bucket: %v", err)
	}
	if err := redisClient.ZAdd(ctx, bucketIndexKey, redis.Z{Score: bucketScore(suffix), Member: suffix}).Err(); err != nil {
		t.Fatalf("seed bucket index: %v", err)
	}
	if err := redisClient.Set(ctx, bucketFailureKey(suffix), 3, 0).Err(); err != nil {
		t.Fatalf("seed failure count: %v", err)
	}

	if err := aggregator.markBucketProcessedAndCleanup(ctx, suffix, processingKey, processedKey); err != nil {
		t.Fatalf("markBucketProcessedAndCleanup: %v", err)
	}

	if exists, err := redisClient.Exists(ctx, processingKey).Result(); err != nil || exists != 0 {
		t.Fatalf("processing bucket exists = %d, err = %v; want missing", exists, err)
	}
	if exists, err := redisClient.Exists(ctx, processedKey).Result(); err != nil || exists != 1 {
		t.Fatalf("processed marker exists = %d, err = %v; want exists", exists, err)
	}
	if exists, err := redisClient.Exists(ctx, bucketFailureKey(suffix)).Result(); err != nil || exists != 0 {
		t.Fatalf("failure key exists = %d, err = %v; want missing", exists, err)
	}
	if _, err := redisClient.ZScore(ctx, bucketIndexKey, suffix).Result(); !errors.Is(err, redis.Nil) {
		t.Fatalf("bucket index lookup error = %v, want redis.Nil", err)
	}
}
