// Package deduction provides functionality for calculating remaining amounts
// in subscription billing systems, supporting various time units and traffic-based calculations.
package deduction

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/period"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// Error definitions for validation and calculation failures
var (
	ErrInvalidAmount         = errors.New("order amount cannot be negative")
	ErrInvalidTraffic        = errors.New("traffic values cannot be negative")
	ErrInvalidTimeRange      = errors.New("expire time must be after start time")
	ErrInvalidUnitTime       = errors.New("invalid unit time")
	ErrInvalidDeductionRatio = errors.New("deduction ratio must be between 0 and 100")
)

// Subscribe represents a subscription with time and traffic limits
type Subscribe struct {
	StartTime      time.Time    // Subscription start time
	ExpireTime     time.Time    // Subscription expiration time
	Traffic        int64        // Traffic allowance per reset cycle in bytes (0 = unlimited)
	Download       int64        // Downloaded traffic in the current cycle in bytes
	Upload         int64        // Uploaded traffic in the current cycle in bytes
	UnitTime       period.Unit  // Time unit the term was bought in
	ResetCycle     period.Cycle // Traffic reset cycle
	DeductionRatio int64        // Deduction ratio for weighted calculations (0-100)
}

// Order represents what was paid for the subscription term
type Order struct {
	Amount int64 // Total amount paid for the term from StartTime to ExpireTime
}

// Validate checks if the Subscribe struct contains valid data
func (s *Subscribe) Validate() error {
	if s.Traffic < 0 || s.Download < 0 || s.Upload < 0 {
		return ErrInvalidTraffic
	}

	// Usage above the quota is not an error: traffic reports can land after
	// the quota ran out, and an unlimited quota (0) has no bound at all. Both
	// simply leave no unused traffic to refund.

	// A subscription without a time limit has no term to measure.
	if !s.noTimeLimit() && !s.ExpireTime.After(s.StartTime) {
		return ErrInvalidTimeRange
	}

	if s.DeductionRatio < 0 || s.DeductionRatio > 100 {
		return ErrInvalidDeductionRatio
	}

	if _, err := period.ParseUnit(string(s.UnitTime)); err != nil {
		return ErrInvalidUnitTime
	}

	return nil
}

// Validate checks if the Order struct contains valid data
func (o *Order) Validate() error {
	if o.Amount < 0 {
		return ErrInvalidAmount
	}
	return nil
}

// CalculateRemainingAmount returns the refundable share of what was paid for
// the subscription. Time after the current traffic-reset cycle refunds pro
// rata; the current cycle refunds by its unused time and traffic. The result
// is always between zero and the amount paid.
func CalculateRemainingAmount(sub Subscribe, order Order) (int64, error) {
	return calculateRemainingAmount(period.App(), sub, order, timeutil.Now())
}

func calculateRemainingAmount(cal period.Calendar, sub Subscribe, order Order, now time.Time) (int64, error) {
	if err := sub.Validate(); err != nil {
		return 0, fmt.Errorf("invalid subscription: %w", err)
	}

	if err := order.Validate(); err != nil {
		return 0, fmt.Errorf("invalid order: %w", err)
	}

	if sub.noTimeLimit() {
		if sub.ResetCycle != period.CycleNone {
			return 0, nil
		}
		return calculateNoLimitAmount(sub, order), nil
	}

	if order.Amount == 0 || !now.Before(sub.ExpireTime) {
		return 0, nil
	}
	if now.Before(sub.StartTime) {
		now = sub.StartTime
	}

	cycleStart, cycleEnd := currentResetCycle(cal, sub, now)
	remaining := float64(sub.ExpireTime.Sub(cycleEnd))
	if cycle := cycleEnd.Sub(cycleStart); cycle > 0 {
		remaining += float64(cycle) * remainingShare(sub, float64(cycleEnd.Sub(now))/float64(cycle))
	}
	return proportion(order.Amount, remaining/float64(sub.ExpireTime.Sub(sub.StartTime))), nil
}

// noTimeLimit reports whether the subscription has no term: bought without a
// time limit, or set to never expire.
func (s *Subscribe) noTimeLimit() bool {
	return s.UnitTime == period.UnitNoLimit || usersub.NoExpiry(s.ExpireTime)
}

// calculateNoLimitAmount refunds a time-unlimited subscription by its unused
// traffic; without a traffic cap there is nothing left to measure.
func calculateNoLimitAmount(sub Subscribe, order Order) int64 {
	if sub.Traffic == 0 {
		return 0
	}
	return proportion(order.Amount, unusedTrafficShare(sub))
}

// unusedTrafficShare is the unused part of the quota in [0, 1]: usage that
// went past the quota leaves none.
func unusedTrafficShare(sub Subscribe) float64 {
	return math.Max(0, math.Min(1, float64(sub.Traffic-sub.Download-sub.Upload)/float64(sub.Traffic)))
}

// currentResetCycle returns the traffic-reset cycle containing now, clamped
// to the subscription term: the calendar reset's own cycle (period), so a
// refund counts the traffic period the owner is in. Without a reset cycle the
// whole term is one cycle.
func currentResetCycle(cal period.Calendar, sub Subscribe, now time.Time) (time.Time, time.Time) {
	cycleStart, cycleEnd, ok := cal.CycleAt(sub.ResetCycle, sub.StartTime, now)
	if !ok {
		return sub.StartTime, sub.ExpireTime
	}
	if cycleStart.Before(sub.StartTime) {
		cycleStart = sub.StartTime
	}
	if cycleEnd.After(sub.ExpireTime) {
		cycleEnd = sub.ExpireTime
	}
	return cycleStart, cycleEnd
}

// remainingShare combines the unused time and traffic of the current cycle
// the way the plan asks: the smaller of the two by default, or a weighted mix
// when the plan sets a deduction ratio.
func remainingShare(sub Subscribe, timeRatio float64) float64 {
	if sub.Traffic == 0 {
		return timeRatio
	}
	trafficRatio := unusedTrafficShare(sub)
	if sub.DeductionRatio == 0 {
		return math.Min(timeRatio, trafficRatio)
	}
	trafficWeight, timeWeight := calculateWeights(sub.DeductionRatio)
	return trafficWeight*trafficRatio + timeWeight*timeRatio
}

// calculateWeights converts deduction ratio to traffic and time weights
// for weighted calculations
func calculateWeights(deductionRatio int64) (float64, float64) {
	if deductionRatio == 0 {
		return 0, 0
	}
	trafficWeight := float64(deductionRatio) / 100
	timeWeight := 1 - trafficWeight
	return trafficWeight, timeWeight
}

// proportion returns the given share of amount, clamped to [0, amount].
func proportion(amount int64, share float64) int64 {
	switch {
	case math.IsNaN(share) || share <= 0:
		return 0
	case share >= 1:
		return amount
	}
	return int64(float64(amount) * share)
}
