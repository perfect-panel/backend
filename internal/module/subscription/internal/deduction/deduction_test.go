package deduction

import (
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/subscription/internal/period"
)

func TestSubscribe_Validate(t *testing.T) {
	tests := []struct {
		name    string
		sub     Subscribe
		wantErr bool
		errType error
	}{
		{
			name: "valid subscription",
			sub: Subscribe{
				StartTime:      time.Now(),
				ExpireTime:     time.Now().Add(24 * time.Hour),
				Traffic:        1000,
				Download:       100,
				Upload:         200,
				UnitTime:       period.UnitMonth,
				DeductionRatio: 50,
			},
			wantErr: false,
		},
		{
			name: "negative traffic",
			sub: Subscribe{
				StartTime:      time.Now(),
				ExpireTime:     time.Now().Add(24 * time.Hour),
				Traffic:        -1000,
				Download:       100,
				Upload:         200,
				UnitTime:       period.UnitMonth,
				DeductionRatio: 50,
			},
			wantErr: true,
			errType: ErrInvalidTraffic,
		},
		{
			name: "negative download",
			sub: Subscribe{
				StartTime:      time.Now(),
				ExpireTime:     time.Now().Add(24 * time.Hour),
				Traffic:        1000,
				Download:       -100,
				Upload:         200,
				UnitTime:       period.UnitMonth,
				DeductionRatio: 50,
			},
			wantErr: true,
			errType: ErrInvalidTraffic,
		},
		{
			name: "download + upload exceeds traffic",
			sub: Subscribe{
				StartTime:      time.Now(),
				ExpireTime:     time.Now().Add(24 * time.Hour),
				Traffic:        1000,
				Download:       600,
				Upload:         500,
				UnitTime:       period.UnitMonth,
				DeductionRatio: 50,
			},
			// Reports can land after the quota ran out: over-use leaves no
			// unused traffic to refund, it is not invalid.
			wantErr: false,
		},
		{
			name: "expire time before start time",
			sub: Subscribe{
				StartTime:      time.Now(),
				ExpireTime:     time.Now().Add(-24 * time.Hour),
				Traffic:        1000,
				Download:       100,
				Upload:         200,
				UnitTime:       period.UnitMonth,
				DeductionRatio: 50,
			},
			wantErr: true,
			errType: ErrInvalidTimeRange,
		},
		{
			name: "invalid deduction ratio - negative",
			sub: Subscribe{
				StartTime:      time.Now(),
				ExpireTime:     time.Now().Add(24 * time.Hour),
				Traffic:        1000,
				Download:       100,
				Upload:         200,
				UnitTime:       period.UnitMonth,
				DeductionRatio: -10,
			},
			wantErr: true,
			errType: ErrInvalidDeductionRatio,
		},
		{
			name: "invalid deduction ratio - over 100",
			sub: Subscribe{
				StartTime:      time.Now(),
				ExpireTime:     time.Now().Add(24 * time.Hour),
				Traffic:        1000,
				Download:       100,
				Upload:         200,
				UnitTime:       period.UnitMonth,
				DeductionRatio: 150,
			},
			wantErr: true,
			errType: ErrInvalidDeductionRatio,
		},
		{
			name: "invalid unit time",
			sub: Subscribe{
				StartTime:      time.Now(),
				ExpireTime:     time.Now().Add(24 * time.Hour),
				Traffic:        1000,
				Download:       100,
				Upload:         200,
				UnitTime:       "InvalidUnit",
				DeductionRatio: 50,
			},
			wantErr: true,
			errType: ErrInvalidUnitTime,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.sub.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Subscribe.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.errType != nil && !errors.Is(err, tt.errType) {
				t.Errorf("Subscribe.Validate() error = %v, want %v", err, tt.errType)
			}
		})
	}
}

func TestOrder_Validate(t *testing.T) {
	if err := (&Order{Amount: 0}).Validate(); err != nil {
		t.Fatalf("zero amount must be valid, got %v", err)
	}
	if err := (&Order{Amount: -1000}).Validate(); !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("negative amount error = %v, want %v", err, ErrInvalidAmount)
	}
}

func TestCalculateWeights(t *testing.T) {
	tests := []struct {
		name              string
		deductionRatio    int64
		wantTrafficWeight float64
		wantTimeWeight    float64
	}{
		{name: "zero ratio", deductionRatio: 0, wantTrafficWeight: 0, wantTimeWeight: 0},
		{name: "50% ratio", deductionRatio: 50, wantTrafficWeight: 0.5, wantTimeWeight: 0.5},
		{name: "75% ratio", deductionRatio: 75, wantTrafficWeight: 0.75, wantTimeWeight: 0.25},
		{name: "100% ratio", deductionRatio: 100, wantTrafficWeight: 1.0, wantTimeWeight: 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTrafficWeight, gotTimeWeight := calculateWeights(tt.deductionRatio)
			if gotTrafficWeight != tt.wantTrafficWeight {
				t.Errorf("calculateWeights() trafficWeight = %v, want %v", gotTrafficWeight, tt.wantTrafficWeight)
			}
			if gotTimeWeight != tt.wantTimeWeight {
				t.Errorf("calculateWeights() timeWeight = %v, want %v", gotTimeWeight, tt.wantTimeWeight)
			}
		})
	}
}

func TestCalculateNoLimitAmount(t *testing.T) {
	tests := []struct {
		name  string
		sub   Subscribe
		order Order
		want  int64
	}{
		{
			name:  "normal no limit calculation",
			sub:   Subscribe{Traffic: 1000, Download: 300, Upload: 200},
			order: Order{Amount: 1000},
			want:  500, // (1000 - 300 - 200) / 1000 * 1000 = 500
		},
		{
			name:  "zero traffic",
			sub:   Subscribe{Traffic: 0, Download: 0, Upload: 0},
			order: Order{Amount: 1000},
			want:  0,
		},
		{
			name:  "overused traffic",
			sub:   Subscribe{Traffic: 1000, Download: 600, Upload: 500},
			order: Order{Amount: 1000},
			want:  0, // remaining traffic would be negative, clamped to 0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := calculateNoLimitAmount(tt.sub, tt.order); got != tt.want {
				t.Errorf("calculateNoLimitAmount() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateRemainingAmount(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		sub     Subscribe
		order   Order
		wantErr bool
	}{
		{
			name: "valid no limit subscription",
			sub: Subscribe{
				StartTime:  now.Add(-24 * time.Hour),
				ExpireTime: now.Add(24 * time.Hour),
				Traffic:    1000,
				Download:   300,
				Upload:     200,
				UnitTime:   period.UnitNoLimit,
			},
			order: Order{Amount: 1000},
		},
		{
			name: "invalid subscription",
			sub: Subscribe{
				StartTime:  now,
				ExpireTime: now.Add(-24 * time.Hour), // Invalid: expire before start
				Traffic:    1000,
				Download:   300,
				Upload:     200,
				UnitTime:   period.UnitMonth,
			},
			order:   Order{Amount: 1000},
			wantErr: true,
		},
		{
			name: "invalid order",
			sub: Subscribe{
				StartTime:  now.Add(-24 * time.Hour),
				ExpireTime: now.Add(24 * time.Hour),
				Traffic:    1000,
				Download:   300,
				Upload:     200,
				UnitTime:   period.UnitMonth,
			},
			order:   Order{Amount: -1}, // Invalid: negative amount
			wantErr: true,
		},
		{
			name: "no limit with reset cycle",
			sub: Subscribe{
				StartTime:  now.Add(-24 * time.Hour),
				ExpireTime: now.Add(24 * time.Hour),
				Traffic:    1000,
				Download:   300,
				Upload:     200,
				UnitTime:   period.UnitNoLimit,
				ResetCycle: period.CycleMonthly, // Should return 0
			},
			order: Order{Amount: 1000},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CalculateRemainingAmount(tt.sub, tt.order)
			if (err != nil) != tt.wantErr {
				t.Errorf("CalculateRemainingAmount() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCalculateRemainingAmount_NoLimitWithResetCycle(t *testing.T) {
	now := time.Now()
	sub := Subscribe{
		StartTime:  now.Add(-24 * time.Hour),
		ExpireTime: now.Add(24 * time.Hour),
		Traffic:    1000,
		Download:   300,
		Upload:     200,
		UnitTime:   period.UnitNoLimit,
		ResetCycle: period.CycleMonthly,
	}

	got, err := CalculateRemainingAmount(sub, Order{Amount: 1000})
	if err != nil {
		t.Errorf("CalculateRemainingAmount() error = %v", err)
		return
	}
	if got != 0 {
		t.Errorf("CalculateRemainingAmount() = %v, want 0", got)
	}
}

// Each case pins a formula the refund once got wrong: whole remaining units
// were added on top of a pro-rata share of the whole term (200% on the
// purchase day), and reset cycles were measured by the cycle's position
// instead of the subscription's remaining time (97% after 11 months, or a
// negative refund).
func TestCalculateRemainingAmountRefundsOnlyWhatIsLeft(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	at := func(year int, month time.Month, day, hour int) time.Time {
		return time.Date(year, month, day, hour, 0, 0, 0, loc)
	}
	const gb = int64(1) << 30

	tests := []struct {
		name   string
		sub    Subscribe
		amount int64
		now    time.Time
		want   int64
	}{
		{
			name:   "year plan unsubscribed on the purchase day",
			sub:    Subscribe{StartTime: at(2026, 3, 10, 9), ExpireTime: at(2027, 3, 10, 9), UnitTime: period.UnitYear},
			amount: 36500,
			now:    at(2026, 3, 10, 10),
			want:   36495, // 8759 of 8760 hours left
		},
		{
			name:   "month plan bought on Jan 31 unsubscribed the same day",
			sub:    Subscribe{StartTime: at(2026, 1, 31, 12), ExpireTime: at(2026, 3, 3, 12), UnitTime: period.UnitMonth},
			amount: 3000,
			now:    at(2026, 1, 31, 13),
			want:   2995, // 743 of 744 hours left
		},
		{
			name:   "month plan with monthly reset after 29 of 30 days",
			sub:    Subscribe{StartTime: at(2026, 4, 1, 0), ExpireTime: at(2026, 5, 1, 0), UnitTime: period.UnitMonth, ResetCycle: period.CycleMonthly},
			amount: 3000,
			now:    at(2026, 4, 30, 0),
			want:   100,
		},
		{
			name:   "month plan with yearly reset after one day",
			sub:    Subscribe{StartTime: at(2026, 4, 1, 0), ExpireTime: at(2026, 5, 1, 0), UnitTime: period.UnitMonth, ResetCycle: period.CycleYearly},
			amount: 3000,
			now:    at(2026, 4, 2, 0),
			want:   2900,
		},
		{
			name: "year plan resetting on the 1st, last month half used",
			sub: Subscribe{
				StartTime: at(2025, 9, 1, 0), ExpireTime: at(2026, 9, 1, 0), UnitTime: period.UnitYear,
				ResetCycle: period.CycleFirstOfMonth, Traffic: 100 * gb, Download: 30 * gb, Upload: 20 * gb,
			},
			amount: 36500,
			now:    at(2026, 8, 1, 0),
			want:   1550, // half of the last 31 days
		},
		{
			name: "current cycle's traffic used up is not refunded",
			sub: Subscribe{
				StartTime: at(2026, 1, 15, 0), ExpireTime: at(2027, 1, 15, 0), UnitTime: period.UnitYear,
				ResetCycle: period.CycleMonthly, Traffic: 100 * gb, Download: 100 * gb,
			},
			amount: 36500,
			now:    at(2026, 3, 16, 0),
			want:   27500, // only Apr 15 onwards: 275 of 365 days
		},
		{
			name: "deduction ratio weighs traffic against time",
			sub: Subscribe{
				StartTime: at(2026, 4, 1, 0), ExpireTime: at(2026, 5, 1, 0), UnitTime: period.UnitMonth,
				Traffic: 100 * gb, Download: 60 * gb, DeductionRatio: 50,
			},
			amount: 3000,
			now:    at(2026, 4, 16, 0),
			want:   1350, // 0.5 * 40% traffic left + 0.5 * 50% time left
		},
		{
			name: "usage past the quota refunds only later cycles",
			sub: Subscribe{
				StartTime: at(2026, 1, 15, 0), ExpireTime: at(2027, 1, 15, 0), UnitTime: period.UnitYear,
				ResetCycle: period.CycleMonthly, Traffic: 100 * gb, Download: 90 * gb, Upload: 15 * gb,
			},
			amount: 36500,
			now:    at(2026, 3, 16, 0),
			want:   27500, // as if used up: only Apr 15 onwards
		},
		{
			name: "unlimited traffic in use refunds by time",
			sub: Subscribe{
				StartTime: at(2026, 4, 1, 0), ExpireTime: at(2026, 5, 1, 0), UnitTime: period.UnitMonth,
				Traffic: 0, Download: 500 * gb, Upload: 20 * gb,
			},
			amount: 3000,
			now:    at(2026, 4, 16, 0),
			want:   1500,
		},
		{
			name: "no expiry refunds the unused traffic",
			sub: Subscribe{
				StartTime: at(2026, 4, 1, 0), ExpireTime: time.UnixMilli(0), UnitTime: period.UnitMonth,
				Traffic: 100 * gb, Download: 25 * gb,
			},
			amount: 3000,
			now:    at(2026, 4, 16, 0),
			want:   2250,
		},
		{
			name:   "expired subscription refunds nothing",
			sub:    Subscribe{StartTime: at(2026, 4, 1, 0), ExpireTime: at(2026, 5, 1, 0), UnitTime: period.UnitMonth},
			amount: 3000,
			now:    at(2026, 5, 1, 1),
			want:   0,
		},
		{
			name:   "not yet started refunds everything",
			sub:    Subscribe{StartTime: at(2026, 4, 1, 0), ExpireTime: at(2026, 5, 1, 0), UnitTime: period.UnitMonth},
			amount: 3000,
			now:    at(2026, 3, 30, 0),
			want:   3000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calculateRemainingAmount(period.In(loc), tt.sub, Order{Amount: tt.amount}, tt.now)
			if err != nil {
				t.Fatalf("calculateRemainingAmount() error = %v", err)
			}
			// Float rounding may cost one minor unit; never more.
			if got > tt.want || got < tt.want-1 {
				t.Fatalf("refund = %d, want %d", got, tt.want)
			}
		})
	}
}

// Whatever the plan, usage and moment of cancellation, a refund stays
// within what was paid.
func TestCalculateRemainingAmountStaysWithinPaid(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	units := []period.Unit{period.UnitYear, period.UnitMonth, period.UnitDay, period.UnitHour}
	loc := time.FixedZone("CST", 8*3600)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)

	for i := 0; i < 20000; i++ {
		start := base.Add(time.Duration(rng.Int63n(int64(5 * 365 * 24 * time.Hour))))
		quantity := 1 + rng.Intn(36)
		unit := units[rng.Intn(len(units))]
		var expire time.Time
		switch unit {
		case period.UnitYear:
			expire = start.AddDate(quantity, 0, 0)
		case period.UnitMonth:
			expire = start.AddDate(0, quantity, 0)
		case period.UnitDay:
			expire = start.AddDate(0, 0, quantity)
		default:
			expire = start.Add(time.Duration(quantity) * time.Hour)
		}
		sub := Subscribe{
			StartTime:      start,
			ExpireTime:     expire,
			UnitTime:       unit,
			ResetCycle:     period.Cycle(rng.Intn(4)),
			DeductionRatio: int64(rng.Intn(101)),
		}
		if rng.Intn(3) > 0 {
			sub.Traffic = 1 + rng.Int63n(1<<40)
			sub.Download = rng.Int63n(sub.Traffic + 1)
			sub.Upload = rng.Int63n(sub.Traffic - sub.Download + 1)
		}
		amount := rng.Int63n(10_000_000)
		span := expire.Sub(start) + 20*24*time.Hour
		now := start.Add(-10*24*time.Hour + time.Duration(rng.Int63n(int64(span))))

		got, err := calculateRemainingAmount(period.In(loc), sub, Order{Amount: amount}, now)
		if err != nil {
			t.Fatalf("case %d: unexpected error %v for %+v", i, err, sub)
		}
		if got < 0 || got > amount {
			t.Fatalf("case %d: refund %d outside [0, %d] for %+v at %v", i, got, amount, sub, now)
		}
	}
}

func TestCurrentResetCycle(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	day := func(year int, month time.Month, d int) time.Time {
		return time.Date(year, month, d, 0, 0, 0, 0, loc)
	}

	tests := []struct {
		name      string
		sub       Subscribe
		now       time.Time
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			name:      "1st of month",
			sub:       Subscribe{StartTime: day(2025, 6, 20), ExpireTime: day(2027, 6, 20), ResetCycle: period.CycleFirstOfMonth},
			now:       day(2026, 2, 10).Add(5 * time.Hour),
			wantStart: day(2026, 2, 1),
			wantEnd:   day(2026, 3, 1),
		},
		{
			name:      "monthly on the 31st falls back to the month's last day",
			sub:       Subscribe{StartTime: day(2026, 1, 31), ExpireTime: day(2027, 1, 31), ResetCycle: period.CycleMonthly},
			now:       day(2026, 2, 15),
			wantStart: day(2026, 1, 31),
			wantEnd:   day(2026, 2, 28),
		},
		{
			name:      "monthly on the 31st from February's last day",
			sub:       Subscribe{StartTime: day(2026, 1, 31), ExpireTime: day(2027, 1, 31), ResetCycle: period.CycleMonthly},
			now:       day(2026, 2, 28).Add(12 * time.Hour),
			wantStart: day(2026, 2, 28),
			wantEnd:   day(2026, 3, 31),
		},
		{
			name:      "yearly from Feb 29 resets on Feb 28 in common years",
			sub:       Subscribe{StartTime: day(2024, 2, 29), ExpireTime: day(2028, 2, 29), ResetCycle: period.CycleYearly},
			now:       day(2025, 3, 1),
			wantStart: day(2025, 2, 28),
			wantEnd:   day(2026, 2, 28),
		},
		{
			name:      "cycle clamped to the subscription term",
			sub:       Subscribe{StartTime: day(2026, 2, 10), ExpireTime: day(2026, 2, 20), ResetCycle: period.CycleFirstOfMonth},
			now:       day(2026, 2, 12),
			wantStart: day(2026, 2, 10),
			wantEnd:   day(2026, 2, 20),
		},
		{
			name:      "no reset cycle spans the whole term",
			sub:       Subscribe{StartTime: day(2026, 2, 10), ExpireTime: day(2027, 2, 10)},
			now:       day(2026, 7, 1),
			wantStart: day(2026, 2, 10),
			wantEnd:   day(2027, 2, 10),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStart, gotEnd := currentResetCycle(period.In(loc), tt.sub, tt.now)
			if !gotStart.Equal(tt.wantStart) || !gotEnd.Equal(tt.wantEnd) {
				t.Fatalf("cycle = [%v, %v), want [%v, %v)", gotStart, gotEnd, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// Benchmark tests
func BenchmarkCalculateRemainingAmount(b *testing.B) {
	now := time.Now()
	sub := Subscribe{
		StartTime:      now.Add(-24 * time.Hour),
		ExpireTime:     now.Add(24 * time.Hour),
		Traffic:        1000,
		Download:       300,
		Upload:         200,
		UnitTime:       period.UnitMonth,
		ResetCycle:     period.CycleNone,
		DeductionRatio: 50,
	}
	order := Order{Amount: 1000}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = CalculateRemainingAmount(sub, order)
	}
}
