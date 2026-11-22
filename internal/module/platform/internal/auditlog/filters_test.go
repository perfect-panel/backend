package auditlog

import (
	"context"
	"fmt"
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/internal/readmodel"
	"github.com/perfect-panel/backend/internal/module/platform/internal/repo"
	"github.com/perfect-panel/backend/internal/repository/kernel"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// stubLogRepo is the log repository answering every filter from rows; the
// other methods are never called.
type stubLogRepo struct {
	kernel.LogRepo
	rows *pageRows
}

func (s stubLogRepo) FilterSystemLog(ctx context.Context, filter *log.FilterParams) ([]*log.SystemLog, int64, error) {
	return s.rows.FilterSystemLog(ctx, filter)
}

// goldenMetadata is the request an entry under test came from.
func goldenMetadata() requestmeta.Metadata {
	return requestmeta.Metadata{
		ClientIP: "203.0.113.7", UserAgent: "RiskClient/1.0", ActorID: 9,
		IPMetadata: requestmeta.IPMetadata{IPCountryCode: "SG", IPCountry: "Singapore", IPRegion: "Central", IPCity: "Singapore", IPASN: 13335, IPASOrganization: "Cloudflare"},
	}
}

func newLogDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:auditlog-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := db.AutoMigrate(&log.SystemLog{}); err != nil {
		t.Fatal(err)
	}
	return db
}

// A subscription filter matches the subscription itself: the fetches of
// subscription 12 are not those of 120 or 1200, which a text pattern for
// "12" would also find.
func TestFilterSubscribeLogMatchesTheSubscriptionExactly(t *testing.T) {
	db := newLogDB(t)
	for _, id := range []int64{12, 120, 1200, 12} {
		content, err := (&log.Subscribe{Token: "t", UserSubscribeId: id}).Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&log.SystemLog{Type: log.TypeSubscribe.Uint8(), ObjectID: 7, Date: "2026-09-01", Content: string(content)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(Deps{Logs: repo.NewLogRepo(db)})
	got, err := svc.FilterSubscribeLog(context.Background(), &dto.FilterSubscribeLogRequest{FilterLogParams: dto.FilterLogParams{Page: 1, Size: 10}, UserId: 7, UserSubscribeId: 12})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 2 || len(got.List) != 2 {
		t.Fatalf("fetches of subscription 12 = %d (%d listed), want the two", got.Total, len(got.List))
	}
	for _, row := range got.List {
		if row.UserSubscribeId != 12 {
			t.Fatalf("listed subscription %d, want 12 only", row.UserSubscribeId)
		}
	}
	all, err := svc.FilterSubscribeLog(context.Background(), &dto.FilterSubscribeLogRequest{FilterLogParams: dto.FilterLogParams{Page: 1, Size: 10}, UserId: 7})
	if err != nil || all.Total != 4 {
		t.Fatalf("all fetches = %+v (err %v), want the four", all, err)
	}
}

// narrowTrafficReader ranks two users' subscriptions today.
type narrowTrafficReader struct{}

var _ TrafficReader = narrowTrafficReader{}

func (narrowTrafficReader) QueryServerTrafficRanking(context.Context, time.Time, time.Time) ([]readmodel.ServerTrafficRanking, error) {
	return nil, nil
}
func (narrowTrafficReader) QueryUserTrafficRanking(context.Context, time.Time, time.Time) ([]readmodel.UserTrafficRanking, error) {
	return []readmodel.UserTrafficRanking{{UserId: 1, SubscribeId: 11, Total: 100}, {UserId: 1, SubscribeId: 12, Total: 200}, {UserId: 2, SubscribeId: 21, Total: 300}}, nil
}
func (narrowTrafficReader) QueryTrafficLogDetails(context.Context, *readmodel.TrafficLogDetailsFilter) ([]*readmodel.TrafficLog, int64, error) {
	return nil, 0, nil
}

// The user and subscription filters narrow both today's live ranking and
// the archived days: the archived row's object is its subscription and its
// user is in the content.
func TestFilterUserSubscribeTrafficLogNarrowsToUserAndSubscription(t *testing.T) {
	db := newLogDB(t)
	yesterday := timeutil.Now().AddDate(0, 0, -1).Format(time.DateOnly)
	for _, row := range []log.UserTraffic{{UserId: 1, SubscribeId: 11, Total: 10}, {UserId: 1, SubscribeId: 12, Total: 20}, {UserId: 2, SubscribeId: 21, Total: 30}} {
		content, err := row.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&log.SystemLog{Type: log.TypeSubscribeTraffic.Uint8(), ObjectID: row.SubscribeId, Date: yesterday, Content: string(content)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(Deps{Logs: repo.NewLogRepo(db), Traffic: narrowTrafficReader{}})
	for _, tc := range []struct {
		name       string
		req        dto.FilterSubscribeTrafficRequest
		total      int64
		subscribes []int64
	}{
		// Today's ranking first, then the archived rows newest first.
		{"everything", dto.FilterSubscribeTrafficRequest{FilterLogParams: dto.FilterLogParams{Page: 1, Size: 10}}, 6, []int64{11, 12, 21, 21, 12, 11}},
		{"user 1", dto.FilterSubscribeTrafficRequest{FilterLogParams: dto.FilterLogParams{Page: 1, Size: 10}, UserId: 1}, 4, []int64{11, 12, 12, 11}},
		{"subscription 21", dto.FilterSubscribeTrafficRequest{FilterLogParams: dto.FilterLogParams{Page: 1, Size: 10}, UserSubscribeId: 21}, 2, []int64{21, 21}},
		{"user 2 subscription 11", dto.FilterSubscribeTrafficRequest{FilterLogParams: dto.FilterLogParams{Page: 1, Size: 10}, UserId: 2, UserSubscribeId: 11}, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.FilterUserSubscribeTrafficLog(context.Background(), &tc.req)
			if err != nil {
				t.Fatal(err)
			}
			var subscribes []int64
			for _, row := range got.List {
				subscribes = append(subscribes, row.SubscribeId)
			}
			if got.Total != tc.total || fmt.Sprint(subscribes) != fmt.Sprint(tc.subscribes) {
				t.Fatalf("total = %d, subscriptions = %v; want %d, %v", got.Total, subscribes, tc.total, tc.subscribes)
			}
		})
	}
}

// The administrators' trail decodes every field of the action, filed under
// the administrator with the request it came from.
func TestFilterAdminActionLogDecodesEntries(t *testing.T) {
	http, err := (&log.AdminAction{Metadata: goldenMetadata(), Action: "settings.update", Object: "verify", Detail: "keys: TurnstileSecret", Source: log.AdminActionSourceHTTP, Timestamp: 1700000000000}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	telegram, err := (&log.AdminAction{Action: "user.ban", Object: "user", ObjectID: 42, Detail: "enabled=false", Source: log.AdminActionSourceTelegram, TelegramSenderID: 500, Timestamp: 1700000001000}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	logs := &pageRows{rows: []*log.SystemLog{
		{Id: 1, ObjectID: 9, Content: string(http), CreatedAt: created},
		{Id: 2, ObjectID: 9, Content: string(telegram), CreatedAt: created},
	}}
	got, err := NewService(Deps{Logs: stubLogRepo{rows: logs}}).FilterAdminActionLog(context.Background(), &dto.FilterAdminActionLogRequest{FilterLogParams: dto.FilterLogParams{Page: 1, Size: 10, Search: "settings"}, UserId: 9})
	if err != nil {
		t.Fatal(err)
	}
	if logs.filter.Type != log.TypeAdminAction.Uint8() || logs.filter.ObjectID != 9 || logs.filter.Search != "settings" {
		t.Fatalf("filter = %+v, want the trail of administrator 9 searched", logs.filter)
	}
	if got.Total != 2 || len(got.List) != 2 {
		t.Fatalf("page = %+v", got)
	}
	first, second := got.List[0], got.List[1]
	if first.Id != 1 || first.UserId != 9 || first.Action != "settings.update" || first.Object != "verify" || first.Detail != "keys: TurnstileSecret" ||
		first.Source != "http" || first.Timestamp != 1700000000000 || first.CreatedAt != created.UnixMilli() || first.ClientIP != "203.0.113.7" || first.IPCountryCode != "SG" {
		t.Fatalf("http action = %+v", first)
	}
	if second.Action != "user.ban" || second.ObjectId != 42 || second.Source != "telegram" || second.TelegramSenderId != 500 || second.ClientIP != "" {
		t.Fatalf("telegram action = %+v", second)
	}
	empty, err := NewService(Deps{Logs: stubLogRepo{rows: &pageRows{}}}).FilterAdminActionLog(context.Background(), &dto.FilterAdminActionLogRequest{FilterLogParams: dto.FilterLogParams{Page: 1, Size: 10}})
	if err != nil || empty.List == nil || len(empty.List) != 0 {
		t.Fatalf("empty trail = %+v (err %v), want an empty list", empty, err)
	}
}

// The unmatched payments decode every field an operator refunds from.
func TestFilterUnmatchedPaymentLogDecodesEntries(t *testing.T) {
	content, err := (&log.UnmatchedPayment{Metadata: goldenMetadata(), OrderNo: "o-9", TradeNo: "t-9", Platform: "alipay", Amount: 1200, Currency: "CNY", Reason: "order already closed", Timestamp: 1700000000000}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	logs := &pageRows{rows: []*log.SystemLog{{Id: 5, ObjectID: 7, Content: string(content), CreatedAt: created}}}
	got, err := NewService(Deps{Logs: stubLogRepo{rows: logs}}).FilterUnmatchedPaymentLog(context.Background(), &dto.FilterUnmatchedPaymentLogRequest{FilterLogParams: dto.FilterLogParams{Page: 1, Size: 10, Search: "o-9"}, UserId: 7})
	if err != nil {
		t.Fatal(err)
	}
	if logs.filter.Type != log.TypeUnmatchedPayment.Uint8() || logs.filter.ObjectID != 7 || logs.filter.Search != "o-9" {
		t.Fatalf("filter = %+v", logs.filter)
	}
	if got.Total != 1 || len(got.List) != 1 {
		t.Fatalf("page = %+v", got)
	}
	want := dto.UnmatchedPaymentLog{Id: 5, UserId: 7, OrderNo: "o-9", TradeNo: "t-9", Platform: "alipay", Amount: 1200, Currency: "CNY", Reason: "order already closed",
		Timestamp: 1700000000000, CreatedAt: created.UnixMilli(), ClientIP: "203.0.113.7", UserAgent: "RiskClient/1.0", ActorID: 9,
		IPCountryCode: "SG", IPCountry: "Singapore", IPRegion: "Central", IPCity: "Singapore", IPASN: 13335, IPASOrganization: "Cloudflare"}
	if got.List[0] != want {
		t.Fatalf("payment = %+v, want %+v", got.List[0], want)
	}
	empty, err := NewService(Deps{Logs: stubLogRepo{rows: &pageRows{}}}).FilterUnmatchedPaymentLog(context.Background(), &dto.FilterUnmatchedPaymentLogRequest{FilterLogParams: dto.FilterLogParams{Page: 1, Size: 10}})
	if err != nil || empty.List == nil || len(empty.List) != 0 {
		t.Fatalf("empty list = %+v (err %v), want an empty list", empty, err)
	}
}
