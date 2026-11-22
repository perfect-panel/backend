package auditlog

import (
	"context"
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/internal/readmodel"
	"github.com/perfect-panel/backend/internal/module/platform/internal/repo"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// rangeTrafficReader ranks three servers and three users today.
type rangeTrafficReader struct{}

var _ TrafficReader = rangeTrafficReader{}

func (rangeTrafficReader) QueryServerTrafficRanking(context.Context, time.Time, time.Time) ([]readmodel.ServerTrafficRanking, error) {
	return []readmodel.ServerTrafficRanking{{ServerId: 1, Total: 100}, {ServerId: 2, Total: 200}, {ServerId: 3, Total: 300}}, nil
}
func (rangeTrafficReader) QueryUserTrafficRanking(context.Context, time.Time, time.Time) ([]readmodel.UserTrafficRanking, error) {
	return []readmodel.UserTrafficRanking{{UserId: 1, Total: 100}, {UserId: 2, Total: 200}, {UserId: 3, Total: 300}}, nil
}
func (rangeTrafficReader) QueryTrafficLogDetails(context.Context, *readmodel.TrafficLogDetailsFilter) ([]*readmodel.TrafficLog, int64, error) {
	return nil, 0, nil
}

func TestTrafficRangePagination(t *testing.T) {
	today := timeutil.Now().Format(time.DateOnly)
	yesterday := timeutil.Now().AddDate(0, 0, -1).Format(time.DateOnly)
	service := NewService(Deps{Logs: repo.NewLogRepo(seedTrafficHistory(t, today, yesterday)), Traffic: rangeTrafficReader{}})
	for _, tc := range []struct {
		name   string
		params dto.FilterLogParams
		total  int64
		live   int
	}{
		{"range", dto.FilterLogParams{StartDate: yesterday, EndDate: today}, 8, 3},
		{"today legacy", dto.FilterLogParams{Date: today}, 3, 3},
		{"history legacy", dto.FilterLogParams{Date: yesterday}, 5, 0},
		{"history end", dto.FilterLogParams{EndDate: yesterday}, 5, 0},
		{"today start", dto.FilterLogParams{StartDate: today}, 3, 3},
		{"no filter", dto.FilterLogParams{}, 8, 3},
		{"range precedence", dto.FilterLogParams{Date: today, StartDate: yesterday, EndDate: yesterday}, 5, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, size := range []int{1, 2, 4, 10} {
				count, live := walkTrafficPages(t, service, tc.params, tc.total, size, today)
				if count != int(tc.total) || live != tc.live {
					t.Fatalf("size=%d: count=%d live=%d", size, count, live)
				}
			}
		})
	}
}

// seedTrafficHistory records five servers' and five subscriptions' traffic
// of yesterday, plus a stray row of today that must never be served: today's
// traffic is ranked live.
func seedTrafficHistory(t *testing.T, today, yesterday string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&log.SystemLog{}); err != nil {
		t.Fatal(err)
	}
	for _, typ := range []log.Type{log.TypeServerTraffic, log.TypeSubscribeTraffic} {
		for i := 1; i <= 5; i++ {
			if err := db.Create(&log.SystemLog{Type: typ.Uint8(), ObjectID: int64(i), Date: yesterday, Content: `{"total":42}`}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Create(&log.SystemLog{Type: typ.Uint8(), Date: today, Content: `{"total":999}`}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// walkTrafficPages reads every page of size of the server and the user
// traffic views under params, checking each page's length against total, and
// counts the server rows served and those of today.
func walkTrafficPages(t *testing.T, service *Service, params dto.FilterLogParams, total int64, size int, today string) (count, live int) {
	t.Helper()
	for page := 1; page <= int(total)/size+2; page++ {
		params.Page = page
		params.Size = size
		wantLen := min(size, max(0, int(total)-(page-1)*size))
		got, err := service.FilterServerTrafficLog(context.Background(), &dto.FilterServerTrafficLogRequest{FilterLogParams: params})
		if err != nil {
			t.Fatal(err)
		}
		if got.Total != total || len(got.List) != wantLen {
			t.Fatalf("size=%d page=%d total=%d len=%d", size, page, got.Total, len(got.List))
		}
		for _, row := range got.List {
			if row.Total == 999 {
				t.Fatal("the stray persisted row of today was served")
			}
			count++
			if row.Date == today {
				live++
			}
		}
		users, err := service.FilterUserSubscribeTrafficLog(context.Background(), &dto.FilterSubscribeTrafficRequest{FilterLogParams: params})
		if err != nil || users.Total != total || len(users.List) != wantLen {
			t.Fatalf("user traffic=%+v err=%v", users, err)
		}
	}
	return count, live
}

// detailsRangeReader records the details filter and finds no entries.
type detailsRangeReader struct {
	filter *readmodel.TrafficLogDetailsFilter
}

var _ TrafficReader = (*detailsRangeReader)(nil)

func (r *detailsRangeReader) QueryServerTrafficRanking(context.Context, time.Time, time.Time) ([]readmodel.ServerTrafficRanking, error) {
	return nil, nil
}
func (r *detailsRangeReader) QueryUserTrafficRanking(context.Context, time.Time, time.Time) ([]readmodel.UserTrafficRanking, error) {
	return nil, nil
}
func (r *detailsRangeReader) QueryTrafficLogDetails(_ context.Context, filter *readmodel.TrafficLogDetailsFilter) ([]*readmodel.TrafficLog, int64, error) {
	r.filter = filter
	return nil, 0, nil
}
func TestTrafficDetailsDateRange(t *testing.T) {
	for _, tc := range []struct {
		name       string
		params     dto.FilterLogParams
		start, end string
	}{
		{"inclusive end", dto.FilterLogParams{StartDate: "2026-01-01", EndDate: "2026-01-03"}, "2026-01-01", "2026-01-04"},
		{"start only", dto.FilterLogParams{StartDate: "2026-01-01", Date: "2025-01-01"}, "2026-01-01", ""},
		{"end only", dto.FilterLogParams{EndDate: "2026-01-03"}, "", "2026-01-04"},
		{"legacy", dto.FilterLogParams{Date: "2026-01-01"}, "2026-01-01", "2026-01-02"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &detailsRangeReader{}
			_, err := NewService(Deps{Traffic: reader}).FilterTrafficLogDetails(context.Background(), &dto.FilterTrafficLogDetailsRequest{FilterLogParams: tc.params})
			if err != nil {
				t.Fatal(err)
			}
			for _, bound := range []struct {
				got  time.Time
				want string
			}{{reader.filter.Start, tc.start}, {reader.filter.End, tc.end}} {
				if bound.want == "" {
					if !bound.got.IsZero() {
						t.Fatal(bound.got)
					}
					continue
				}
				want, err := time.ParseInLocation(time.DateOnly, bound.want, timeutil.Location())
				if err != nil || !bound.got.Equal(want) {
					t.Fatalf("got=%v want=%v err=%v", bound.got, want, err)
				}
			}
		})
	}
}
