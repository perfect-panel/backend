package trafficagg

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/redis/go-redis/v9"
)

var reportServer = &node.Server{Id: 4, Protocols: `[{"type":"vless","ratio":1}]`}

func newReportAggregator(t *testing.T, served func(context.Context, int64, string) (map[int64]struct{}, error)) (*Aggregator, *redis.Client) {
	t.Helper()
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	return New(Deps{Redis: redisClient, ServedSubscriptions: served}), redisClient
}

func reportBucket(t *testing.T, client *redis.Client, now time.Time) map[string]string {
	t.Helper()
	values, err := client.HGetAll(context.Background(), bucketPrefix+bucketSuffix(bucketMinute(now))).Result()
	if err != nil {
		t.Fatalf("read bucket: %v", err)
	}
	return values
}

// A node report is applied entry by entry: negative, oversized and foreign
// entries are dropped while the valid entries of the same report still count.
func TestAddReportDropsInvalidAndUnservedEntries(t *testing.T) {
	scopeCalls := 0
	aggregator, client := newReportAggregator(t, func(_ context.Context, serverID int64, protocol string) (map[int64]struct{}, error) {
		scopeCalls++
		if serverID != reportServer.Id || protocol != "vless" {
			t.Fatalf("scope requested for server %d/%s", serverID, protocol)
		}
		return map[int64]struct{}{2: {}, 3: {}, 5: {}, 6: {}}, nil
	})
	now := time.Now()
	err := aggregator.AddReportAt(context.Background(), reportServer, "vless", []UserTraffic{
		{SID: 2, Upload: 5, Download: 7},
		{SID: 3, Upload: -100, Download: 0},                                        // would refund usage
		{SID: 3, Upload: 1, Download: -1},                                          // negative component
		{SID: 5, Upload: MaxReportedTraffic + 1},                                   // beyond the bound
		{SID: 5, Upload: math.MaxInt64, Download: math.MaxInt64},                   // overflows the SQL increment
		{SID: 99, Upload: 1, Download: 1},                                          // not served by this server
		{SID: 2, Upload: MaxReportedTraffic, Download: 0},                          // repeats past the per-report bound
		{SID: 6, Upload: MaxReportedTraffic / 2, Download: MaxReportedTraffic / 2}, // at the bound
	}, now)
	if err != nil {
		t.Fatalf("AddReportAt: %v", err)
	}
	if scopeCalls != 1 {
		t.Fatalf("served scope resolved %d times, want once per report", scopeCalls)
	}
	got := reportBucket(t, client, now)
	want := map[string]string{
		trafficField(4, 2, trafficFieldUpload):   "5",
		trafficField(4, 2, trafficFieldDownload): "7",
		trafficField(4, 6, trafficFieldUpload):   strconv.FormatInt(MaxReportedTraffic/2, 10),
		trafficField(4, 6, trafficFieldDownload): strconv.FormatInt(MaxReportedTraffic/2, 10),
	}
	if len(got) != len(want) {
		t.Fatalf("bucket = %v, want %v", got, want)
	}
	for field, value := range want {
		if got[field] != value {
			t.Fatalf("bucket field %s = %q, want %q (bucket %v)", field, got[field], value, got)
		}
	}
	if reported, err := client.HGet(context.Background(), serverLastReportedKey, "4").Result(); err != nil || reported == "" {
		t.Fatalf("heartbeat not recorded: %q, %v", reported, err)
	}
}

// The scope lookup runs only for entries that would be applied, and a failed
// lookup applies nothing but still records the heartbeat.
func TestAddReportScopeFailureAppliesNothing(t *testing.T) {
	scopeErr := errors.New("user list unavailable")
	aggregator, client := newReportAggregator(t, func(context.Context, int64, string) (map[int64]struct{}, error) {
		return nil, scopeErr
	})
	now := time.Now()
	if err := aggregator.AddReportAt(context.Background(), reportServer, "vless", []UserTraffic{{SID: 2, Upload: 5, Download: 7}}, now); !errors.Is(err, scopeErr) {
		t.Fatalf("AddReportAt error = %v, want %v", err, scopeErr)
	}
	if got := reportBucket(t, client, now); len(got) != 0 {
		t.Fatalf("unscoped traffic applied: %v", got)
	}
	if reported, err := client.HGet(context.Background(), serverLastReportedKey, "4").Result(); err != nil || reported == "" {
		t.Fatalf("heartbeat not recorded: %q, %v", reported, err)
	}

	skipped, _ := newReportAggregator(t, func(context.Context, int64, string) (map[int64]struct{}, error) {
		t.Fatal("scope resolved for a report with nothing to apply")
		return nil, nil
	})
	if err := skipped.AddReportAt(context.Background(), reportServer, "vless", []UserTraffic{{SID: 2}, {SID: 3, Upload: -1}}, now); err != nil {
		t.Fatalf("AddReportAt: %v", err)
	}
}
