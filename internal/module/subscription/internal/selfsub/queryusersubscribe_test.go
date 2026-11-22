package selfsub

import (
	"fmt"
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/period"
)

func TestNextResetTime(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	cal := period.In(shanghai)
	at := func(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, shanghai) }
	tests := []struct {
		name  string
		cycle int64
		start time.Time
		now   time.Time
		want  time.Time
	}{
		{"monthly on a shorter month's end", 2, at(2026, 1, 31, 10), at(2026, 4, 1, 12), at(2026, 4, 30, 0)},
		{"monthly after this month's reset", 2, at(2026, 1, 15, 10), at(2026, 4, 15, 1), at(2026, 5, 15, 0)},
		{"first of month", 1, at(2026, 1, 15, 10), at(2026, 12, 20, 12), at(2027, 1, 1, 0)},
		{"yearly leap day falls back", 3, at(2024, 2, 29, 10), at(2025, 1, 1, 12), at(2025, 2, 28, 0)},
		{"yearly after this year's reset", 3, at(2024, 7, 9, 10), at(2026, 7, 9, 3), at(2027, 7, 9, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := &dto.UserSubscribe{StartTime: tt.start.UnixMilli(), ExpireTime: tt.start.AddDate(5, 0, 0).UnixMilli(), Subscribe: dto.Subscribe{ResetCycle: tt.cycle}}
			if got := nextResetTime(cal, sub, tt.now); got != tt.want.UnixMilli() {
				t.Fatalf("nextResetTime = %v, want %v", time.UnixMilli(got).In(shanghai), tt.want)
			}
		})
	}

	if got := nextResetTime(cal, &dto.UserSubscribe{StartTime: at(2026, 1, 1, 0).UnixMilli()}, at(2026, 3, 1, 0)); got != 0 {
		t.Fatalf("a plan without calendar reset reports %d", got)
	}
	// A row without a start time counts from its expiry.
	noStart := &dto.UserSubscribe{ExpireTime: at(2026, 9, 20, 8).UnixMilli(), Subscribe: dto.Subscribe{ResetCycle: 2}}
	if got := nextResetTime(cal, noStart, at(2026, 3, 1, 0)); got != at(2026, 3, 20, 0).UnixMilli() {
		t.Fatalf("expiry-based reset = %v", time.UnixMilli(got).In(shanghai))
	}
}

// The start day and "now" are read in the calendar's zone whatever zone the
// process runs in. 20:00 UTC on Jan 31 is Feb 1 in Shanghai: a Shanghai
// deployment resets on the 1st, even on a UTC host.
func TestNextResetTimeIgnoresTheProcessZone(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	local := time.Local
	t.Cleanup(func() { time.Local = local })
	for _, process := range []*time.Location{time.UTC, shanghai, time.FixedZone("UTC-10", -10*3600)} {
		t.Run(fmt.Sprint(process), func(t *testing.T) {
			time.Local = process
			start := time.Date(2026, 1, 31, 20, 0, 0, 0, time.UTC)
			now := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
			sub := &dto.UserSubscribe{StartTime: start.UnixMilli(), ExpireTime: start.AddDate(1, 0, 0).UnixMilli(), Subscribe: dto.Subscribe{ResetCycle: 2}}
			want := time.Date(2026, 4, 1, 0, 0, 0, 0, shanghai).UnixMilli()
			if got := nextResetTime(period.In(shanghai), sub, now); got != want {
				t.Fatalf("next reset = %v, want Apr 1 in Shanghai", time.UnixMilli(got).In(shanghai))
			}
		})
	}
}
