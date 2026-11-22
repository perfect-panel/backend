package period

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return loc
}

// The zones the calendar tests run in: the default application zone, UTC,
// one with daylight saving time and one far enough east that a UTC evening is
// already the next day.
func testZones(t *testing.T) []*time.Location {
	return []*time.Location{
		mustZone(t, "Asia/Shanghai"),
		time.UTC,
		mustZone(t, "America/Los_Angeles"),
		mustZone(t, "Pacific/Auckland"),
	}
}

func TestParseUnit(t *testing.T) {
	for _, name := range []string{"Year", "Month", "Day", "Hour", "Minute", "NoLimit"} {
		if unit, err := ParseUnit(name); err != nil || string(unit) != name {
			t.Fatalf("ParseUnit(%q) = %q, %v", name, unit, err)
		}
	}
	for _, name := range []string{"", "month", "Week", "Quarter"} {
		if _, err := ParseUnit(name); !errors.Is(err, ErrUnknownUnit) {
			t.Fatalf("ParseUnit(%q) error = %v, want ErrUnknownUnit", name, err)
		}
	}
}

func TestTermEnd(t *testing.T) {
	shanghai := mustZone(t, "Asia/Shanghai")
	start := time.Date(2026, 1, 31, 10, 30, 0, 0, shanghai)
	tests := []struct {
		unit     Unit
		quantity int64
		want     time.Time
	}{
		{UnitYear, 1, time.Date(2027, 1, 31, 10, 30, 0, 0, shanghai)},
		// time.AddDate normalizes: a month from Jan 31 ends on Mar 3 in a
		// common year, the existing month-end rule.
		{UnitMonth, 1, time.Date(2026, 3, 3, 10, 30, 0, 0, shanghai)},
		{UnitMonth, 12, time.Date(2027, 1, 31, 10, 30, 0, 0, shanghai)},
		{UnitDay, 30, time.Date(2026, 3, 2, 10, 30, 0, 0, shanghai)},
		{UnitHour, 25, time.Date(2026, 2, 1, 11, 30, 0, 0, shanghai)},
		{UnitMinute, 90, time.Date(2026, 1, 31, 12, 0, 0, 0, shanghai)},
		{UnitNoLimit, 3, usersub.NoLimitExpiry()},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s x%d", tt.unit, tt.quantity), func(t *testing.T) {
			got, err := In(shanghai).TermEnd(tt.unit, tt.quantity, start)
			if err != nil || !got.Equal(tt.want) {
				t.Fatalf("TermEnd = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
	if _, err := In(shanghai).TermEnd("Week", 1, start); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("unknown unit error = %v, want ErrUnknownUnit", err)
	}
}

// A term is counted in the calendar's zone, not in the zone the start value
// happens to carry: Feb 1 04:00 in Shanghai is Jan 31 in UTC, and a month
// from each is a different instant.
func TestTermEndCountsMonthsInTheCalendarZone(t *testing.T) {
	shanghai := mustZone(t, "Asia/Shanghai")
	start := time.Date(2026, 1, 31, 20, 0, 0, 0, time.UTC)
	inShanghai, err := In(shanghai).TermEnd(UnitMonth, 1, start)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 3, 1, 4, 0, 0, 0, shanghai); !inShanghai.Equal(want) {
		t.Fatalf("Shanghai month = %v, want %v", inShanghai, want)
	}
	inUTC, err := In(time.UTC).TermEnd(UnitMonth, 1, start)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 3, 3, 20, 0, 0, 0, time.UTC); !inUTC.Equal(want) {
		t.Fatalf("UTC month = %v, want %v", inUTC, want)
	}
}

func TestResetsOn(t *testing.T) {
	for _, loc := range testZones(t) {
		cal := In(loc)
		date := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 13, 0, 0, 0, loc) }
		tests := []struct {
			name  string
			cycle Cycle
			start time.Time
			day   time.Time
			want  bool
		}{
			{"none never resets", CycleNone, date(2026, 1, 1), date(2026, 2, 1), false},
			{"1st resets on the 1st", CycleFirstOfMonth, date(2026, 1, 15), date(2026, 3, 1), true},
			{"1st skips other days", CycleFirstOfMonth, date(2026, 1, 1), date(2026, 3, 2), false},
			{"monthly on the start day", CycleMonthly, date(2026, 1, 15), date(2026, 6, 15), true},
			{"monthly not the day after", CycleMonthly, date(2026, 1, 15), date(2026, 6, 16), false},
			{"monthly 31st on a 30-day month's end", CycleMonthly, date(2026, 1, 31), date(2026, 4, 30), true},
			{"monthly 31st not on the 29th of April", CycleMonthly, date(2026, 1, 31), date(2026, 4, 29), false},
			{"monthly 30th on February's end", CycleMonthly, date(2026, 1, 30), date(2026, 2, 28), true},
			{"monthly 28th unaffected", CycleMonthly, date(2026, 1, 28), date(2026, 2, 28), true},
			{"yearly on the anniversary", CycleYearly, date(2025, 7, 9), date(2026, 7, 9), true},
			{"yearly not in another month", CycleYearly, date(2025, 7, 9), date(2026, 8, 9), false},
			{"yearly Feb 29 on Feb 28 of a common year", CycleYearly, date(2024, 2, 29), date(2027, 2, 28), true},
			{"yearly Feb 29 not Feb 28 of a leap year", CycleYearly, date(2024, 2, 29), date(2028, 2, 28), false},
			{"yearly Feb 29 on Feb 29 of a leap year", CycleYearly, date(2024, 2, 29), date(2028, 2, 29), true},
			{"yearly Feb 28 on Feb 28", CycleYearly, date(2025, 2, 28), date(2027, 2, 28), true},
		}
		for _, tt := range tests {
			t.Run(loc.String()+"/"+tt.name, func(t *testing.T) {
				if got := cal.ResetsOn(tt.cycle, tt.start, tt.day); got != tt.want {
					t.Fatalf("ResetsOn = %v, want %v", got, tt.want)
				}
			})
		}
	}
}

// The start day is the one in the calendar's zone: 20:00 UTC on Jan 31 is the
// morning of Feb 1 in Shanghai, so a monthly Shanghai calendar resets on the
// 1st and a UTC calendar on the 31st (the 30th in April).
func TestResetsOnReadsDaysInTheCalendarZone(t *testing.T) {
	shanghai := mustZone(t, "Asia/Shanghai")
	start := time.Date(2026, 1, 31, 20, 0, 0, 0, time.UTC)
	aprilEvening := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC) // Apr 30 20:00 in Shanghai
	mayMorning := time.Date(2026, 4, 30, 18, 0, 0, 0, time.UTC)   // May 1 02:00 in Shanghai
	if !In(time.UTC).ResetsOn(CycleMonthly, start, aprilEvening) {
		t.Fatal("UTC calendar: Jan 31 start must reset on Apr 30")
	}
	if In(shanghai).ResetsOn(CycleMonthly, start, aprilEvening) {
		t.Fatal("Shanghai calendar: a Feb 1 start must not reset on Apr 30")
	}
	if !In(shanghai).ResetsOn(CycleMonthly, start, mayMorning) {
		t.Fatal("Shanghai calendar: a Feb 1 start must reset on May 1")
	}
}

// NextReset and CycleAt agree with ResetsOn: the next reset is the first
// reset day after now, and the cycle around now runs from the last reset day
// to the next one, found by walking the calendar day by day.
func TestNextResetAndCycleAtAgreeWithResetsOn(t *testing.T) {
	for _, loc := range testZones(t) {
		cal := In(loc)
		starts := []time.Time{
			time.Date(2024, 2, 29, 23, 30, 0, 0, loc),
			time.Date(2025, 1, 31, 0, 0, 0, 0, loc),
			time.Date(2025, 3, 30, 12, 0, 0, 0, loc),
			time.Date(2025, 11, 1, 1, 0, 0, 0, loc),
		}
		for _, cycle := range []Cycle{CycleFirstOfMonth, CycleMonthly, CycleYearly} {
			for _, start := range starts {
				for now := time.Date(2026, 1, 1, 7, 0, 0, 0, loc); now.Year() < 2029; now = now.AddDate(0, 0, 11) {
					next, ok := cal.NextReset(cycle, start, now)
					if !ok {
						t.Fatalf("%s cycle %d: no next reset", loc, cycle)
					}
					if want := walkDays(cal, cycle, start, now, 1); !next.Equal(want) {
						t.Fatalf("%s cycle %d start %v now %v: NextReset = %v, want %v", loc, cycle, start, now, next, want)
					}
					from, to, ok := cal.CycleAt(cycle, start, now)
					if !ok || from.After(now) || !to.After(now) {
						t.Fatalf("%s cycle %d: CycleAt(%v) = [%v, %v)", loc, cycle, now, from, to)
					}
					if want := walkDays(cal, cycle, start, now, -1); !from.Equal(want) {
						t.Fatalf("%s cycle %d start %v now %v: cycle from %v, want %v", loc, cycle, start, now, from, want)
					}
					if !to.Equal(next) {
						t.Fatalf("%s cycle %d: cycle ends %v, next reset %v", loc, cycle, to, next)
					}
				}
			}
		}
		if _, ok := cal.NextReset(CycleNone, starts[0], starts[0]); ok {
			t.Fatal("CycleNone has a next reset")
		}
		if _, _, ok := cal.CycleAt(CycleNone, starts[0], starts[0]); ok {
			t.Fatal("CycleNone has a reset cycle")
		}
	}
}

// walkDays finds the nearest reset day's midnight strictly after now (step 1)
// or at or before now (step -1) by checking one day after another.
func walkDays(cal Calendar, cycle Cycle, start, now time.Time, step int) time.Time {
	day := cal.DayStart(now)
	if step > 0 {
		day = day.AddDate(0, 0, 1)
	}
	for i := 0; i < 800; i++ {
		if cal.ResetsOn(cycle, start, day) {
			return cal.DayStart(day)
		}
		day = cal.DayStart(day.AddDate(0, 0, step))
	}
	panic("no reset day within 800 days")
}

func TestResetBetween(t *testing.T) {
	for _, loc := range testZones(t) {
		cal := In(loc)
		at := func(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, loc) }
		start := at(2026, 1, 15, 9)
		tests := []struct {
			name     string
			cycle    Cycle
			from, to time.Time
			want     bool
		}{
			{"reset day inside", CycleMonthly, at(2026, 3, 10, 8), at(2026, 3, 20, 8), true},
			{"reset day is the first day", CycleMonthly, at(2026, 3, 15, 23), at(2026, 3, 20, 8), true},
			{"reset day is the last day", CycleMonthly, at(2026, 3, 1, 8), at(2026, 3, 15, 0), true},
			{"no reset day", CycleMonthly, at(2026, 3, 16, 0), at(2026, 4, 14, 23), false},
			{"backwards range", CycleMonthly, at(2026, 3, 20, 0), at(2026, 3, 10, 0), false},
			{"1st of the month", CycleFirstOfMonth, at(2026, 3, 31, 22), at(2026, 4, 1, 1), true},
			{"1st not reached", CycleFirstOfMonth, at(2026, 3, 2, 0), at(2026, 3, 31, 23), false},
			{"yearly anniversary", CycleYearly, at(2026, 12, 1, 0), at(2027, 2, 1, 0), true},
			{"yearly not reached", CycleYearly, at(2026, 2, 1, 0), at(2026, 12, 31, 0), false},
			{"no cycle", CycleNone, at(2026, 1, 1, 0), at(2027, 1, 1, 0), false},
		}
		for _, tt := range tests {
			t.Run(loc.String()+"/"+tt.name, func(t *testing.T) {
				if got := cal.ResetBetween(tt.cycle, start, tt.from, tt.to); got != tt.want {
					t.Fatalf("ResetBetween = %v, want %v", got, tt.want)
				}
			})
		}
	}
}

func TestCycleValid(t *testing.T) {
	for cycle, want := range map[Cycle]bool{-1: false, 0: true, 1: true, 2: true, 3: true, 4: false} {
		if got := cycle.Valid(); got != want {
			t.Fatalf("Cycle(%d).Valid() = %v, want %v", cycle, got, want)
		}
	}
}
