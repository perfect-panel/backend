package trafficagg

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/infra/rediskeys"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/redis/go-redis/v9"
)

// newLifecycleAggregator returns a pipeline over miniredis and an
// accounting store, for flushes end to end. The store knows subscription 2
// (owner 7), so the tests report for that subscription.
func newLifecycleAggregator(t *testing.T) (*Aggregator, *accountingStore, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := &accountingStore{marks: map[string]bool{}}
	return New(Deps{Redis: client, Store: store, Subscriptions: store, Usage: store}), store, client
}

// useZone runs the test in the named app zone.
func useZone(t *testing.T, name string) {
	t.Helper()
	previous := timeutil.LocationName()
	if err := timeutil.LoadLocation(name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = timeutil.LoadLocation(previous) })
}

func report(t *testing.T, aggregator *Aggregator, now time.Time, upload, download int64) {
	t.Helper()
	if err := aggregator.AddReportAt(context.Background(), reportServer, "vless", []UserTraffic{{SID: 2, Upload: upload, Download: download}}, now); err != nil {
		t.Fatalf("AddReportAt: %v", err)
	}
}

func aggregateKeys(t *testing.T, client *redis.Client) []string {
	t.Helper()
	keys, err := client.Keys(context.Background(), bucketPrefix+"[0-9]*").Result()
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func indexed(t *testing.T, client *redis.Client) int64 {
	t.Helper()
	n, err := client.ZCard(context.Background(), bucketIndexKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// When daylight-saving time ends, the app zone's wall clock repeats an hour:
// two minutes an hour apart share a wall-clock name. Keyed by the Unix
// minute, they are two buckets, and both are billed.
func TestBucketsAreDistinctAcrossTheRepeatedDSTHour(t *testing.T) {
	useZone(t, "America/New_York")
	aggregator, store, client := newLifecycleAggregator(t)
	// 2026-11-01 01:30 EDT and 01:30 EST, an hour apart.
	first := time.Date(2026, 11, 1, 5, 30, 20, 0, time.UTC)
	second := first.Add(time.Hour)
	if first.In(timeutil.Location()).Format(legacyBucketLayout) != second.In(timeutil.Location()).Format(legacyBucketLayout) {
		t.Fatal("the two report times must share a wall-clock minute for this test to mean anything")
	}

	report(t, aggregator, first, 5, 7)
	report(t, aggregator, second, 11, 13)
	if keys := aggregateKeys(t, client); len(keys) != 2 {
		t.Fatalf("aggregate buckets = %v, want one per minute", keys)
	}
	if err := aggregator.FlushDueBuckets(context.Background(), second.Add(2*time.Minute)); err != nil {
		t.Fatalf("FlushDueBuckets: %v", err)
	}
	if store.upload != 16 || store.download != 20 || store.logs != 2 {
		t.Fatalf("billed %d/%d in %d logs, want 16/20 in 2", store.upload, store.download, store.logs)
	}
	if n := indexed(t, client); n != 0 {
		t.Fatalf("%d buckets left in the index", n)
	}
}

// A report of a minute that was already flushed re-creates the minute's
// aggregate. It is flushed under a fresh suffix on the next tick instead of
// being dropped as the leftovers of a processed bucket.
func TestFlushReissuesAReportThatLandsAfterItsMinuteWasFlushed(t *testing.T) {
	aggregator, store, client := newLifecycleAggregator(t)
	now := time.Date(2026, 9, 28, 12, 0, 30, 0, time.UTC)
	suffix := bucketSuffix(bucketMinute(now))

	report(t, aggregator, now, 5, 7)
	if err := aggregator.FlushDueBuckets(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatalf("first flush: %v", err)
	}
	if store.upload != 5 || store.download != 7 {
		t.Fatalf("first flush billed %d/%d, want 5/7", store.upload, store.download)
	}

	// The straggler: its write lands after the minute was flushed.
	report(t, aggregator, now, 100, 200)
	if err := aggregator.FlushDueBuckets(context.Background(), now.Add(2*time.Minute)); err != nil {
		t.Fatalf("second flush: %v", err)
	}
	if store.upload != 105 || store.download != 207 || store.logs != 2 {
		t.Fatalf("after the straggler billed %d/%d in %d logs, want 105/207 in 2", store.upload, store.download, store.logs)
	}
	if keys := aggregateKeys(t, client); len(keys) != 0 {
		t.Fatalf("aggregates left behind: %v", keys)
	}
	if n := indexed(t, client); n != 0 {
		t.Fatalf("%d buckets left in the index", n)
	}
	reissued := suffix + reissueSeparator + "1"
	if !store.marks[usageBucketConsumer+"|"+reissued] || !store.marks[networkTrafficBucketConsumer+"|"+reissued] {
		t.Fatalf("the reissued bucket was not marked in the inboxes under its own suffix: %v", store.marks)
	}
	if exists, _ := client.Exists(context.Background(), processedBucketPrefix+reissued).Result(); exists != 1 {
		t.Fatal("the reissued bucket was not marked processed")
	}
}

// A report whose write lands while the flush is between taking the bucket
// over and cleaning up re-creates and re-indexes the aggregate, and the
// cleanup must keep it discoverable until it is reissued.
func TestFlushReissuesAnAggregateRecreatedBeforeTheCleanup(t *testing.T) {
	aggregator, store, client := newLifecycleAggregator(t)
	now := time.Date(2026, 9, 28, 12, 0, 30, 0, time.UTC)

	report(t, aggregator, now, 5, 7)
	store.applyHook = func() {
		store.applyHook = nil
		report(t, aggregator, now, 100, 200)
	}
	if err := aggregator.FlushDueBuckets(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatalf("FlushDueBuckets: %v", err)
	}
	if store.upload != 105 || store.download != 207 || store.logs != 2 || store.applies != 2 {
		t.Fatalf("billed %d/%d in %d logs over %d applies, want 105/207 in 2 over 2", store.upload, store.download, store.logs, store.applies)
	}
	if keys := aggregateKeys(t, client); len(keys) != 0 {
		t.Fatalf("an aggregate was orphaned: %v", keys)
	}
	if n := indexed(t, client); n != 0 {
		t.Fatalf("%d buckets left in the index", n)
	}
}

// Every valid entry is billed: the report threshold is the nodes' hint, not
// a server-side filter.
func TestAddReportBillsEntriesOfAnySize(t *testing.T) {
	aggregator, _, client := newLifecycleAggregator(t)
	now := time.Date(2026, 9, 28, 12, 0, 30, 0, time.UTC)
	report(t, aggregator, now, 1, 0)
	report(t, aggregator, now, 0, 1)
	fields, err := client.HGetAll(context.Background(), bucketPrefix+bucketSuffix(bucketMinute(now))).Result()
	if err != nil {
		t.Fatal(err)
	}
	if fields[trafficField(4, 2, trafficFieldUpload)] != "1" || fields[trafficField(4, 2, trafficFieldDownload)] != "1" {
		t.Fatalf("one-byte entries were not billed: %v", fields)
	}
}

// A byte count is exact at ratio one and scaled in float64 otherwise;
// float32 rounded a 5 GB count to a 512-byte grid.
func TestScaleTrafficIsExact(t *testing.T) {
	const fiveGB = 5_000_000_001
	if got := scaleTraffic(fiveGB, 1, 1); got != fiveGB {
		t.Fatalf("scaleTraffic(5 GB, 1, 1) = %d, want %d", got, fiveGB)
	}
	if got := scaleTraffic(fiveGB, 1.5, 1); got != 7_500_000_001 {
		t.Fatalf("scaleTraffic(5 GB, 1.5, 1) = %d, want 7500000001", got)
	}
	if got := scaleTraffic(fiveGB, 2, 0.5); got != fiveGB {
		t.Fatalf("scaleTraffic(5 GB, 2, 0.5) = %d, want %d", got, fiveGB)
	}
	if got := scaleTraffic(MaxReportedTraffic, 1, 1); got != MaxReportedTraffic {
		t.Fatalf("scaleTraffic(max, 1, 1) = %d, want %d", got, MaxReportedTraffic)
	}
}

// Suffixes name the Unix minute; the wall-clock layout of the buckets written
// before the upgrade is still read, in the app zone, and a reissued suffix
// names its origin's minute.
func TestParseBucketTimeReadsCurrentLegacyAndReissuedSuffixes(t *testing.T) {
	useZone(t, "Asia/Shanghai")
	minute := time.Date(2026, 9, 28, 4, 0, 0, 0, time.UTC)
	suffix := bucketSuffix(minute)
	if suffix != strconv.FormatInt(minute.Unix(), 10) {
		t.Fatalf("bucketSuffix = %q", suffix)
	}
	for _, candidate := range []string{suffix, suffix + "-3"} {
		got, err := parseBucketTime(candidate)
		if err != nil || !got.Equal(minute) {
			t.Fatalf("parseBucketTime(%q) = %v, %v; want %v", candidate, got, err, minute)
		}
	}
	legacy, err := parseBucketTime("202607200640")
	if err != nil || !legacy.Equal(time.Date(2026, 7, 20, 6, 40, 0, 0, timeutil.Location())) {
		t.Fatalf("legacy suffix = %v, %v", legacy, err)
	}
	for _, malformed := range []string{"not-a-bucket", "1790000041", "-60", "", "abc"} {
		if _, err := parseBucketTime(malformed); err == nil {
			t.Fatalf("parseBucketTime(%q) accepted a suffix that names no minute", malformed)
		}
	}
}

// deadLetter parks a bucket of subscription 2 as the flush does after the
// failure threshold and returns its key.
func deadLetter(t *testing.T, aggregator *Aggregator, client *redis.Client, suffix string, upload, download int64) string {
	t.Helper()
	ctx := context.Background()
	processingKey := processingBucketPrefix + suffix
	if err := client.HSet(ctx, processingKey, map[string]any{
		trafficField(4, 2, trafficFieldUpload):   upload,
		trafficField(4, 2, trafficFieldDownload): download,
	}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, bucketFailureKey(suffix), bucketFailureThreshold-1, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := aggregator.handleBucketFlushFailure(ctx, suffix, processingKey, errors.New("database unavailable")); err != nil {
		t.Fatalf("move to dead letter: %v", err)
	}
	return deadLetterBucketKey(suffix)
}

// A dead letter is replayed once its retry is due and retired when the
// replay succeeds; its suffix stays spent.
func TestReplayDeadLettersBillsAndRetiresTheBucket(t *testing.T) {
	aggregator, store, client := newLifecycleAggregator(t)
	ctx := context.Background()
	suffix := bucketSuffix(time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC))
	key := deadLetter(t, aggregator, client, suffix, 5, 7)
	if exists, _ := client.Exists(ctx, processedBucketPrefix+suffix).Result(); exists != 1 {
		t.Fatal("a dead-lettered suffix must be marked spent")
	}

	// Not due yet: the first replay waits the base wait after the move.
	if err := aggregator.ReplayDeadLetters(ctx, timeutil.Now()); err != nil {
		t.Fatal(err)
	}
	if store.applies != 0 {
		t.Fatal("a dead letter was replayed before its retry was due")
	}

	if err := aggregator.ReplayDeadLetters(ctx, timeutil.Now().Add(2*deadLetterReplayBaseWait)); err != nil {
		t.Fatalf("ReplayDeadLetters: %v", err)
	}
	if store.upload != 5 || store.download != 7 || store.logs != 1 {
		t.Fatalf("replay billed %d/%d in %d logs, want 5/7 in 1", store.upload, store.download, store.logs)
	}
	for _, left := range []string{key, deadLetterMetaKey(key)} {
		if exists, _ := client.Exists(ctx, left).Result(); exists != 0 {
			t.Fatalf("%s survived the replay", left)
		}
	}
	if n, _ := client.ZCard(ctx, deadLetterIndexKey).Result(); n != 0 {
		t.Fatalf("%d dead letters left in the index", n)
	}
	if exists, _ := client.Exists(ctx, processedBucketPrefix+suffix).Result(); exists != 1 {
		t.Fatal("the replayed suffix is no longer marked spent")
	}
	// Billing under the original suffix keeps a replay from charging twice.
	if err := aggregator.persistBucket(ctx, suffix, []trafficDelta{{ServerId: 4, SubscribeId: 2, Upload: 5, Download: 7}}); err != nil {
		t.Fatal(err)
	}
	if store.upload != 5 || store.download != 7 || store.logs != 1 {
		t.Fatalf("a second pass under the suffix charged again: %d/%d in %d logs", store.upload, store.download, store.logs)
	}
}

// A failed replay waits twice as long each time, and a tick replays at most
// deadLetterReplayLimit dead letters.
func TestReplayDeadLettersBacksOffAndBoundsEachTick(t *testing.T) {
	aggregator, store, client := newLifecycleAggregator(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	suffix := bucketSuffix(base)
	key := deadLetter(t, aggregator, client, suffix, 5, 7)
	metaKey := deadLetterMetaKey(key)
	due := timeutil.Now().Add(2 * deadLetterReplayBaseWait)

	store.fail = "usage"
	if err := aggregator.ReplayDeadLetters(ctx, due); err == nil {
		t.Fatal("a failed replay must be reported")
	}
	meta, _ := client.HGetAll(ctx, metaKey).Result()
	if meta["replay_attempts"] != "1" || meta["last_error"] != errAccounting.Error() {
		t.Fatalf("meta after a failed replay = %v", meta)
	}
	next, err := time.Parse(time.RFC3339Nano, meta["next_retry_at"])
	if err != nil || !next.Equal(due.Add(deadLetterReplayBaseWait)) {
		t.Fatalf("next_retry_at = %q, %v; want %v", meta["next_retry_at"], err, due.Add(deadLetterReplayBaseWait))
	}
	if exists, _ := client.Exists(ctx, key).Result(); exists != 1 {
		t.Fatal("a failed replay dropped the dead letter")
	}

	// Before the next retry nothing happens; a second failure doubles the wait.
	store.fail = ""
	if err := aggregator.ReplayDeadLetters(ctx, due.Add(30*time.Second)); err != nil || store.applies != 0 {
		t.Fatalf("replayed before the backoff elapsed: applies=%d, %v", store.applies, err)
	}
	store.fail = "usage"
	_ = aggregator.ReplayDeadLetters(ctx, next)
	meta, _ = client.HGetAll(ctx, metaKey).Result()
	if second, _ := time.Parse(time.RFC3339Nano, meta["next_retry_at"]); meta["replay_attempts"] != "2" || !second.Equal(next.Add(2*deadLetterReplayBaseWait)) {
		t.Fatalf("meta after the second failure = %v", meta)
	}
	if replayBackoff(64) != deadLetterReplayMaxWait {
		t.Fatalf("backoff is not capped: %v", replayBackoff(64))
	}

	// Recovered: the replay succeeds once due again.
	store.fail = ""
	if err := aggregator.ReplayDeadLetters(ctx, next.Add(3*deadLetterReplayBaseWait)); err != nil {
		t.Fatalf("recovered replay: %v", err)
	}
	if store.upload != 5 || store.download != 7 {
		t.Fatalf("recovered replay billed %d/%d, want 5/7", store.upload, store.download)
	}

	// Seven due dead letters, moved before replays had a schedule: one tick
	// takes five, the next the rest.
	for i := 1; i <= 7; i++ {
		s := bucketSuffix(base.Add(time.Duration(i) * time.Minute))
		k := deadLetter(t, aggregator, client, s, 1, 1)
		if err := client.HDel(ctx, deadLetterMetaKey(k), "next_retry_at").Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := aggregator.ReplayDeadLetters(ctx, timeutil.Now()); err != nil {
		t.Fatal(err)
	}
	if n, _ := client.ZCard(ctx, deadLetterIndexKey).Result(); n != 2 {
		t.Fatalf("%d dead letters left after one tick, want 2", n)
	}
	if err := aggregator.ReplayDeadLetters(ctx, timeutil.Now()); err != nil {
		t.Fatal(err)
	}
	if n, _ := client.ZCard(ctx, deadLetterIndexKey).Result(); n != 0 {
		t.Fatalf("%d dead letters left after two ticks, want 0", n)
	}
	if store.upload != 12 || store.download != 14 {
		t.Fatalf("total billed %d/%d, want 12/14", store.upload, store.download)
	}
}

// A dead letter whose data expired is dropped from the index instead of
// being retried forever.
func TestReplayDeadLettersDropsAnExpiredBucket(t *testing.T) {
	aggregator, store, client := newLifecycleAggregator(t)
	ctx := context.Background()
	suffix := bucketSuffix(time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC))
	key := deadLetter(t, aggregator, client, suffix, 5, 7)
	if err := client.Del(ctx, key).Err(); err != nil {
		t.Fatal(err)
	}
	if err := aggregator.ReplayDeadLetters(ctx, timeutil.Now().Add(2*deadLetterReplayBaseWait)); err != nil {
		t.Fatal(err)
	}
	if store.applies != 0 {
		t.Fatal("an expired dead letter charged something")
	}
	if n, _ := client.ZCard(ctx, deadLetterIndexKey).Result(); n != 0 {
		t.Fatal("the expired dead letter is still indexed")
	}
	if exists, _ := client.Exists(ctx, deadLetterMetaKey(key)).Result(); exists != 0 {
		t.Fatal("the expired dead letter's meta survived")
	}
}

// The scheduled flush replays due dead letters after the buckets.
func TestFlushDueBucketsReplaysDeadLetters(t *testing.T) {
	aggregator, store, client := newLifecycleAggregator(t)
	suffix := bucketSuffix(time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC))
	deadLetter(t, aggregator, client, suffix, 5, 7)
	if err := aggregator.FlushDueBuckets(context.Background(), timeutil.Now().Add(2*deadLetterReplayBaseWait)); err != nil {
		t.Fatal(err)
	}
	if store.upload != 5 || store.download != 7 {
		t.Fatalf("the flush did not replay the dead letter: %d/%d", store.upload, store.download)
	}
}

// Interrupt the handoff either before Redis changes anything or after Redis
// committed it but the caller lost the reply.
type handoffFailure struct {
	afterCommit bool
	failed      bool
}

func (h *handoffFailure) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *handoffFailure) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *handoffFailure) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		args := cmd.Args()
		// The handoff script declares the bucket index as its third key.
		script := cmd.Name() == "eval" || cmd.Name() == "evalsha"
		if h.failed || !script || len(args) < 6 || args[5] != "panel:"+bucketIndexKey {
			return next(ctx, cmd)
		}
		if h.afterCommit {
			if err := next(ctx, cmd); err != nil {
				return err
			}
		}
		h.failed = true
		return errors.New("interrupted bucket handoff")
	}
}

func TestLateTrafficRemainsIndexedAcrossRedisFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		after bool
	}{{"before commit", false}, {"lost reply", true}} {
		t.Run(tc.name, func(t *testing.T) {
			a, store, client := newLifecycleAggregator(t)
			rediskeys.New("panel").Install(client)
			ctx := t.Context()
			now := time.Date(2026, 9, 28, 12, 0, 30, 0, time.UTC)
			report(t, a, now, 5, 7)
			if err := a.FlushDueBuckets(ctx, now.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			report(t, a, now, 100, 200)
			fail := &handoffFailure{afterCommit: tc.after}
			client.AddHook(fail)
			if err := a.FlushDueBuckets(ctx, now.Add(2*time.Minute)); err == nil || !fail.failed {
				t.Fatalf("handoff failure = %v", err)
			}
			if n := indexed(t, client); n != 1 {
				t.Fatalf("buckets discoverable after interrupted handoff = %d, want 1", n)
			}
			for i := 3; i <= 5; i++ {
				if err := a.FlushDueBuckets(ctx, now.Add(time.Duration(i)*time.Minute)); err != nil {
					t.Fatal(err)
				}
			}
			if store.upload != 105 || store.download != 207 || store.logs != 2 || indexed(t, client) != 0 {
				t.Fatalf("after retries accounted %d/%d in %d logs", store.upload, store.download, store.logs)
			}
		})
	}
}

// Parking an older batch under its original inbox ID must leave late reports
// discoverable and bill them under a separate ID from the pending replay.
func TestParkedBucketKeepsLateReportIndexed(t *testing.T) {
	a, store, client := newLifecycleAggregator(t)
	rediskeys.New("panel").Install(client)
	ctx := t.Context()
	now := timeutil.Now().Add(-time.Minute)
	suffix := bucketSuffix(bucketMinute(now))
	report(t, a, now, 100, 200)
	deadLetter(t, a, client, suffix, 5, 7)
	if indexed(t, client) != 1 {
		t.Fatal("late report lost its index during dead-letter transfer")
	}
	for range 2 {
		if err := a.FlushDueBuckets(ctx, timeutil.Now().Add(2*deadLetterReplayBaseWait)); err != nil {
			t.Fatal(err)
		}
	}
	if store.upload != 105 || store.download != 207 || store.logs != 2 || indexed(t, client) != 0 {
		t.Fatalf("accounted %d/%d with %d logs", store.upload, store.download, store.logs)
	}
}
