// Package trafficagg is the network module's traffic pipeline. Node reports
// accumulate in per-minute Redis buckets; a scheduled flush bills each bucket
// to the subscriptions' usage and writes the traffic log, retrying a failed
// bucket until it moves to the dead letters, which the flush replays with a
// backoff until they succeed or expire. The flush also persists the servers'
// latest report times.
package trafficagg

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	trafficEntity "github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/redis/go-redis/v9"
)

const (
	// legacyBucketLayout is the app-zone wall-clock layout the buckets were
	// named by before they were keyed by the Unix minute; buckets written
	// before the upgrade are still read by it.
	legacyBucketLayout     = "200601021504"
	bucketTTL              = 24 * time.Hour
	deadLetterTTL          = 30 * 24 * time.Hour
	bucketFailureThreshold = 10
	bucketIndexKey         = "traffic:agg:buckets"
	bucketPrefix           = "traffic:agg:"
	// bucketReissuePrefix counts, per minute, the buckets re-created after
	// the minute's flush and reissued under a fresh suffix.
	bucketReissuePrefix    = "traffic:reissue:"
	reissueSeparator       = "-"
	processingBucketPrefix = "traffic:processing:"
	// processedBucketPrefix marks a suffix as spent: a flush or a dead-letter
	// move consumed it as an inbox key, so anything re-created under it is
	// reissued rather than flushed under the same key again.
	processedBucketPrefix    = "traffic:processed:"
	bucketFailurePrefix      = "traffic:failed:"
	deadLetterBucketPrefix   = "traffic:deadletter:"
	deadLetterMetaPrefix     = "traffic:deadletter:meta:"
	deadLetterIndexKey       = "traffic:deadletter:buckets"
	deadLetterReplayLimit    = 5
	deadLetterReplayBaseWait = time.Minute
	deadLetterReplayMaxWait  = 6 * time.Hour
	serverLastReportedKey    = "traffic:server:last_reported"
	conditionalHDelLua       = `for i = 1, #ARGV, 2 do if redis.call("HGET", KEYS[1], ARGV[i]) == ARGV[i + 1] then redis.call("HDEL", KEYS[1], ARGV[i]) end end return 1`
	trafficFieldUpload       = "u"
	trafficFieldDownload     = "d"
	trafficFieldSeparator    = "|"
	defaultTrafficBatchSize  = 1000
	defaultTrafficMultiplier = 1
	droppedSIDLogLimit       = 20
)

// Claim an aggregate or drop a stale index entry atomically with respect to report writes.
var claimBucket = redis.NewScript(`
	if redis.call("EXISTS", KEYS[3]) == 1 then return 2 end
	if redis.call("EXISTS", KEYS[2]) == 1 then return 1 end
	if redis.call("EXISTS", KEYS[1]) == 0 then
		redis.call("ZREM", KEYS[4], ARGV[1])
		return 0
	end
	redis.call("RENAMENX", KEYS[1], KEYS[2])
	return 1
`)

// A late report re-created the aggregate and needs the old index entry until
// it can be handed off. AddReportAt re-indexes writes that arrive after this check.
var unindexIfEmpty = redis.NewScript(`
	if redis.call("EXISTS", KEYS[1]) == 0 then
		return redis.call("ZREM", KEYS[2], ARGV[1])
	end
	return 0
`)

// Register the destination before moving the data. If the client loses the
// reply, the next flush finds the fresh suffix and uses its durable inbox ID.
var reindexStraggler = redis.NewScript(`
	if redis.call("EXISTS", KEYS[1]) == 0 then return 0 end
	if redis.call("EXISTS", KEYS[2]) == 1 then
		return redis.error_reply("reissued processing bucket already exists")
	end
	redis.call("ZADD", KEYS[3], ARGV[1], ARGV[2])
	redis.call("RENAMENX", KEYS[1], KEYS[2])
	redis.call("ZREM", KEYS[3], ARGV[3])
	return 1
`)

// Park the processing hash, its replay metadata, and its index together. The
// index stores the logical destination name; Redis commands operate on KEYS.
// ARGV after the two TTLs contains the remaining metadata field/value pairs.
var parkBucket = redis.NewScript(`
	local fields = redis.call("HLEN", KEYS[1])
	if fields == 0 then return 0 end
	if redis.call("EXISTS", KEYS[2], KEYS[3]) > 0 then return -1 end
	redis.call("ZADD", KEYS[4], ARGV[3], ARGV[2])
	redis.call("HSET", KEYS[3], "bucket", ARGV[1], "deadletter_key", ARGV[2],
		"field_count", fields, "replay_attempts", 0, unpack(ARGV, 6))
	redis.call("RENAMENX", KEYS[1], KEYS[2])
	redis.call("EXPIRE", KEYS[2], ARGV[4])
	redis.call("EXPIRE", KEYS[3], ARGV[4])
	redis.call("EXPIRE", KEYS[4], ARGV[4])
	redis.call("SET", KEYS[5], "1", "EX", ARGV[5])
	redis.call("DEL", KEYS[6])
	if redis.call("EXISTS", KEYS[7]) == 0 then redis.call("ZREM", KEYS[8], ARGV[1]) end
	return fields
`)

// MaxReportedTraffic bounds the bytes (upload plus download) one traffic
// report may attribute to one subscription. No node carries that much for a
// single user between two reports (10 TiB is about 15 minutes at a sustained
// 100 Gbit/s), so only a corrupt or forged report reaches it; the bound also
// keeps the Redis HINCRBY and SQL `upload + ?` increments far from int64
// overflow.
const MaxReportedTraffic int64 = 10 << 40

// UserTraffic is one entry of a node's traffic report: the bytes one
// subscription used since the previous report.
type UserTraffic struct {
	SID      int64
	Upload   int64
	Download int64
}

// Aggregator is the traffic pipeline: it collects the node reports and
// flushes them to the usage counters and the traffic log.
type Aggregator struct {
	deps Deps
}

type trafficKey struct {
	ServerId    int64
	SubscribeId int64
}

type trafficDelta struct {
	ServerId    int64
	SubscribeId int64
	Upload      int64
	Download    int64
}

// Deps declares the network pipeline's collaborators. Queue adapters receive
// this pipeline through the network facade. Usage writes are performed by
// the injected subscription service, not the store.
type Deps struct {
	Store Store
	Usage subscription.TrafficUsage
	Redis *redis.Client
	// Subscriptions reads the owners of the subscriptions a bucket charges.
	Subscriptions SubscriptionReader
	// Multiplier returns the node traffic multiplier in effect at the given
	// time; nil means no multiplier is configured.
	Multiplier func(at time.Time) float32
	// ServedSubscriptions resolves the user_subscribe ids the server serves
	// over the protocol; report entries naming any other subscription are
	// dropped. nil skips the check. It is called at most once per report.
	ServedSubscriptions func(ctx context.Context, serverID int64, protocol string) (map[int64]struct{}, error)
}

// New returns the pipeline over deps.
func New(deps Deps) *Aggregator {
	return &Aggregator{deps: deps}
}

// AddReport adds a server's traffic report for a protocol, received now.
func (a *Aggregator) AddReport(ctx context.Context, serverInfo *node.Server, protocol string, logs []UserTraffic) error {
	return a.AddReportAt(ctx, serverInfo, protocol, logs, timeutil.Now())
}

// RecordServerReport records the latest heartbeat in Redis. The scheduled
// traffic flush persists all servers' latest timestamps to the database in one
// batch, avoiding a database write for every node heartbeat.
func (a *Aggregator) RecordServerReport(ctx context.Context, serverID int64, reportedAt time.Time) error {
	if a == nil || a.deps.Redis == nil {
		return errors.New("traffic aggregator is not initialized")
	}
	if serverID <= 0 {
		return errors.New("server not found")
	}
	return a.deps.Redis.HSet(ctx, serverLastReportedKey, strconv.FormatInt(serverID, 10), strconv.FormatInt(reportedAt.UnixMilli(), 10)).Err()
}

// AddReportAt records the report as the server's heartbeat and adds its
// entries, scaled by the protocol ratio and the multiplier in effect, to the
// bucket of now's minute. An entry that is invalid or for a subscription the
// server does not serve is dropped. Every valid entry is billed, however
// small: the node settings' report threshold is the nodes' reporting hint
// (a node holds a subscription's delta back until it exceeds it), not a
// server-side filter, which would lose traffic a node reported in good
// faith. The writes go in one transaction, so a node that retries a report
// after a broken connection cannot bill it twice in part.
func (a *Aggregator) AddReportAt(ctx context.Context, serverInfo *node.Server, protocol string, logs []UserTraffic, now time.Time) error {
	if a == nil || a.deps.Redis == nil {
		return errors.New("traffic aggregator is not initialized")
	}
	if serverInfo == nil || serverInfo.Id <= 0 {
		return errors.New("server not found")
	}

	pipe := a.deps.Redis.TxPipeline()
	pipe.HSet(ctx, serverLastReportedKey, strconv.FormatInt(serverInfo.Id, 10), strconv.FormatInt(now.UnixMilli(), 10))

	if len(logs) == 0 {
		_, err := pipe.Exec(ctx)
		return err
	}

	ratio, ok, err := protocolRatio(serverInfo, protocol)
	if err != nil {
		logger.WithContext(ctx).Error("[TrafficAggregator] Unmarshal protocols failed",
			logger.Field("server_id", serverInfo.Id),
			logger.Field("protocol", protocol),
			logger.Field("error", err.Error()),
		)
		_, execErr := pipe.Exec(ctx)
		return execErr
	}
	if !ok {
		logger.WithContext(ctx).Error("[TrafficAggregator] Protocol not found",
			logger.Field("server_id", serverInfo.Id),
			logger.Field("protocol", protocol),
		)
		_, execErr := pipe.Exec(ctx)
		return execErr
	}

	minute := bucketMinute(now)
	suffix := bucketSuffix(minute)
	bucketKey := bucketPrefix + suffix
	multiplier := float64(defaultTrafficMultiplier)
	if a.deps.Multiplier != nil {
		multiplier = float64(a.deps.Multiplier(now))
	}

	// Entries are checked one by one: an invalid or foreign entry is dropped
	// and logged without costing the rest of the report.
	var invalid, unserved droppedEntries
	scope := servedScope{resolve: a.deps.ServedSubscriptions}
	reported := make(map[int64]int64, len(logs))
	trafficOps := 0
	for _, item := range logs {
		if item.SID <= 0 {
			continue
		}
		total, ok := reportedTotal(item)
		if !ok {
			invalid.add(item.SID)
			continue
		}
		if total == 0 {
			continue
		}
		serves, err := scope.serves(ctx, serverInfo.Id, protocol, item.SID)
		if err != nil {
			// Nothing can be attributed without the scope; the heartbeat is
			// still recorded.
			if _, execErr := pipe.Exec(ctx); execErr != nil {
				logger.WithContext(ctx).Error("[TrafficAggregator] Record server report failed",
					logger.Field("server_id", serverInfo.Id),
					logger.Field("error", execErr.Error()),
				)
			}
			return fmt.Errorf("resolve served subscriptions: %w", err)
		}
		if !serves {
			unserved.add(item.SID)
			continue
		}
		// Repeated entries for one subscription share the per-report bound.
		if reported[item.SID] > MaxReportedTraffic-total {
			invalid.add(item.SID)
			continue
		}
		reported[item.SID] += total
		pipe.HIncrBy(ctx, bucketKey, trafficField(serverInfo.Id, item.SID, trafficFieldDownload), scaleTraffic(item.Download, ratio, multiplier))
		pipe.HIncrBy(ctx, bucketKey, trafficField(serverInfo.Id, item.SID, trafficFieldUpload), scaleTraffic(item.Upload, ratio, multiplier))
		trafficOps++
	}
	logDropped(ctx, serverInfo.Id, protocol, invalid, unserved)

	if trafficOps > 0 {
		pipe.Expire(ctx, bucketKey, bucketTTL)
		pipe.ZAdd(ctx, bucketIndexKey, redis.Z{
			Score:  float64(minute.Unix()),
			Member: suffix,
		})
	}

	_, err = pipe.Exec(ctx)
	return err
}

// scaleTraffic applies the protocol ratio and the multiplier to a reported
// byte count. A factor of one leaves the count untouched, so unscaled
// traffic is billed byte for byte; any other factor is applied in float64,
// which carries every count a report may hold exactly, and rounds down.
func scaleTraffic(count int64, ratio, multiplier float64) int64 {
	factor := ratio * multiplier
	if factor == 1 {
		return count
	}
	return int64(float64(count) * factor)
}

// servedScope resolves, at most once per report, the subscriptions a server
// serves over a protocol; without a resolver every subscription is served.
type servedScope struct {
	resolve  func(ctx context.Context, serverID int64, protocol string) (map[int64]struct{}, error)
	served   map[int64]struct{}
	resolved bool
}

func (s *servedScope) serves(ctx context.Context, serverID int64, protocol string, sid int64) (bool, error) {
	if s.resolve == nil {
		return true, nil
	}
	if !s.resolved {
		served, err := s.resolve(ctx, serverID, protocol)
		if err != nil {
			return false, err
		}
		s.served, s.resolved = served, true
	}
	_, ok := s.served[sid]
	return ok, nil
}

// logDropped reports the entries a traffic report lost: invalid ones are
// errors; a few unserved ones are expected around a user-list refresh (a
// subscription that just expired or moved plans), many point at a
// misbehaving node.
func logDropped(ctx context.Context, serverID int64, protocol string, invalid, unserved droppedEntries) {
	if invalid.count > 0 {
		logger.WithContext(ctx).Error("[TrafficAggregator] Dropped invalid traffic entries",
			logger.Field("server_id", serverID),
			logger.Field("protocol", protocol),
			logger.Field("count", invalid.count),
			logger.Field("sids", invalid.sids),
		)
	}
	if unserved.count > 0 {
		logger.WithContext(ctx).Info("[TrafficAggregator] Dropped traffic for subscriptions the server does not serve",
			logger.Field("server_id", serverID),
			logger.Field("protocol", protocol),
			logger.Field("count", unserved.count),
			logger.Field("sids", unserved.sids),
		)
	}
}

// FlushDueBuckets flushes every bucket of a minute before now's, replays
// the dead-letter buckets whose retry is due, then persists the servers'
// report times. A failing bucket does not stop the others; the failures are
// returned together. A failed replay is only logged: the dead letter's own
// backoff schedules its next attempt.
func (a *Aggregator) FlushDueBuckets(ctx context.Context, now time.Time) error {
	if a == nil || a.deps.Redis == nil {
		return errors.New("traffic aggregator is not initialized")
	}

	maxScore := bucketMinute(now).Unix() - 1
	buckets, err := a.deps.Redis.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:     bucketIndexKey,
		Start:   "-inf",
		Stop:    strconv.FormatInt(maxScore, 10),
		ByScore: true,
	}).Result()
	if err != nil {
		return err
	}

	var errs []string
	for _, suffix := range buckets {
		if err := a.flushBucket(ctx, suffix); err != nil {
			logger.WithContext(ctx).Error("[TrafficAggregator] Flush bucket failed",
				logger.Field("bucket", suffix),
				logger.Field("error", err.Error()),
			)
			errs = append(errs, fmt.Sprintf("%s: %s", suffix, err.Error()))
		}
	}

	if err := a.ReplayDeadLetters(ctx, now); err != nil {
		logger.WithContext(ctx).Error("[TrafficAggregator] Dead-letter replay failed", logger.Field("error", err.Error()))
	}

	if err := a.FlushServerReports(ctx); err != nil {
		logger.WithContext(ctx).Error("[TrafficAggregator] Flush server reports failed", logger.Field("error", err.Error()))
		errs = append(errs, "server_reports: "+err.Error())
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// FlushServerReports persists the servers' latest report times and drops
// those that did not change meanwhile from Redis.
func (a *Aggregator) FlushServerReports(ctx context.Context) error {
	values, err := a.deps.Redis.HGetAll(ctx, serverLastReportedKey).Result()
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}

	reports := make(map[int64]time.Time, len(values))
	args := make([]any, 0, len(values)*2)
	for field, value := range values {
		serverID, parseErr := strconv.ParseInt(field, 10, 64)
		if parseErr != nil || serverID <= 0 {
			continue
		}
		millis, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil {
			continue
		}
		reports[serverID] = time.UnixMilli(millis).In(timeutil.Location())
		args = append(args, field, value)
	}
	if len(reports) == 0 {
		return nil
	}

	if err := a.deps.Store.BatchUpdateServerLastReportedAt(ctx, reports); err != nil {
		return err
	}
	return redis.NewScript(conditionalHDelLua).Run(ctx, a.deps.Redis, []string{serverLastReportedKey}, args...).Err()
}

// flushBucket bills one indexed bucket. It takes the aggregate over by
// renaming it to the processing key, so one replica flushes it; a suffix
// already spent only has its leftovers cleaned. Either way an aggregate a
// late report re-created meanwhile is reissued (see reissueStraggler), so
// no report is dropped.
func (a *Aggregator) flushBucket(ctx context.Context, suffix string) error {
	if _, err := parseBucketTime(suffix); err != nil {
		// A key that names no bucket can never be flushed: drop it from the
		// index rather than retry it forever.
		logger.WithContext(ctx).Errorw("[TrafficAggregator] dropping a malformed bucket", logger.Field("suffix", suffix), logger.Field("error", err.Error()))
		pipe := a.deps.Redis.TxPipeline()
		pipe.ZRem(ctx, bucketIndexKey, suffix)
		pipe.Del(ctx, bucketFailureKey(suffix))
		_, dropErr := pipe.Exec(ctx)
		return dropErr
	}

	aggregateKey := bucketPrefix + suffix
	processingKey := processingBucketPrefix + suffix
	processedKey := processedBucketPrefix + suffix
	state, err := claimBucket.Run(ctx, a.deps.Redis, []string{aggregateKey, processingKey, processedKey, bucketIndexKey}, suffix).Int()
	if err != nil {
		return err
	}
	switch state {
	case 0: // empty
		return nil
	case 2: // already billed or parked
		if err := a.cleanupBucket(ctx, suffix, processingKey); err != nil {
			return err
		}
		return a.reissueStraggler(ctx, suffix)
	}

	values, err := a.deps.Redis.HGetAll(ctx, processingKey).Result()
	if err != nil {
		return err
	}
	deltas := parseTrafficDeltas(ctx, values)
	if len(deltas) == 0 {
		if err := a.cleanupBucket(ctx, suffix, processingKey); err != nil {
			return err
		}
		return a.reissueStraggler(ctx, suffix)
	}

	if err := a.persistBucket(ctx, suffix, deltas); err != nil {
		return a.handleBucketFlushFailure(ctx, suffix, processingKey, err)
	}
	if err := a.markBucketProcessedAndCleanup(ctx, suffix, processingKey, processedKey); err != nil {
		return err
	}
	return a.reissueStraggler(ctx, suffix)
}

// reissueStraggler flushes, under a fresh suffix, an aggregate re-created
// after its minute's flush took the original: a report whose write landed
// between the take-over and the cleanup, or after the
// minute was spent. Under its own suffix the reissued bucket is a new key to
// the idempotent inboxes, which know the original suffix as consumed, so
// its traffic is billed rather than skipped as a replay.
func (a *Aggregator) reissueStraggler(ctx context.Context, suffix string) error {
	aggregateKey := bucketPrefix + suffix
	exists, err := a.deps.Redis.Exists(ctx, aggregateKey).Result()
	if err != nil || exists == 0 {
		return err
	}
	origin, _, _ := strings.Cut(suffix, reissueSeparator)
	counterKey := bucketReissuePrefix + origin
	sequence, err := a.deps.Redis.Incr(ctx, counterKey).Result()
	if err != nil {
		return err
	}
	if err := a.deps.Redis.Expire(ctx, counterKey, bucketTTL).Err(); err != nil {
		return err
	}
	fresh := origin + reissueSeparator + strconv.FormatInt(sequence, 10)
	moved, err := reindexStraggler.Run(ctx, a.deps.Redis,
		[]string{aggregateKey, processingBucketPrefix + fresh, bucketIndexKey},
		bucketScore(origin), fresh, suffix).Bool()
	if err != nil || !moved {
		return err
	}
	logger.WithContext(ctx).Infow("[TrafficAggregator] Reissued a bucket re-created after its flush",
		logger.Field("bucket", suffix),
		logger.Field("reissued_as", fresh),
	)
	return a.flushBucket(ctx, fresh)
}

func (a *Aggregator) cleanupBucket(ctx context.Context, suffix, processingKey string) error {
	pipe := a.deps.Redis.TxPipeline()
	pipe.Del(ctx, processingKey)
	unindexIfEmpty.Eval(ctx, pipe, []string{bucketPrefix + suffix, bucketIndexKey}, suffix)
	pipe.Del(ctx, bucketFailureKey(suffix))
	_, err := pipe.Exec(ctx)
	return err
}

func (a *Aggregator) markBucketProcessedAndCleanup(ctx context.Context, suffix, processingKey, processedKey string) error {
	pipe := a.deps.Redis.TxPipeline()
	pipe.Set(ctx, processedKey, "1", bucketTTL)
	pipe.Del(ctx, processingKey)
	unindexIfEmpty.Eval(ctx, pipe, []string{bucketPrefix + suffix, bucketIndexKey}, suffix)
	pipe.Del(ctx, bucketFailureKey(suffix))
	_, err := pipe.Exec(ctx)
	return err
}

func (a *Aggregator) handleBucketFlushFailure(ctx context.Context, suffix, processingKey string, cause error) error {
	failures, err := a.recordBucketFlushFailure(ctx, suffix)
	if err != nil {
		return err
	}
	if failures < bucketFailureThreshold {
		return cause
	}

	return a.moveBucketToDeadLetter(ctx, suffix, processingKey, failures, cause)
}

func (a *Aggregator) recordBucketFlushFailure(ctx context.Context, suffix string) (int64, error) {
	failureKey := bucketFailureKey(suffix)
	failures, err := a.deps.Redis.Incr(ctx, failureKey).Result()
	if err != nil {
		return 0, err
	}
	if err := a.deps.Redis.Expire(ctx, failureKey, deadLetterTTL).Err(); err != nil {
		return 0, err
	}
	return failures, nil
}

// moveBucketToDeadLetter parks a bucket the flush keeps failing on, with
// the metadata the replay reads, and marks its suffix spent: a failed flush
// may have committed one half under it, which the replay under the same
// suffix skips through the inboxes. Its first replay is due after the base
// wait.
func (a *Aggregator) moveBucketToDeadLetter(ctx context.Context, suffix, processingKey string, failures int64, cause error) error {
	now := timeutil.Now()
	causeText := ""
	if cause != nil {
		causeText = cause.Error()
	}
	base := deadLetterBucketKey(suffix)
	dest := base
	for attempt := 0; attempt < 4; attempt++ {
		fields, err := parkBucket.Run(ctx, a.deps.Redis, []string{
			processingKey, dest, deadLetterMetaKey(dest), deadLetterIndexKey,
			processedBucketPrefix + suffix, bucketFailureKey(suffix), bucketPrefix + suffix, bucketIndexKey,
		}, suffix, dest, bucketScore(suffix), int64(deadLetterTTL/time.Second), int64(bucketTTL/time.Second),
			"source_key", processingKey, "failure_count", failures, "last_error", causeText,
			"moved_at", now.Format(time.RFC3339Nano), "next_retry_at", now.Add(deadLetterReplayBaseWait).Format(time.RFC3339Nano)).Int64()
		switch {
		case err != nil:
			return err
		case fields == 0:
			return nil // another flush already moved it
		case fields > 0:
			logger.WithContext(ctx).Error("[TrafficAggregator] Bucket moved to deadletter",
				logger.Field("bucket", suffix), logger.Field("failures", failures),
				logger.Field("deadletter_key", dest), logger.Field("field_count", fields), logger.Field("error", causeText))
			return nil
		}
		dest = fmt.Sprintf("%s:%d:%d", base, now.UnixNano(), attempt)
	}
	return fmt.Errorf("deadletter key already exists for bucket %s", suffix)
}

// ReplayDeadLetters re-applies up to deadLetterReplayLimit dead-lettered
// buckets whose retry is due, oldest first, and retires those that succeed.
// A failed replay waits twice as long as the one before, up to
// deadLetterReplayMaxWait; a dead letter whose data expired (deadLetterTTL)
// is dropped from the index. A replay bills under the bucket's original
// suffix, so the idempotent inboxes skip the half its failed flush had
// committed. The failures are returned together.
func (a *Aggregator) ReplayDeadLetters(ctx context.Context, now time.Time) error {
	if a == nil || a.deps.Redis == nil {
		return errors.New("traffic aggregator is not initialized")
	}
	keys, err := a.deps.Redis.ZRange(ctx, deadLetterIndexKey, 0, -1).Result()
	if err != nil {
		return err
	}
	replayed := 0
	var errs []string
	for _, deadLetterKey := range keys {
		if replayed >= deadLetterReplayLimit {
			break
		}
		meta, err := a.deps.Redis.HGetAll(ctx, deadLetterMetaKey(deadLetterKey)).Result()
		if err != nil {
			return err
		}
		if !replayDue(meta, now) {
			continue
		}
		replayed++
		if err := a.replayDeadLetter(ctx, deadLetterKey, meta, now); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %s", deadLetterKey, err.Error()))
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// replayDue reports whether a dead letter's next retry is at or before now;
// one without a schedule (moved before replays existed) is due.
func replayDue(meta map[string]string, now time.Time) bool {
	next, err := time.Parse(time.RFC3339Nano, meta["next_retry_at"])
	return err != nil || !now.Before(next)
}

func (a *Aggregator) replayDeadLetter(ctx context.Context, deadLetterKey string, meta map[string]string, now time.Time) error {
	suffix := meta["bucket"]
	if suffix == "" {
		suffix, _, _ = strings.Cut(strings.TrimPrefix(deadLetterKey, deadLetterBucketPrefix), ":")
	}
	metaKey := deadLetterMetaKey(deadLetterKey)
	values, err := a.deps.Redis.HGetAll(ctx, deadLetterKey).Result()
	if err != nil {
		return err
	}
	deltas := parseTrafficDeltas(ctx, values)
	if len(deltas) == 0 {
		// The data expired, or never named a subscription: nothing is left
		// to bill.
		logger.WithContext(ctx).Errorw("[TrafficAggregator] Dropping a dead-letter bucket with nothing to replay",
			logger.Field("bucket", suffix),
			logger.Field("deadletter_key", deadLetterKey),
		)
		return a.retireDeadLetter(ctx, suffix, deadLetterKey, metaKey)
	}

	if err := a.persistBucket(ctx, suffix, deltas); err != nil {
		attempts, _ := strconv.ParseInt(meta["replay_attempts"], 10, 64)
		attempts++
		nextRetry := now.Add(replayBackoff(attempts))
		pipe := a.deps.Redis.TxPipeline()
		pipe.HSet(ctx, metaKey, map[string]any{
			"replay_attempts": attempts,
			"next_retry_at":   nextRetry.Format(time.RFC3339Nano),
			"last_replay_at":  now.Format(time.RFC3339Nano),
			"last_error":      err.Error(),
		})
		pipe.Expire(ctx, metaKey, deadLetterTTL)
		if _, metaErr := pipe.Exec(ctx); metaErr != nil {
			return errors.Join(err, metaErr)
		}
		logger.WithContext(ctx).Errorw("[TrafficAggregator] Dead-letter bucket replay failed",
			logger.Field("bucket", suffix),
			logger.Field("deadletter_key", deadLetterKey),
			logger.Field("replay_attempts", attempts),
			logger.Field("next_retry_at", nextRetry.Format(time.RFC3339)),
			logger.Field("error", err.Error()),
		)
		return err
	}

	logger.WithContext(ctx).Infow("[TrafficAggregator] Dead-letter bucket replayed",
		logger.Field("bucket", suffix),
		logger.Field("deadletter_key", deadLetterKey),
		logger.Field("field_count", len(values)),
		logger.Field("replay_attempts", meta["replay_attempts"]),
	)
	return a.retireDeadLetter(ctx, suffix, deadLetterKey, metaKey)
}

// retireDeadLetter drops a replayed (or empty) dead letter and marks its
// suffix spent.
func (a *Aggregator) retireDeadLetter(ctx context.Context, suffix, deadLetterKey, metaKey string) error {
	pipe := a.deps.Redis.TxPipeline()
	pipe.Set(ctx, processedBucketPrefix+suffix, "1", bucketTTL)
	pipe.Del(ctx, deadLetterKey, metaKey)
	pipe.ZRem(ctx, deadLetterIndexKey, deadLetterKey)
	_, err := pipe.Exec(ctx)
	return err
}

// replayBackoff is the wait before the next replay after attempts failed
// ones: the base wait, doubled per attempt, up to the maximum.
func replayBackoff(attempts int64) time.Duration {
	wait := deadLetterReplayBaseWait
	for i := int64(1); i < attempts && wait < deadLetterReplayMaxWait; i++ {
		wait *= 2
	}
	return min(wait, deadLetterReplayMaxWait)
}

func (a *Aggregator) persistBucket(ctx context.Context, suffix string, deltas []trafficDelta) error {
	timestamp, err := parseBucketTime(suffix)
	if err != nil {
		return err
	}

	subscribeIDs := make([]int64, 0, len(deltas))
	seen := make(map[int64]struct{}, len(deltas))
	for _, delta := range deltas {
		if _, ok := seen[delta.SubscribeId]; ok {
			continue
		}
		seen[delta.SubscribeId] = struct{}{}
		subscribeIDs = append(subscribeIDs, delta.SubscribeId)
	}

	if a.deps.Subscriptions == nil {
		return errors.New("subscription reads are not configured")
	}
	subs, err := a.deps.Subscriptions.SubscriptionsByIDs(ctx, subscribeIDs)
	if err != nil {
		return err
	}
	subMap := make(map[int64]int64, len(subs))
	for _, sub := range subs {
		if sub != nil {
			subMap[sub.Id] = sub.UserId
		}
	}

	updateMap := make(map[int64]trafficEntity.SubscribeTrafficDelta)
	logs := make([]*trafficEntity.TrafficLog, 0, len(deltas))
	for _, delta := range deltas {
		userID, ok := subMap[delta.SubscribeId]
		if !ok {
			logger.WithContext(ctx).Error("[TrafficAggregator] User subscribe not found",
				logger.Field("sid", delta.SubscribeId),
				logger.Field("server_id", delta.ServerId),
			)
			continue
		}
		current := updateMap[delta.SubscribeId]
		current.SubscribeId = delta.SubscribeId
		current.Download += delta.Download
		current.Upload += delta.Upload
		updateMap[delta.SubscribeId] = current
		logs = append(logs, &trafficEntity.TrafficLog{
			ServerId:    delta.ServerId,
			SubscribeId: delta.SubscribeId,
			UserId:      userID,
			Upload:      delta.Upload,
			Download:    delta.Download,
			Timestamp:   timestamp,
		})
	}
	if len(updateMap) == 0 {
		return nil
	}

	updates := make([]trafficEntity.SubscribeTrafficDelta, 0, len(updateMap))
	for _, update := range updateMap {
		updates = append(updates, update)
	}
	sort.Slice(updates, func(i, j int) bool {
		return updates[i].SubscribeId < updates[j].SubscribeId
	})

	// The subscription usage counters and the network traffic log commit in
	// their own domain transactions (ADR-001 step 2). The bucket suffix is a
	// stable replay key: the flush pipeline retries the identical bucket until
	// it succeeds (then deadletters), so each transaction marks the bucket in
	// the idempotent inbox to keep replays from double-counting the side that
	// already committed.
	if a.deps.Usage == nil {
		return errors.New("subscription traffic accounting is not configured")
	}
	if err := a.deps.Usage.ApplyBucketOnce(ctx, suffix, updates); err != nil {
		return err
	}
	processed, err := a.bucketProcessed(ctx, networkTrafficBucketConsumer, suffix)
	if err != nil {
		return err
	}
	if processed {
		return nil
	}
	return a.deps.Store.InTrafficLogTx(ctx, func(tx TrafficLogTx) error {
		if err := tx.InsertTrafficLogs(ctx, logs, defaultTrafficBatchSize); err != nil {
			return err
		}
		return tx.InsertInboxRecord(ctx, networkTrafficBucketConsumer, suffix)
	})
}

// The subscription half owns its own durable consumer identity.
const networkTrafficBucketConsumer = "network.traffic_bucket"

func (a *Aggregator) bucketProcessed(ctx context.Context, consumer, suffix string) (bool, error) {
	mark, err := a.deps.Store.FindInboxRecord(ctx, consumer, suffix)
	if err != nil {
		return false, err
	}
	return mark != nil, nil
}

// reportedTotal validates one reported entry and returns its upload plus
// download: both must be non-negative and within MaxReportedTraffic.
func reportedTotal(item UserTraffic) (int64, bool) {
	if item.Upload < 0 || item.Download < 0 || item.Upload > MaxReportedTraffic || item.Download > MaxReportedTraffic-item.Upload {
		return 0, false
	}
	return item.Upload + item.Download, true
}

// droppedEntries counts the report entries a check rejected, keeping the
// first few subscription ids for the log line.
type droppedEntries struct {
	count int
	sids  []int64
}

func (d *droppedEntries) add(sid int64) {
	d.count++
	if len(d.sids) < droppedSIDLogLimit {
		d.sids = append(d.sids, sid)
	}
}

func protocolRatio(serverInfo *node.Server, protocol string) (float64, bool, error) {
	protocols, err := serverInfo.UnmarshalProtocols()
	if err != nil {
		return 0, false, err
	}
	for _, item := range protocols {
		if strings.EqualFold(item.Type, protocol) {
			if item.Ratio > 0 {
				return item.Ratio, true, nil
			}
			return 1, true, nil
		}
	}
	return 0, false, nil
}

func trafficField(serverID, subscribeID int64, kind string) string {
	return strings.Join([]string{
		strconv.FormatInt(serverID, 10),
		strconv.FormatInt(subscribeID, 10),
		kind,
	}, trafficFieldSeparator)
}

func bucketFailureKey(suffix string) string {
	return bucketFailurePrefix + suffix
}

func deadLetterBucketKey(suffix string) string {
	return deadLetterBucketPrefix + suffix
}

func deadLetterMetaKey(deadLetterKey string) string {
	return deadLetterMetaPrefix + strings.TrimPrefix(deadLetterKey, deadLetterBucketPrefix)
}

func bucketScore(suffix string) float64 {
	timestamp, err := parseBucketTime(suffix)
	if err != nil {
		return float64(timeutil.Now().Unix())
	}
	return float64(timestamp.Unix())
}

func parseTrafficDeltas(ctx context.Context, values map[string]string) []trafficDelta {
	merged := make(map[trafficKey]trafficDelta, len(values)/2)
	for field, rawValue := range values {
		parts := strings.Split(field, trafficFieldSeparator)
		if len(parts) != 3 {
			logger.WithContext(ctx).Error("[TrafficAggregator] Invalid traffic field", logger.Field("field", field))
			continue
		}
		serverID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			logger.WithContext(ctx).Error("[TrafficAggregator] Invalid server id", logger.Field("field", field), logger.Field("error", err.Error()))
			continue
		}
		subscribeID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			logger.WithContext(ctx).Error("[TrafficAggregator] Invalid subscribe id", logger.Field("field", field), logger.Field("error", err.Error()))
			continue
		}
		value, err := strconv.ParseInt(rawValue, 10, 64)
		if err != nil {
			logger.WithContext(ctx).Error("[TrafficAggregator] Invalid traffic value", logger.Field("field", field), logger.Field("value", rawValue), logger.Field("error", err.Error()))
			continue
		}

		key := trafficKey{ServerId: serverID, SubscribeId: subscribeID}
		delta := merged[key]
		delta.ServerId = serverID
		delta.SubscribeId = subscribeID
		switch parts[2] {
		case trafficFieldUpload:
			delta.Upload += value
		case trafficFieldDownload:
			delta.Download += value
		default:
			logger.WithContext(ctx).Error("[TrafficAggregator] Invalid traffic kind", logger.Field("field", field))
			continue
		}
		merged[key] = delta
	}

	result := make([]trafficDelta, 0, len(merged))
	for _, delta := range merged {
		result = append(result, delta)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ServerId == result[j].ServerId {
			return result[i].SubscribeId < result[j].SubscribeId
		}
		return result[i].ServerId < result[j].ServerId
	})
	return result
}

// bucketMinute is the minute a report received at now belongs to.
func bucketMinute(now time.Time) time.Time {
	return now.UTC().Truncate(time.Minute)
}

// bucketSuffix names a minute's bucket by its Unix time, which no zone or
// daylight-saving change can make ambiguous: the app-zone wall-clock layout
// it replaces named two minutes alike once a year, and the second one's
// reports were dropped as a replay of the first.
func bucketSuffix(minute time.Time) string {
	return strconv.FormatInt(minute.Unix(), 10)
}

// parseBucketTime returns the minute a bucket suffix names, in the app
// zone. A reissued bucket (suffix-N) names its origin's minute; a suffix in
// the wall-clock layout of the buckets written before the upgrade is read
// in the app zone, as it was written.
func parseBucketTime(suffix string) (time.Time, error) {
	origin, _, _ := strings.Cut(suffix, reissueSeparator)
	if len(origin) == len(legacyBucketLayout) {
		return time.ParseInLocation(legacyBucketLayout, origin, timeutil.Location())
	}
	seconds, err := strconv.ParseInt(origin, 10, 64)
	if err != nil || seconds <= 0 || seconds%60 != 0 {
		return time.Time{}, fmt.Errorf("bucket suffix %q names no minute", suffix)
	}
	return time.Unix(seconds, 0).In(timeutil.Location()), nil
}
