package user

import (
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
)

func TestCalculateNextResetTimeAtUsesStartTime(t *testing.T) {
	loc := time.Local
	sub := &dto.UserSubscribe{
		StartTime:  time.Date(2026, 1, 31, 10, 0, 0, 0, loc).UnixMilli(),
		ExpireTime: time.Date(2026, 12, 15, 10, 0, 0, 0, loc).UnixMilli(),
		Subscribe:  dto.Subscribe{ResetCycle: 2},
	}
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, loc)
	want := time.Date(2026, 4, 30, 0, 0, 0, 0, loc).UnixMilli()

	if got := calculateNextResetTimeAt(sub, now); got != want {
		t.Fatalf("calculateNextResetTimeAt() = %d, want %d", got, want)
	}
}

func TestCalculateNextResetTimeAtYearlyLeapFallback(t *testing.T) {
	loc := time.Local
	sub := &dto.UserSubscribe{
		StartTime:  time.Date(2024, 2, 29, 10, 0, 0, 0, loc).UnixMilli(),
		ExpireTime: time.Date(2028, 12, 15, 10, 0, 0, 0, loc).UnixMilli(),
		Subscribe:  dto.Subscribe{ResetCycle: 3},
	}
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, loc)
	want := time.Date(2025, 2, 28, 0, 0, 0, 0, loc).UnixMilli()

	if got := calculateNextResetTimeAt(sub, now); got != want {
		t.Fatalf("calculateNextResetTimeAt() = %d, want %d", got, want)
	}
}
