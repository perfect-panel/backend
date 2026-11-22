package trafficstat

import (
	"context"
	"errors"
	"testing"
	"time"

	trafficEntity "github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
)

// statTraffic ranks one subscription and one server, and records the
// retention deletes.
type statTraffic struct {
	queries int
	before  time.Time
	left    int
	err     error
}

var _ TrafficLog = (*statTraffic)(nil)

func (r *statTraffic) QueryUserTrafficRanking(context.Context, time.Time, time.Time) ([]trafficEntity.UserTrafficRanking, error) {
	r.queries++
	return []trafficEntity.UserTrafficRanking{{SubscribeId: 9, UserId: 3, Upload: 5, Download: 7, Total: 12}}, nil
}

func (r *statTraffic) QueryServerTrafficRanking(context.Context, time.Time, time.Time) ([]trafficEntity.ServerTrafficRanking, error) {
	return []trafficEntity.ServerTrafficRanking{{ServerId: 4, Upload: 5, Download: 7, Total: 12}}, nil
}

func (r *statTraffic) DeleteBeforeBatch(_ context.Context, before time.Time, limit int) (int64, error) {
	r.before = before
	if r.err != nil {
		return 0, r.err
	}
	n := min(r.left, limit)
	r.left -= n
	return int64(n), nil
}

// statLogs holds the recorded rows.
type statLogs struct {
	rows []*log.SystemLog
}

var _ StatLogs = (*statLogs)(nil)

func (r *statLogs) InsertBatch(_ context.Context, rows []*log.SystemLog, _ int) error {
	r.rows = append(r.rows, rows...)
	return nil
}

func (r *statLogs) FindFirstByDateType(_ context.Context, date string, typ uint8) (*log.SystemLog, error) {
	for _, row := range r.rows {
		if row.Date == date && row.Type == typ {
			return row, nil
		}
	}
	return nil, nil
}

// The day's statistics are recorded once: a second run for the same day
// (another replica's tick, or a replay) inserts nothing and reads no traffic.
func TestRecordDailyStatisticsRecordsEachDayOnce(t *testing.T) {
	traffic, logs := &statTraffic{}, &statLogs{}
	s := NewService(Deps{Traffic: traffic, Logs: logs})
	for range 2 {
		if err := s.RecordDailyStatistics(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	// One subscription row, the user ranking, one server row, the server
	// ranking and the day's total, which comes last.
	want := []log.Type{log.TypeSubscribeTraffic, log.TypeUserTrafficRank, log.TypeServerTraffic, log.TypeServerTrafficRank, log.TypeTrafficStat}
	if len(logs.rows) != len(want) || traffic.queries != 1 {
		t.Fatalf("rows = %d, ranking queries = %d; want one day's %d rows from one run", len(logs.rows), traffic.queries, len(want))
	}
	for i, kind := range want {
		if logs.rows[i].Type != kind.Uint8() {
			t.Fatalf("row %d type = %d, want %d", i, logs.rows[i].Type, kind.Uint8())
		}
	}
	if logs.rows[0].ObjectID != 9 || logs.rows[2].ObjectID != 4 {
		t.Fatalf("object ids = %d/%d, want the subscription 9 and the server 4", logs.rows[0].ObjectID, logs.rows[2].ObjectID)
	}
	var total log.TrafficStat
	if err := total.Unmarshal([]byte(logs.rows[4].Content)); err != nil || total.Upload != 5 || total.Download != 7 || total.Total != 12 {
		t.Fatalf("day total = %+v (%v), want 5 up, 7 down, 12 in all", total, err)
	}
}

func TestPruneTrafficLogsDeletesBatchByBatch(t *testing.T) {
	before := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	traffic := &statTraffic{left: pruneBatchSize + 3}
	s := NewService(Deps{Traffic: traffic, Logs: &statLogs{}})
	deleted, err := s.PruneTrafficLogs(context.Background(), before)
	if err != nil || deleted != pruneBatchSize+3 || !traffic.before.Equal(before) {
		t.Fatalf("deleted %d (err %v, before %v); want every row before %v", deleted, err, traffic.before, before)
	}

	traffic.err = errors.New("delete failed")
	if _, err := s.PruneTrafficLogs(context.Background(), before); !errors.Is(err, traffic.err) {
		t.Fatalf("prune error = %v, want the delete failure", err)
	}
}
