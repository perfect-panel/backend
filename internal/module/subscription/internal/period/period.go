// Package period holds the subscription module's calendar rules: the time
// units that size a subscription term and the traffic reset cycles of a plan.
// It is the one place these rules live; the calendar reset, renewals, the
// refund quote and the owner's next-reset display all ask it.
//
// A Calendar evaluates the rules in one time zone whatever zone a time value
// carries (a value read from the database comes in the driver's zone), so
// "the 15th" is the same day for every caller. Production code uses App(),
// the application zone of pkg/timeutil.
package period

import (
	"errors"
	"fmt"
	"time"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// Unit is a purchasable subscription time unit, a plan's unit_time.
type Unit string

const (
	UnitYear   Unit = "Year"
	UnitMonth  Unit = "Month"
	UnitDay    Unit = "Day"
	UnitHour   Unit = "Hour"
	UnitMinute Unit = "Minute"
	// UnitNoLimit is a term without end.
	UnitNoLimit Unit = "NoLimit"
)

// ErrUnknownUnit rejects a time unit no rule knows. A term computed from one
// used to end where it started: a subscription expired on arrival.
var ErrUnknownUnit = errors.New("unknown subscription time unit")

// ParseUnit validates a stored or requested unit name.
func ParseUnit(name string) (Unit, error) {
	switch unit := Unit(name); unit {
	case UnitYear, UnitMonth, UnitDay, UnitHour, UnitMinute, UnitNoLimit:
		return unit, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownUnit, name)
}

// Cycle is a plan's calendar traffic reset rule, its reset_cycle.
type Cycle int64

const (
	// CycleNone has no calendar reset: the traffic quota lasts the term.
	CycleNone Cycle = 0
	// CycleFirstOfMonth resets on the 1st of every month.
	CycleFirstOfMonth Cycle = 1
	// CycleMonthly resets every month on the start day, or on the month's
	// last day when the month is shorter.
	CycleMonthly Cycle = 2
	// CycleYearly resets every year on the start date; a Feb 29 start resets
	// on Feb 28 in common years.
	CycleYearly Cycle = 3
)

// Valid reports whether c is a known cycle.
func (c Cycle) Valid() bool {
	return c >= CycleNone && c <= CycleYearly
}

// Calendar evaluates the rules in one time zone.
type Calendar struct {
	loc *time.Location
}

// In returns the calendar of loc (UTC when nil).
func In(loc *time.Location) Calendar {
	if loc == nil {
		loc = time.UTC
	}
	return Calendar{loc: loc}
}

// App returns the calendar of the application time zone.
func App() Calendar {
	return In(timeutil.Location())
}

// Location returns the calendar's time zone.
func (c Calendar) Location() *time.Location {
	return c.zone()
}

func (c Calendar) zone() *time.Location {
	if c.loc == nil {
		return time.UTC
	}
	return c.loc
}

// DayStart returns midnight of t's day.
func (c Calendar) DayStart(t time.Time) time.Time {
	t = t.In(c.zone())
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, c.zone())
}

// TermEnd returns the end of a term of quantity units beginning at start.
// Calendar units follow time.AddDate, so a month from Jan 31 ends on Mar 2 or
// 3; a term without limit ends at the no-limit sentinel.
func (c Calendar) TermEnd(unit Unit, quantity int64, start time.Time) (time.Time, error) {
	start = start.In(c.zone())
	switch unit {
	case UnitYear:
		return start.AddDate(int(quantity), 0, 0), nil
	case UnitMonth:
		return start.AddDate(0, int(quantity), 0), nil
	case UnitDay:
		return start.AddDate(0, 0, int(quantity)), nil
	case UnitHour:
		return start.Add(time.Hour * time.Duration(quantity)), nil
	case UnitMinute:
		return start.Add(time.Minute * time.Duration(quantity)), nil
	case UnitNoLimit:
		return usersub.NoLimitExpiry(), nil
	}
	return time.Time{}, fmt.Errorf("%w: %q", ErrUnknownUnit, unit)
}

// ResetsOn reports whether day, any instant of it, is a reset day of cycle
// for a subscription that started at start.
func (c Calendar) ResetsOn(cycle Cycle, start, day time.Time) bool {
	start, day = start.In(c.zone()), day.In(c.zone())
	switch cycle {
	case CycleFirstOfMonth:
		return day.Day() == 1
	case CycleMonthly:
		return day.Day() == c.resetDate(day.Year(), day.Month(), start.Day()).Day()
	case CycleYearly:
		return day.Month() == start.Month() && day.Day() == c.resetDate(day.Year(), start.Month(), start.Day()).Day()
	}
	return false
}

// NextReset returns the first reset of cycle after now, at the start of its
// day, for a subscription that started at start. ok is false when the cycle
// never resets.
func (c Calendar) NextReset(cycle Cycle, start, now time.Time) (next time.Time, ok bool) {
	start, now = start.In(c.zone()), now.In(c.zone())
	switch cycle {
	case CycleFirstOfMonth:
		return time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, c.zone()), true
	case CycleMonthly:
		next = c.resetDate(now.Year(), now.Month(), start.Day())
		if !next.After(now) {
			next = c.resetDate(now.Year(), now.Month()+1, start.Day())
		}
		return next, true
	case CycleYearly:
		next = c.resetDate(now.Year(), start.Month(), start.Day())
		if !next.After(now) {
			next = c.resetDate(now.Year()+1, start.Month(), start.Day())
		}
		return next, true
	}
	return time.Time{}, false
}

// LastReset returns the most recent reset of cycle at or before at, at the
// start of its day, for a subscription that started at start. ok is false
// when the cycle never resets. It is the reset a subscription is owed when
// no run has reset it for that day yet.
func (c Calendar) LastReset(cycle Cycle, start, at time.Time) (last time.Time, ok bool) {
	last, _, ok = c.CycleAt(cycle, start, at)
	return last, ok
}

// CycleAt returns the reset cycle containing now: from the last reset at or
// before now up to the next one. ok is false when the cycle never resets.
func (c Calendar) CycleAt(cycle Cycle, start, now time.Time) (from, to time.Time, ok bool) {
	start, now = start.In(c.zone()), now.In(c.zone())
	switch cycle {
	case CycleFirstOfMonth:
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, c.zone())
		return from, from.AddDate(0, 1, 0), true
	case CycleMonthly:
		from = c.resetDate(now.Year(), now.Month(), start.Day())
		if from.After(now) {
			from = c.resetDate(now.Year(), now.Month()-1, start.Day())
		}
		return from, c.resetDate(from.Year(), from.Month()+1, start.Day()), true
	case CycleYearly:
		from = c.resetDate(now.Year(), start.Month(), start.Day())
		if from.After(now) {
			from = c.resetDate(now.Year()-1, start.Month(), start.Day())
		}
		return from, c.resetDate(from.Year()+1, start.Month(), start.Day()), true
	}
	return time.Time{}, time.Time{}, false
}

// ResetBetween reports whether cycle has a reset day on any date from from's
// day through to's day, both included.
func (c Calendar) ResetBetween(cycle Cycle, start, from, to time.Time) bool {
	first, last := c.DayStart(from), c.DayStart(to)
	if last.Before(first) {
		return false
	}
	// The first reset strictly after the instant before first's midnight is
	// the first one on or after first's day.
	next, ok := c.NextReset(cycle, start, first.Add(-time.Nanosecond))
	return ok && !next.After(last)
}

// resetDate returns midnight of day in the given month, moved to the month's
// last day when the month is shorter. Months outside 1-12 roll into the
// neighbouring year.
func (c Calendar) resetDate(year int, month time.Month, day int) time.Time {
	first := time.Date(year, month, 1, 0, 0, 0, 0, c.zone())
	if last := first.AddDate(0, 1, -1).Day(); day > last {
		day = last
	}
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, c.zone())
}
