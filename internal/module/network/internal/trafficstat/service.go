// Package trafficstat keeps the records derived from the raw traffic log:
// the daily traffic statistics the dashboard and the admin log views read,
// and the retention of the raw log itself. Only the module facade may reach
// it.
package trafficstat

import (
	"context"
	"time"

	trafficEntity "github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/orm"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// pruneBatchSize bounds each delete of the retention cleanup.
const pruneBatchSize = 5000

// TrafficLog reads a day's traffic per subscription and per server, and
// deletes the raw log older than a threshold a batch at a time.
type TrafficLog interface {
	QueryUserTrafficRanking(ctx context.Context, start, end time.Time) ([]trafficEntity.UserTrafficRanking, error)
	QueryServerTrafficRanking(ctx context.Context, start, end time.Time) ([]trafficEntity.ServerTrafficRanking, error)
	orm.BatchDeleter
}

// StatLogs is where the daily statistics are recorded: the platform's system
// log, which every module may write.
type StatLogs interface {
	FindFirstByDateType(ctx context.Context, date string, typ uint8) (*log.SystemLog, error)
	InsertBatch(ctx context.Context, data []*log.SystemLog, batchSize int) error
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Traffic TrafficLog
	Logs    StatLogs
}

// Service is the traffic statistics entry point used by the network facade.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// RecordDailyStatistics records the previous day's traffic, in the
// application's zone: a row per subscription and per server, their top-10
// rankings and the day's total. The rows are written in one batch ending with
// the total, so a day whose total is recorded is skipped: a second run (another
// replica's tick, or a replay) inserts nothing.
func (s *Service) RecordDailyStatistics(ctx context.Context) error {
	now := timeutil.Now()
	start := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, timeutil.Location())
	end := start.Add(24 * time.Hour)
	date := start.Format(time.DateOnly)

	recorded, err := s.deps.Logs.FindFirstByDateType(ctx, date, log.TypeTrafficStat.Uint8())
	if err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "query the recorded traffic stat of %s", date)
	}
	if recorded != nil {
		logger.WithContext(ctx).Infof("[Traffic Stat] Traffic of %s already recorded, skipping", date)
		return nil
	}

	// The traffic is read before the write; all the day's rows are then
	// persisted with batched INSERTs instead of one per user or server.
	userTraffic, err := s.deps.Traffic.QueryUserTrafficRanking(ctx, start, end)
	if err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "query the user traffic of %s", date)
	}
	serverTraffic, err := s.deps.Traffic.QueryServerTrafficRanking(ctx, start, end)
	if err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "query the server traffic of %s", date)
	}
	rows, err := dailyRows(date, userTraffic, serverTraffic)
	if err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "encode the traffic stat of %s", date)
	}
	if err := s.deps.Logs.InsertBatch(ctx, rows, 1000); err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "record the traffic stat of %s", date)
	}
	logger.WithContext(ctx).Infof("[Traffic Stat] Recorded the traffic of %s in %s", date, time.Since(now).String())
	return nil
}

// marshaler is a log content that encodes itself.
type marshaler interface {
	Marshal() ([]byte, error)
}

// dailyRows builds a day's statistics rows; the day's total comes last.
func dailyRows(date string, userTraffic []trafficEntity.UserTrafficRanking, serverTraffic []trafficEntity.ServerTrafficRanking) ([]*log.SystemLog, error) {
	rows := make([]*log.SystemLog, 0, len(userTraffic)+len(serverTraffic)+3)
	add := func(kind log.Type, objectID int64, content marshaler) error {
		encoded, err := content.Marshal()
		if err != nil {
			return err
		}
		rows = append(rows, &log.SystemLog{Type: kind.Uint8(), Date: date, ObjectID: objectID, Content: string(encoded)})
		return nil
	}

	userTop10 := log.UserTrafficRank{Rank: make(map[uint8]log.UserTraffic)}
	stat := log.TrafficStat{}
	for i, traffic := range userTraffic {
		item := log.UserTraffic{SubscribeId: traffic.SubscribeId, UserId: traffic.UserId, Upload: traffic.Upload, Download: traffic.Download, Total: traffic.Total}
		if i < 10 {
			userTop10.Rank[uint8(i+1)] = item
		}
		stat.Upload += item.Upload
		stat.Download += item.Download
		if err := add(log.TypeSubscribeTraffic, item.SubscribeId, &item); err != nil {
			return nil, err
		}
	}
	stat.Total = stat.Upload + stat.Download
	if err := add(log.TypeUserTrafficRank, 0, &userTop10); err != nil {
		return nil, err
	}

	serverTop10 := log.ServerTrafficRank{Rank: make(map[uint8]log.ServerTraffic)}
	for i, traffic := range serverTraffic {
		item := log.ServerTraffic{ServerId: traffic.ServerId, Upload: traffic.Upload, Download: traffic.Download, Total: traffic.Total}
		if i < 10 {
			serverTop10.Rank[uint8(i+1)] = item
		}
		if err := add(log.TypeServerTraffic, item.ServerId, &item); err != nil {
			return nil, err
		}
	}
	if err := add(log.TypeServerTrafficRank, 0, &serverTop10); err != nil {
		return nil, err
	}
	if err := add(log.TypeTrafficStat, 0, &stat); err != nil {
		return nil, err
	}
	return rows, nil
}

// PruneTrafficLogs deletes the raw traffic log older than before, batch by
// batch, and returns how many rows it deleted.
func (s *Service) PruneTrafficLogs(ctx context.Context, before time.Time) (int64, error) {
	deleted, err := orm.DeleteBefore(ctx, s.deps.Traffic, before, pruneBatchSize)
	if err != nil {
		return deleted, xerr.Wrapf(err, xerr.DatabaseDeletedError, "delete the traffic logs before %s", before.Format(time.DateOnly))
	}
	return deleted, nil
}
