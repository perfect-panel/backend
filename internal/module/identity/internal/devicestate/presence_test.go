package devicestate

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// presenceFixture is one account whose devices connect to the device
// WebSocket.
type presenceFixture struct {
	*identitytest.Env
	owner *user.User
}

func newPresenceFixture(t *testing.T) *presenceFixture {
	t.Helper()
	logtest.Discard(t)
	env := identitytest.New(t)
	enabled := true
	owner := &user.User{Enable: &enabled}
	if err := env.DB.Create(owner).Error; err != nil {
		t.Fatal(err)
	}
	return &presenceFixture{Env: env, owner: owner}
}

// device stores a device of the owner with the given flags.
func (f *presenceFixture) device(t *testing.T, identifier string, enabled, online bool) *user.Device {
	t.Helper()
	d := &user.Device{UserId: f.owner.Id, Identifier: identifier, Ip: "192.0.2.1"}
	if err := f.DB.Create(d).Error; err != nil {
		t.Fatal(err)
	}
	// Create skips false flags that have a column default, so they are
	// written explicitly.
	if err := f.DB.Model(d).Updates(map[string]any{"enabled": enabled, "online": online}).Error; err != nil {
		t.Fatal(err)
	}
	return d
}

func (f *presenceFixture) stored(t *testing.T, id int64) user.Device {
	t.Helper()
	var d user.Device
	if err := f.DB.First(&d, id).Error; err != nil {
		t.Fatal(err)
	}
	return d
}

func (f *presenceFixture) records(t *testing.T) []user.DeviceOnlineRecord {
	t.Helper()
	var rows []user.DeviceOnlineRecord
	if err := f.DB.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

// rejectWrites makes every write of the operation to table fail.
func (f *presenceFixture) rejectWrites(t *testing.T, operation, table string) {
	t.Helper()
	trigger := "CREATE TRIGGER reject_" + strings.ToLower(operation) + "_" + table + " BEFORE " + operation + " ON " + table +
		" BEGIN SELECT RAISE(FAIL, 'test failure'); END"
	if err := f.DB.Exec(trigger).Error; err != nil {
		t.Fatal(err)
	}
}

// A connecting device shows online. A disabled one stays offline, and a
// device removed meanwhile has no presence to record.
func TestMarkOnlineShowsOnlyEnabledDevicesOnline(t *testing.T) {
	f := newPresenceFixture(t)
	ctx := context.Background()
	enabled := f.device(t, "enabled-device", true, false)
	disabled := f.device(t, "disabled-device", false, false)

	for _, identifier := range []string{enabled.Identifier, disabled.Identifier, "removed-device"} {
		if err := MarkOnline(ctx, f.Store.UserDevice(), identifier); err != nil {
			t.Fatalf("MarkOnline(%s) = %v", identifier, err)
		}
	}

	if !f.stored(t, enabled.Id).Online {
		t.Fatal("the enabled device is not online")
	}
	if f.stored(t, disabled.Id).Online {
		t.Fatal("the disabled device went online")
	}
}

// A closed connection shows the device offline and records, for the
// account, when the connection opened and closed and how long it lasted.
func TestMarkOfflineRecordsTheConnection(t *testing.T) {
	f := newPresenceFixture(t)
	d := f.device(t, "phone", true, true)
	connectedAt := timeutil.Now().Add(-90 * time.Second)

	if err := MarkOffline(context.Background(), f.Store.UserDevice(), f.owner.Id, d.Identifier, connectedAt); err != nil {
		t.Fatalf("MarkOffline() = %v", err)
	}

	if f.stored(t, d.Id).Online {
		t.Fatal("the device is still online")
	}
	records := f.records(t)
	if len(records) != 1 {
		t.Fatalf("records = %+v, want one", records)
	}
	r := records[0]
	if r.UserId != f.owner.Id || r.Identifier != d.Identifier || r.DurationDays != 1 {
		t.Fatalf("record = %+v, want the owner's phone starting a one-day streak", r)
	}
	if r.OnlineSeconds < 90 || r.OnlineSeconds > 95 || r.OnlineTime.Sub(connectedAt).Abs() > time.Second ||
		r.OfflineTime.Sub(connectedAt.Add(90*time.Second)).Abs() > 5*time.Second {
		t.Fatalf("record = %+v, want the 90 seconds since %v", r, connectedAt)
	}
}

// A device removed before its connection closed has no presence to record.
func TestMarkOfflineSkipsARemovedDevice(t *testing.T) {
	f := newPresenceFixture(t)

	if err := MarkOffline(context.Background(), f.Store.UserDevice(), f.owner.Id, "removed-device", timeutil.Now()); err != nil {
		t.Fatalf("MarkOffline() = %v", err)
	}
	if records := f.records(t); len(records) != 0 {
		t.Fatalf("records = %+v, want none", records)
	}
}

// The connection ended even when the online flag cannot be cleared, so its
// online time is still recorded; the failure is reported.
func TestMarkOfflineRecordsTheConnectionWhenTheFlagCannotBeCleared(t *testing.T) {
	f := newPresenceFixture(t)
	d := f.device(t, "phone", true, true)
	f.rejectWrites(t, "UPDATE", "user_device")

	err := MarkOffline(context.Background(), f.Store.UserDevice(), f.owner.Id, d.Identifier, timeutil.Now().Add(-time.Minute))

	if err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("MarkOffline() = %v, want the failed offline flag reported", err)
	}
	if records := f.records(t); len(records) != 1 {
		t.Fatalf("records = %+v, want the connection recorded", records)
	}
}

// A record that cannot be written and a device that cannot be read are
// reported, not dropped.
func TestPresenceReportsStoreFailures(t *testing.T) {
	f := newPresenceFixture(t)
	ctx := context.Background()
	d := f.device(t, "phone", true, true)
	f.rejectWrites(t, "INSERT", "user_device_online_record")

	err := MarkOffline(ctx, f.Store.UserDevice(), f.owner.Id, d.Identifier, timeutil.Now())
	if err == nil || !strings.Contains(err.Error(), "record the online time") {
		t.Fatalf("MarkOffline() = %v, want the failed record reported", err)
	}
	if f.stored(t, d.Id).Online {
		t.Fatal("the device is still online")
	}

	if err := f.DB.Migrator().DropTable(&user.Device{}); err != nil {
		t.Fatal(err)
	}
	if err := MarkOnline(ctx, f.Store.UserDevice(), "unread-device"); err == nil || !strings.Contains(err.Error(), "find device") {
		t.Fatalf("MarkOnline() = %v, want the failed lookup reported", err)
	}
	if err := MarkOffline(ctx, f.Store.UserDevice(), f.owner.Id, "unread-device", timeutil.Now()); err == nil || !strings.Contains(err.Error(), "find device") {
		t.Fatalf("MarkOffline() = %v, want the failed lookup reported", err)
	}
}

// onlineRecord stores a finished connection of the owner created at.
func (f *presenceFixture) onlineRecord(t *testing.T, at time.Time, streak int64) {
	t.Helper()
	record := &user.DeviceOnlineRecord{UserId: f.owner.Id, Identifier: "tablet", OnlineTime: at, OfflineTime: at, DurationDays: streak, CreatedAt: at}
	if err := f.DB.Create(record).Error; err != nil {
		t.Fatal(err)
	}
}

// A connection continues the streak of the previous day's connections; one
// after a day offline, or the day's second one, does not lengthen it. The
// connection closes at a pinned instant, so the day it is recorded on and
// the seeded history agree whatever the wall clock says.
func TestMarkOfflineContinuesTheStreakOfThePreviousDay(t *testing.T) {
	now := time.Date(2026, time.March, 14, 15, 9, 26, 0, timeutil.Location())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, tc := range []struct {
		name string
		seed func(f *presenceFixture, t *testing.T)
		want int64
	}{
		{"yesterday online", func(f *presenceFixture, t *testing.T) { f.onlineRecord(t, today.Add(-12*time.Hour), 3) }, 4},
		{"the day's second connection", func(f *presenceFixture, t *testing.T) {
			f.onlineRecord(t, today.Add(-12*time.Hour), 3)
			f.onlineRecord(t, today.Add(2*time.Hour), 4)
		}, 4},
		{"a day offline", func(f *presenceFixture, t *testing.T) { f.onlineRecord(t, today.Add(-36*time.Hour), 3) }, 1},
		{"no history", func(*presenceFixture, *testing.T) {}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPresenceFixture(t)
			tc.seed(f, t)
			d := f.device(t, "phone", true, true)
			if err := markOffline(context.Background(), f.Store.UserDevice(), f.owner.Id, d.Identifier, now.Add(-time.Minute), now); err != nil {
				t.Fatalf("markOffline() = %v", err)
			}
			records := f.records(t)
			if got := records[len(records)-1].DurationDays; got != tc.want {
				t.Fatalf("streak = %d, want %d", got, tc.want)
			}
		})
	}
}
