package usersub

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/perfect-panel/backend/pkg/timeutil"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestStatusSet(t *testing.T) {
	if !LiveStatuses.Contains(SubscribeStatusActive) || LiveStatuses.Contains(SubscribeStatusFinished) {
		t.Fatalf("LiveStatuses membership is wrong: %v", LiveStatuses)
	}
	if got := HeldStatuses.Values(); !reflect.DeepEqual(got, []int64{4, 5}) {
		t.Fatalf("HeldStatuses.Values() = %v", got)
	}
	for status := uint8(0); status <= SubscribeStatusStopped; status++ {
		if !AllStatuses.Contains(status) {
			t.Fatalf("AllStatuses lacks %d", status)
		}
		if OnHold(status) != (status == SubscribeStatusDeducted || status == SubscribeStatusStopped) {
			t.Fatalf("OnHold(%d) is wrong", status)
		}
	}
}

// The epoch marker in the wall clocks other zones give it, as a zone-less
// column returns it to a session in another zone: written in UTC+8 and read
// in UTC, and written in UTC-5 and read in UTC.
var (
	epochWrittenEast = time.Date(1970, time.January, 1, 8, 0, 0, 0, time.UTC)
	epochWrittenWest = time.Date(1969, time.December, 31, 19, 0, 0, 0, time.UTC)
)

// NoExpiry recognises the marker whatever zone wrote or read it, so it draws
// the line a year after the epoch instead of testing for one instant.
func TestNoExpiry(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if !NoLimitExpiry().Equal(time.UnixMilli(0)) || NoLimitExpiry().Location() != timeutil.Location() {
		t.Fatalf("NoLimitExpiry() = %v, want the epoch in the application zone", NoLimitExpiry())
	}
	for name, tt := range map[string]struct {
		at   time.Time
		want bool
	}{
		"NULL reads as the zero time":             {time.Time{}, true},
		"the marker":                              {NoLimitExpiry(), true},
		"the marker in another zone":              {time.UnixMilli(0).In(shanghai), true},
		"the marker written east, read in UTC":    {epochWrittenEast, true},
		"the marker written west, read in UTC":    {epochWrittenWest, true},
		"one millisecond after epoch":             {time.UnixMilli(1), true},
		"the last instant below the bound":        {NoLimitBound().Add(-time.Millisecond), true},
		"the bound is the first real expiry":      {NoLimitBound(), false},
		"a real expiry":                           {time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), false},
		"a real expiry read in a zone behind UTC": {time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("UTC-12", -12*3600)), false},
	} {
		if got := NoExpiry(tt.at); got != tt.want {
			t.Fatalf("%s: NoExpiry = %v, want %v", name, got, tt.want)
		}
	}
}

// availabilityCase is one subscription row of the availability table; both
// the Go predicate and the SQL condition are checked against it.
type availabilityCase struct {
	name   string
	status uint8
	expire time.Time
	nullEx bool
	used   [2]int64 // upload, download
	quota  int64
	want   Availability
}

func availabilityCases(now time.Time) []availabilityCase {
	future, past := now.Add(time.Hour), now.Add(-time.Minute)
	return []availabilityCase{
		{name: "active", status: SubscribeStatusActive, expire: future, want: Available},
		{name: "legacy pending", status: SubscribeStatusPending, expire: future, want: Available},
		{name: "active no limit", status: SubscribeStatusActive, expire: NoLimitExpiry(), want: Available},
		{name: "active no limit written east of the session zone", status: SubscribeStatusActive, expire: epochWrittenEast, want: Available},
		{name: "active no limit written west of the session zone", status: SubscribeStatusActive, expire: epochWrittenWest, want: Available},
		{name: "active NULL expiry", status: SubscribeStatusActive, nullEx: true, want: Available},
		{name: "active unlimited traffic", status: SubscribeStatusActive, expire: future, used: [2]int64{1 << 40, 1}, want: Available},
		{name: "active traffic left", status: SubscribeStatusActive, expire: future, used: [2]int64{50, 49}, quota: 100, want: Available},
		{name: "active expiry equals now", status: SubscribeStatusActive, expire: now, want: Expired},
		{name: "active past expiry", status: SubscribeStatusActive, expire: past, want: Expired},
		{name: "active traffic used up", status: SubscribeStatusActive, expire: future, used: [2]int64{50, 50}, quota: 100, want: TrafficExhausted},
		{name: "past expiry and used up", status: SubscribeStatusActive, expire: past, used: [2]int64{100, 0}, quota: 100, want: Expired},
		{name: "finished after an old reset", status: SubscribeStatusFinished, expire: future, want: TrafficExhausted},
		{name: "finished", status: SubscribeStatusFinished, expire: future, used: [2]int64{0, 100}, quota: 100, want: TrafficExhausted},
		{name: "expired status", status: SubscribeStatusExpired, expire: future, want: Expired},
		{name: "deducted", status: SubscribeStatusDeducted, expire: future, want: Refunded},
		{name: "stopped", status: SubscribeStatusStopped, expire: NoLimitExpiry(), want: Stopped},
		{name: "stopped and expired", status: SubscribeStatusStopped, expire: past, want: Stopped},
		{name: "unknown status", status: 9, expire: future, want: Inactive},
	}
}

func (c availabilityCase) subscribe(id int64) Subscribe {
	sub := Subscribe{Id: id, Status: c.status, ExpireTime: c.expire, Traffic: c.quota, Upload: c.used[0], Download: c.used[1]}
	sub.Token, sub.UUID = fmt.Sprintf("token-%d", id), fmt.Sprintf("uuid-%d", id)
	return sub
}

func TestAvailabilityAt(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for i, tt := range availabilityCases(now) {
		sub := tt.subscribe(int64(i + 1))
		if tt.nullEx {
			sub.ExpireTime = time.Time{}
		}
		if got := sub.AvailabilityAt(now); got != tt.want {
			t.Fatalf("%s: AvailabilityAt = %d, want %d", tt.name, got, tt.want)
		}
		if got := sub.ServableAt(now); got != (tt.want == Available) {
			t.Fatalf("%s: ServableAt = %v", tt.name, got)
		}
	}
}

// openSubscribeTable is a fresh SQLite user_subscribe table for the test, the
// portable equivalent of the production table, closed when the test ends.
func openSubscribeTable(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "status.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec(`CREATE TABLE user_subscribe (
 id INTEGER PRIMARY KEY, user_id BIGINT, order_id BIGINT, subscribe_id BIGINT,
 start_time TIMESTAMP, expire_time TIMESTAMP, finished_at TIMESTAMP, traffic_reset_at TIMESTAMP,
 traffic BIGINT DEFAULT 0, download BIGINT DEFAULT 0, upload BIGINT DEFAULT 0,
 token VARCHAR(255) UNIQUE, uuid VARCHAR(255) UNIQUE, status INTEGER DEFAULT 0,
 note TEXT, entitlement_source VARCHAR(32) NOT NULL DEFAULT '', created_at TIMESTAMP, updated_at TIMESTAMP)`).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

// selectIDs returns the ids of the user_subscribe rows condition selects.
func selectIDs(t *testing.T, db *gorm.DB, condition string, args ...any) []int64 {
	t.Helper()
	var ids []int64
	if err := db.Model(&Subscribe{}).Where(condition, args...).Order("id").Pluck("id", &ids).Error; err != nil {
		t.Fatal(err)
	}
	return ids
}

// ServableCondition selects exactly the rows ServableAt accepts. The rows are
// written and read through SQLite, so NULL expiries and the epoch sentinel go
// through a real driver.
func TestServableConditionAgreesWithServableAt(t *testing.T) {
	db := openSubscribeTable(t)
	now := time.Now().Truncate(time.Millisecond)
	cases := availabilityCases(now)
	var want []int64
	for i, tt := range cases {
		sub := tt.subscribe(int64(i + 1))
		if err := db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
		if tt.nullEx {
			if err := db.Exec("UPDATE user_subscribe SET expire_time = NULL WHERE id = ?", sub.Id).Error; err != nil {
				t.Fatal(err)
			}
		}
		var stored Subscribe
		if err := db.First(&stored, sub.Id).Error; err != nil {
			t.Fatal(err)
		}
		if stored.ServableAt(now) {
			want = append(want, sub.Id)
		}
	}
	// Rows with NULL counters are never exhausted in either form.
	if err := db.Exec("INSERT INTO user_subscribe (id, status, expire_time, traffic, upload, download, token, uuid) VALUES (100, 1, NULL, 10, NULL, NULL, 't100', 'u100')").Error; err != nil {
		t.Fatal(err)
	}
	want = append(want, 100)

	condition, args := ServableCondition(now)
	got := selectIDs(t, db, condition, args...)
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SQL servable ids = %v, Go servable ids = %v", got, want)
	}
}

// ExpiredCondition and UnexpiredCondition split the rows the way ExpiredAt
// does, and ExpiringCondition finds only real term ends: the no-limit marker
// in every wall clock it can be stored in stays out of all three.
func TestExpiryConditionsAgreeWithExpiredAt(t *testing.T) {
	db := openSubscribeTable(t)
	now := time.Now().Truncate(time.Millisecond)
	rows := map[int64]time.Time{
		1:  NoLimitExpiry(),
		2:  epochWrittenEast,
		3:  epochWrittenWest,
		4:  time.UnixMilli(0).In(time.FixedZone("UTC+14", 14*3600)),
		5:  {}, // NULL, set below
		6:  now.Add(-time.Minute),
		7:  now,
		8:  now.Add(time.Hour),
		9:  NoLimitBound(),
		10: NoLimitBound().Add(-time.Second),
	}
	var wantExpired, wantUnexpired []int64
	for id, expire := range rows {
		sub := Subscribe{Id: id, Status: SubscribeStatusActive, ExpireTime: expire, Token: fmt.Sprintf("t%d", id), UUID: fmt.Sprintf("u%d", id)}
		if err := db.Create(&sub).Error; err != nil {
			t.Fatal(err)
		}
		if id == 5 {
			if err := db.Exec("UPDATE user_subscribe SET expire_time = NULL WHERE id = ?", id).Error; err != nil {
				t.Fatal(err)
			}
		}
		var stored Subscribe
		if err := db.First(&stored, id).Error; err != nil {
			t.Fatal(err)
		}
		if stored.ExpiredAt(now) {
			wantExpired = append(wantExpired, id)
		} else {
			wantUnexpired = append(wantUnexpired, id)
		}
	}
	sort.Slice(wantExpired, func(i, j int) bool { return wantExpired[i] < wantExpired[j] })
	sort.Slice(wantUnexpired, func(i, j int) bool { return wantUnexpired[i] < wantUnexpired[j] })
	if !reflect.DeepEqual(wantExpired, []int64{6, 7, 9}) {
		t.Fatalf("ExpiredAt accepts %v, want only the real term ends at or before now", wantExpired)
	}

	expired, args := ExpiredCondition(now)
	if got := selectIDs(t, db, expired, args...); !reflect.DeepEqual(got, wantExpired) {
		t.Fatalf("ExpiredCondition selects %v, ExpiredAt %v", got, wantExpired)
	}
	unexpired, args := UnexpiredCondition(now)
	if got := selectIDs(t, db, unexpired, args...); !reflect.DeepEqual(got, wantUnexpired) {
		t.Fatalf("UnexpiredCondition selects %v, want %v", got, wantUnexpired)
	}
	expiring, args := ExpiringCondition(now.Add(-time.Hour), now.Add(2*time.Hour))
	if got := selectIDs(t, db, expiring, args...); !reflect.DeepEqual(got, []int64{6, 7, 8}) {
		t.Fatalf("ExpiringCondition selects %v, want the real term ends inside the window", got)
	}
	// A window opening before the bound is cut there: the markers stay out.
	expiring, args = ExpiringCondition(time.Time{}, now.Add(2*time.Hour))
	if got := selectIDs(t, db, expiring, args...); !reflect.DeepEqual(got, []int64{6, 7, 8, 9}) {
		t.Fatalf("ExpiringCondition from the zero time selects %v, want the real term ends before the window's end", got)
	}
}

func TestResetTrafficReactivatesOnlyFinishedSubscriptionsInTerm(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	future, past := now.Add(time.Hour), now.Add(-time.Hour)
	finishedAt := past
	tests := []struct {
		name       string
		sub        Subscribe
		wantStatus uint8
		wantCols   []string
	}{
		{"finished in term", Subscribe{Status: SubscribeStatusFinished, ExpireTime: future, FinishedAt: &finishedAt},
			SubscribeStatusActive, []string{"download", "upload", "status", "finished_at"}},
		{"finished without time limit", Subscribe{Status: SubscribeStatusFinished, ExpireTime: NoLimitExpiry(), FinishedAt: &finishedAt},
			SubscribeStatusActive, []string{"download", "upload", "status", "finished_at"}},
		{"finished without time limit in another wall clock", Subscribe{Status: SubscribeStatusFinished, ExpireTime: epochWrittenEast, FinishedAt: &finishedAt},
			SubscribeStatusActive, []string{"download", "upload", "status", "finished_at"}},
		{"finished but expired", Subscribe{Status: SubscribeStatusFinished, ExpireTime: past, FinishedAt: &finishedAt},
			SubscribeStatusFinished, []string{"download", "upload"}},
		{"finished before its provider period", Subscribe{Status: SubscribeStatusFinished, StartTime: future, ExpireTime: future.Add(time.Hour)},
			SubscribeStatusFinished, []string{"download", "upload"}},
		{"active", Subscribe{Status: SubscribeStatusActive, ExpireTime: future}, SubscribeStatusActive, []string{"download", "upload"}},
		{"expired", Subscribe{Status: SubscribeStatusExpired, ExpireTime: past}, SubscribeStatusExpired, []string{"download", "upload"}},
		{"stopped", Subscribe{Status: SubscribeStatusStopped, ExpireTime: future}, SubscribeStatusStopped, []string{"download", "upload"}},
		{"deducted", Subscribe{Status: SubscribeStatusDeducted, ExpireTime: future}, SubscribeStatusDeducted, []string{"download", "upload"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := tt.sub
			sub.Upload, sub.Download = 7, 8
			columns := sub.ResetTraffic(now)
			if sub.Upload != 0 || sub.Download != 0 || sub.Status != tt.wantStatus || !reflect.DeepEqual(columns, tt.wantCols) {
				t.Fatalf("after reset: status=%d up=%d down=%d columns=%v; want status %d columns %v", sub.Status, sub.Upload, sub.Download, columns, tt.wantStatus, tt.wantCols)
			}
			if tt.wantStatus == SubscribeStatusActive && sub.FinishedAt != nil {
				t.Fatal("a reactivated subscription keeps its finished time")
			}
		})
	}
}

// ExpiryFromMilli maps the administration API's 0 to the marker and keeps a
// real term end.
func TestExpiryFromMilli(t *testing.T) {
	if got := ExpiryFromMilli(0); !got.Equal(NoLimitExpiry()) || !NoExpiry(got) {
		t.Fatalf("ExpiryFromMilli(0) = %v, want the no-limit marker", got)
	}
	if got := ExpiryFromMilli(1); !got.Equal(NoLimitExpiry()) {
		t.Fatalf("ExpiryFromMilli(1) = %v, want the no-limit marker", got)
	}
	end := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if got := ExpiryFromMilli(end.UnixMilli()); !got.Equal(end) || NoExpiry(got) || got.Location() != timeutil.Location() {
		t.Fatalf("ExpiryFromMilli(%d) = %v, want %v in the application zone", end.UnixMilli(), got, end)
	}
}
