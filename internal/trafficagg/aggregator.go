package trafficagg

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/model/entity/node"
	trafficEntity "github.com/perfect-panel/server/internal/model/entity/traffic"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/redis/go-redis/v9"
)

const (
	bucketLayout             = "200601021504"
	bucketTTL                = 24 * time.Hour
	deadLetterTTL            = 30 * 24 * time.Hour
	bucketFailureThreshold   = 10
	bucketIndexKey           = "traffic:agg:buckets"
	bucketPrefix             = "traffic:agg:"
	processingBucketPrefix   = "traffic:processing:"
	processedBucketPrefix    = "traffic:processed:"
	bucketFailurePrefix      = "traffic:failed:"
	deadLetterBucketPrefix   = "traffic:deadletter:"
	deadLetterMetaPrefix     = "traffic:deadletter:meta:"
	deadLetterIndexKey       = "traffic:deadletter:buckets"
	serverLastReportedKey    = "traffic:server:last_reported"
	conditionalHDelLua       = `for i = 1, #ARGV, 2 do if redis.call("HGET", KEYS[1], ARGV[i]) == ARGV[i + 1] then redis.call("HDEL", KEYS[1], ARGV[i]) end end return 1`
	trafficFieldUpload       = "u"
	trafficFieldDownload     = "d"
	trafficFieldSeparator    = "|"
	defaultTrafficBatchSize  = 1000
	defaultTrafficMultiplier = 1
)

type UserTraffic struct {
	SID      int64
	Upload   int64
	Download int64
}

type Aggregator struct {
	svc *svc.ServiceContext
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

func New(svcCtx *svc.ServiceContext) *Aggregator {
	return &Aggregator{svc: svcCtx}
}

func (a *Aggregator) AddReport(ctx context.Context, serverInfo *node.Server, protocol string, logs []UserTraffic) error {
	return a.AddReportAt(ctx, serverInfo, protocol, logs, timeutil.Now())
}

func (a *Aggregator) AddReportAt(ctx context.Context, serverInfo *node.Server, protocol string, logs []UserTraffic, now time.Time) error {
	if a == nil || a.svc == nil || a.svc.Redis == nil {
		return errors.New("traffic aggregator is not initialized")
	}
	if serverInfo == nil || serverInfo.Id <= 0 {
		return errors.New("server not found")
	}

	pipe := a.svc.Redis.Pipeline()
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

	minute := now.In(timeutil.Location()).Truncate(time.Minute)
	suffix := minute.Format(bucketLayout)
	bucketKey := bucketPrefix + suffix
	multiplier := float32(defaultTrafficMultiplier)
	if a.svc.NodeMultiplierManager != nil {
		multiplier = a.svc.NodeMultiplierManager.GetMultiplier(now)
	}

	trafficOps := 0
	for _, item := range logs {
		if item.SID <= 0 {
			continue
		}
		if item.Download+item.Upload <= a.svc.Config.Node.TrafficReportThreshold {
			continue
		}
		download := int64(float32(item.Download) * ratio * multiplier)
		upload := int64(float32(item.Upload) * ratio * multiplier)
		pipe.HIncrBy(ctx, bucketKey, trafficField(serverInfo.Id, item.SID, trafficFieldDownload), download)
		pipe.HIncrBy(ctx, bucketKey, trafficField(serverInfo.Id, item.SID, trafficFieldUpload), upload)
		trafficOps++
	}

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

func (a *Aggregator) FlushDueBuckets(ctx context.Context, now time.Time) error {
	if a == nil || a.svc == nil || a.svc.Redis == nil {
		return errors.New("traffic aggregator is not initialized")
	}

	maxScore := now.In(timeutil.Location()).Truncate(time.Minute).Unix() - 1
	buckets, err := a.svc.Redis.ZRangeByScore(ctx, bucketIndexKey, &redis.ZRangeBy{
		Min: "-inf",
		Max: strconv.FormatInt(maxScore, 10),
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

	if err := a.FlushServerReports(ctx); err != nil {
		logger.WithContext(ctx).Error("[TrafficAggregator] Flush server reports failed", logger.Field("error", err.Error()))
		errs = append(errs, "server_reports: "+err.Error())
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (a *Aggregator) FlushServerReports(ctx context.Context) error {
	values, err := a.svc.Redis.HGetAll(ctx, serverLastReportedKey).Result()
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}

	reports := make(map[int64]time.Time, len(values))
	args := make([]interface{}, 0, len(values)*2)
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

	if err := a.svc.Store.Node().BatchUpdateServerLastReportedAt(ctx, reports); err != nil {
		return err
	}
	return redis.NewScript(conditionalHDelLua).Run(ctx, a.svc.Redis, []string{serverLastReportedKey}, args...).Err()
}

func (a *Aggregator) flushBucket(ctx context.Context, suffix string) error {
	if _, err := parseBucketTime(suffix); err != nil {
		pipe := a.svc.Redis.Pipeline()
		pipe.ZRem(ctx, bucketIndexKey, suffix)
		pipe.Del(ctx, bucketFailureKey(suffix))
		_, _ = pipe.Exec(ctx)
		return nil
	}

	aggregateKey := bucketPrefix + suffix
	processingKey := processingBucketPrefix + suffix
	processedKey := processedBucketPrefix + suffix
	processed, err := a.svc.Redis.Exists(ctx, processedKey).Result()
	if err != nil {
		return err
	}
	if processed > 0 {
		return a.cleanupBucket(ctx, suffix, processingKey)
	}

	exists, err := a.svc.Redis.Exists(ctx, processingKey).Result()
	if err != nil {
		return err
	}
	if exists == 0 {
		renamed, err := a.svc.Redis.RenameNX(ctx, aggregateKey, processingKey).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) || strings.Contains(err.Error(), "no such key") {
				_ = a.svc.Redis.ZRem(ctx, bucketIndexKey, suffix).Err()
				return nil
			}
			return err
		}
		if !renamed {
			return nil
		}
	}

	values, err := a.svc.Redis.HGetAll(ctx, processingKey).Result()
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return a.cleanupBucket(ctx, suffix, processingKey)
	}

	deltas := parseTrafficDeltas(ctx, values)
	if len(deltas) == 0 {
		return a.cleanupBucket(ctx, suffix, processingKey)
	}

	if err := a.persistBucket(ctx, suffix, deltas); err != nil {
		return a.handleBucketFlushFailure(ctx, suffix, processingKey, err)
	}
	return a.markBucketProcessedAndCleanup(ctx, suffix, processingKey, processedKey)
}

func (a *Aggregator) cleanupBucket(ctx context.Context, suffix, processingKey string) error {
	pipe := a.svc.Redis.Pipeline()
	pipe.Del(ctx, processingKey)
	pipe.ZRem(ctx, bucketIndexKey, suffix)
	pipe.Del(ctx, bucketFailureKey(suffix))
	_, err := pipe.Exec(ctx)
	return err
}

func (a *Aggregator) markBucketProcessedAndCleanup(ctx context.Context, suffix, processingKey, processedKey string) error {
	pipe := a.svc.Redis.Pipeline()
	pipe.Set(ctx, processedKey, "1", bucketTTL)
	pipe.Del(ctx, processingKey)
	pipe.ZRem(ctx, bucketIndexKey, suffix)
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
	failures, err := a.svc.Redis.Incr(ctx, failureKey).Result()
	if err != nil {
		return 0, err
	}
	if err := a.svc.Redis.Expire(ctx, failureKey, deadLetterTTL).Err(); err != nil {
		return 0, err
	}
	return failures, nil
}

func (a *Aggregator) moveBucketToDeadLetter(ctx context.Context, suffix, processingKey string, failures int64, cause error) error {
	fieldCount, _ := a.svc.Redis.HLen(ctx, processingKey).Result()
	deadLetterKey, err := a.renameProcessingBucketToDeadLetter(ctx, suffix, processingKey)
	if err != nil {
		return err
	}

	now := timeutil.Now()
	causeText := ""
	if cause != nil {
		causeText = cause.Error()
	}
	metaKey := deadLetterMetaKey(deadLetterKey)
	pipe := a.svc.Redis.Pipeline()
	pipe.HSet(ctx, metaKey, map[string]interface{}{
		"bucket":         suffix,
		"source_key":     processingKey,
		"deadletter_key": deadLetterKey,
		"failure_count":  failures,
		"field_count":    fieldCount,
		"last_error":     causeText,
		"moved_at":       now.Format(time.RFC3339Nano),
	})
	pipe.Expire(ctx, metaKey, deadLetterTTL)
	pipe.Expire(ctx, deadLetterKey, deadLetterTTL)
	pipe.ZAdd(ctx, deadLetterIndexKey, redis.Z{
		Score:  bucketScore(suffix),
		Member: deadLetterKey,
	})
	pipe.Expire(ctx, deadLetterIndexKey, deadLetterTTL)
	pipe.ZRem(ctx, bucketIndexKey, suffix)
	pipe.Del(ctx, bucketFailureKey(suffix))
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}

	logger.WithContext(ctx).Error("[TrafficAggregator] Bucket moved to deadletter",
		logger.Field("bucket", suffix),
		logger.Field("failures", failures),
		logger.Field("deadletter_key", deadLetterKey),
		logger.Field("field_count", fieldCount),
		logger.Field("error", causeText),
	)
	return nil
}

func (a *Aggregator) renameProcessingBucketToDeadLetter(ctx context.Context, suffix, processingKey string) (string, error) {
	deadLetterKey := deadLetterBucketKey(suffix)
	renamed, err := a.svc.Redis.RenameNX(ctx, processingKey, deadLetterKey).Result()
	if err != nil {
		return "", err
	}
	if renamed {
		return deadLetterKey, nil
	}

	for i := 0; i < 3; i++ {
		candidate := fmt.Sprintf("%s:%d:%d", deadLetterKey, timeutil.Now().UnixNano(), i)
		renamed, err = a.svc.Redis.RenameNX(ctx, processingKey, candidate).Result()
		if err != nil {
			return "", err
		}
		if renamed {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("deadletter key already exists for bucket %s", suffix)
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

	subs, err := a.svc.Store.UserSubscription().FindSubscribesByIds(ctx, subscribeIDs)
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

	return a.svc.Store.InTx(ctx, func(store repository.Store) error {
		if err := store.UserSubscription().BatchUpdateUserSubscribeWithTraffic(ctx, updates); err != nil {
			return err
		}
		return store.TrafficLog().InsertBatch(ctx, logs, defaultTrafficBatchSize)
	})
}

func protocolRatio(serverInfo *node.Server, protocol string) (float32, bool, error) {
	protocols, err := serverInfo.UnmarshalProtocols()
	if err != nil {
		return 0, false, err
	}
	for _, item := range protocols {
		if strings.EqualFold(item.Type, protocol) {
			if item.Ratio > 0 {
				return float32(item.Ratio), true, nil
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

func parseBucketTime(suffix string) (time.Time, error) {
	return time.ParseInLocation(bucketLayout, suffix, timeutil.Location())
}
