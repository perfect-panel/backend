package auditlog

import (
	"context"
	"fmt"
	"time"

	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/orm"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

const (
	// cleanupBatchSize bounds each delete of the retention cleanup.
	cleanupBatchSize = 5000
	// maxRetentionDays bounds the retention the cleanup accepts, so a corrupt
	// setting cannot make it delete the whole log.
	maxRetentionDays = 3650
)

// TrafficLogPruner is the subdomain's port onto the network domain's raw
// traffic log, whose retention follows the log settings; the composition
// root adapts the network facade to it.
type TrafficLogPruner interface {
	PruneTrafficLogs(ctx context.Context, before time.Time) (int64, error)
}

// CleanupLogs applies the log retention settings: with automatic clearing
// on, it deletes the raw traffic log and the system log older than the
// configured number of days, counted back from the start of today in the
// application's zone. It runs on its own schedule, apart from the traffic
// statistics, so a failure of one cannot silently disable the other.
func (s *Service) CleanupLogs(ctx context.Context) error {
	autoClear, days := s.deps.logRetention()
	return cleanupLogs(ctx, autoClear, days, s.deps.TrafficRetention, s.deps.Logs)
}

func cleanupLogs(ctx context.Context, autoClear bool, days int64, traffic TrafficLogPruner, logs orm.BatchDeleter) error {
	if !autoClear || traffic == nil || logs == nil {
		return nil
	}
	if days < 1 || days > maxRetentionDays {
		return fmt.Errorf("invalid log retention days: %d", days)
	}

	now := timeutil.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	threshold := today.AddDate(0, 0, -int(days))
	trafficDeleted, err := traffic.PruneTrafficLogs(ctx, threshold)
	if err != nil {
		return err
	}
	logsDeleted, err := orm.DeleteBefore(ctx, logs, threshold, cleanupBatchSize)
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseDeletedError, "delete the system logs before %s", threshold.Format(time.DateOnly))
	}
	logger.WithContext(ctx).Infow("[Log Cleanup] cleanup completed",
		logger.Field("threshold", threshold.Format(time.DateOnly)),
		logger.Field("traffic_deleted", trafficDeleted),
		logger.Field("logs_deleted", logsDeleted))
	return nil
}
