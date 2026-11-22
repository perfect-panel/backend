package usersub

import (
	"time"

	"github.com/perfect-panel/backend/pkg/timeutil"
)

// StatusSet names a group of subscription statuses that one business rule
// treats alike. The rules below are the only places that enumerate statuses;
// code that filters or branches on a status uses them instead of numbers.
type StatusSet []uint8

// Contains reports whether status belongs to the set.
func (s StatusSet) Contains(status uint8) bool {
	for _, member := range s {
		if member == status {
			return true
		}
	}
	return false
}

// Values returns the set as SQL parameters. A []uint8 is a []byte to the
// database driver and would bind as one binary value instead of a list.
func (s StatusSet) Values() []int64 {
	values := make([]int64, len(s))
	for i, member := range s {
		values[i] = int64(member)
	}
	return values
}

var (
	// LiveStatuses may use the service while their term and traffic last.
	// No write path creates Pending any more; rows left from older versions
	// are served like Active ones.
	LiveStatuses = StatusSet{SubscribeStatusPending, SubscribeStatusActive}
	// InTermStatuses are in their paid term, with or without traffic left:
	// the calendar traffic reset, the owner's node list and the token rotation
	// of all subscriptions apply to them.
	InTermStatuses = StatusSet{SubscribeStatusActive, SubscribeStatusFinished}
	// CurrentStatuses have not ended, been refunded or been stopped. Only they
	// may be cancelled by their owner, and marketing counts them as active.
	CurrentStatuses = StatusSet{SubscribeStatusPending, SubscribeStatusActive, SubscribeStatusFinished}
	// EndedStatuses are the ones marketing counts as inactive.
	EndedStatuses = StatusSet{SubscribeStatusExpired, SubscribeStatusStopped}
	// OwnerVisibleStatuses are listed to the subscription's owner; a refunded
	// or stopped subscription is not.
	OwnerVisibleStatuses = StatusSet{SubscribeStatusPending, SubscribeStatusActive, SubscribeStatusFinished, SubscribeStatusExpired}
	// HeldStatuses were refunded (Deducted) or stopped by an administrator.
	// Only an administrator brings them back; renewals, resets and provider
	// updates keep the hold.
	HeldStatuses = StatusSet{SubscribeStatusDeducted, SubscribeStatusStopped}
	// AllStatuses lists every status, for administrators.
	AllStatuses = StatusSet{
		SubscribeStatusPending, SubscribeStatusActive, SubscribeStatusFinished,
		SubscribeStatusExpired, SubscribeStatusDeducted, SubscribeStatusStopped,
	}
)

// OnHold reports whether the subscription was refunded (Deducted) or stopped
// by an administrator. Only an administrator may bring it back; a user
// purchase must not.
func OnHold(status uint8) bool {
	return HeldStatuses.Contains(status)
}

// NoLimitExpiry returns the expire_time written for a subscription without a
// time limit: the Unix epoch in the application zone. It is read at call
// time, never stored in a package variable: the process zone is set at
// startup (timeutil.LoadProcessLocation), and a zone-less database column
// (PostgreSQL's timestamp) keeps the writer's wall clock, so a marker built
// at package init would be stored as another value than one built later.
func NoLimitExpiry() time.Time {
	return time.UnixMilli(0).In(timeutil.Location())
}

// NoLimitBound returns the earliest expire_time that is a real term end. A
// stored expiry before it means "no time limit" whatever wall clock it was
// written in: the epoch lands within a day of 1970-01-01 in every zone, and
// no term ever ended in 1970. SQL conditions bind it as a parameter and
// compare with it instead of testing equality with NoLimitExpiry, so rows
// written in different wall clocks are all recognised.
func NoLimitBound() time.Time {
	return time.Date(1971, time.January, 1, 0, 0, 0, 0, timeutil.Location())
}

// NoExpiry reports whether expireTime means "no time limit": any value
// before NoLimitBound, which covers the epoch marker read in any zone and
// the zero time a NULL expire_time reads as.
func NoExpiry(expireTime time.Time) bool {
	return expireTime.Before(NoLimitBound())
}

// ExpiryFromMilli returns the expire_time for a term end given in Unix
// milliseconds by the administration API, where 0 means "no time limit".
// Any value NoExpiry accepts is stored as the marker NoLimitExpiry writes.
func ExpiryFromMilli(ms int64) time.Time {
	if expireTime := time.UnixMilli(ms).In(timeutil.Location()); !NoExpiry(expireTime) {
		return expireTime
	}
	return NoLimitExpiry()
}

// ExpiredAt reports whether the subscription's term is over at now. A
// subscription without a time limit never expires.
func (s *Subscribe) ExpiredAt(now time.Time) bool {
	return !NoExpiry(s.ExpireTime) && !s.ExpireTime.After(now)
}

// TrafficExhausted reports whether the traffic quota is used up; a zero quota
// is unlimited. It compares without adding the counters, so it cannot
// overflow.
func (s *Subscribe) TrafficExhausted() bool {
	return s.Traffic > 0 && (s.Upload >= s.Traffic || s.Download >= s.Traffic-s.Upload)
}

// Availability says whether a subscription may use the service at a given
// moment and, if not, why. It is the one answer to "can this subscription be
// served?": delivery, the storefront node list, the edge manifest and, through
// ServableCondition, the node user list all ask it.
type Availability uint8

const (
	// Available subscriptions reach real nodes.
	Available Availability = iota
	// Refunded subscriptions were cancelled with a refund (Deducted).
	Refunded
	// Stopped subscriptions are held by an administrator.
	Stopped
	// Expired subscriptions are past their term.
	Expired
	// TrafficExhausted subscriptions used up their traffic quota.
	TrafficExhausted
	// Inactive covers any status no rule serves.
	Inactive
)

// AvailabilityAt classifies the subscription at now. A hold wins over
// everything, an elapsed term over the traffic state, so the reason shown is
// the one the owner must resolve first.
func (s *Subscribe) AvailabilityAt(now time.Time) Availability {
	switch {
	case s.Status == SubscribeStatusDeducted:
		return Refunded
	case s.Status == SubscribeStatusStopped:
		return Stopped
	case s.Status == SubscribeStatusExpired || s.ExpiredAt(now):
		return Expired
	case s.Status == SubscribeStatusFinished || s.TrafficExhausted():
		return TrafficExhausted
	case LiveStatuses.Contains(s.Status):
		return Available
	default:
		return Inactive
	}
}

// ServableAt reports whether the subscription may use the service at now.
func (s *Subscribe) ServableAt(now time.Time) bool {
	return s.AvailabilityAt(now) == Available
}

// The SQL forms of the predicates above, as conditions over user_subscribe
// with the arguments they bind. TestServableConditionAgreesWithServableAt
// and TestExpiryConditionsAgreeWithExpiredAt keep them in step with the Go
// forms. A no-limit expiry is any value below NoLimitBound (see NoExpiry).

// ExpiredCondition is ExpiredAt(now); a NULL expiry never matches.
func ExpiredCondition(now time.Time) (string, []any) {
	return "expire_time <= ? AND expire_time >= ?", []any{now, NoLimitBound()}
}

// UnexpiredCondition is the negation of ExpiredAt(now).
func UnexpiredCondition(now time.Time) (string, []any) {
	return "(expire_time IS NULL OR expire_time < ? OR expire_time > ?)", []any{NoLimitBound(), now}
}

// ExpiringCondition selects the terms ending at or after from and before to.
// A no-limit expiry never does: a window starting below NoLimitBound is cut
// there.
func ExpiringCondition(from, to time.Time) (string, []any) {
	if bound := NoLimitBound(); from.Before(bound) {
		from = bound
	}
	return "expire_time >= ? AND expire_time < ?", []any{from, to}
}

// TrafficExhaustedCondition is TrafficExhausted.
const TrafficExhaustedCondition = "traffic > 0 AND upload + download >= traffic"

// ServableCondition is ServableAt(now). NULL traffic counters are never
// exhausted (IS NOT TRUE), matching the zero values they read as.
func ServableCondition(now time.Time) (string, []any) {
	unexpired, args := UnexpiredCondition(now)
	return "status IN ? AND " + unexpired + " AND (" + TrafficExhaustedCondition + ") IS NOT TRUE",
		append([]any{LiveStatuses.Values()}, args...)
}

// ReactivatedByTrafficReset reports whether clearing the traffic counters at
// now brings the subscription back: only a Finished (traffic-exhausted) one
// inside its term. A hold is never lifted by a reset and an expired
// subscription needs a renewal. Every traffic reset follows this rule: the
// calendar reset, the paid one, the quota task and the administrator's.
func (s *Subscribe) ReactivatedByTrafficReset(now time.Time) bool {
	return s.Status == SubscribeStatusFinished && !s.ExpiredAt(now) && !s.StartTime.After(now)
}

// ResetTraffic clears the traffic counters, reactivates the subscription when
// ReactivatedByTrafficReset allows it, and returns the columns it changed.
func (s *Subscribe) ResetTraffic(now time.Time) []string {
	s.Download, s.Upload = 0, 0
	columns := []string{"download", "upload"}
	if s.ReactivatedByTrafficReset(now) {
		s.Status = SubscribeStatusActive
		s.FinishedAt = nil
		columns = append(columns, "status", "finished_at")
	}
	return columns
}

// LocalControlColumns are the user_subscribe columns local operations may
// write on a provider-managed subscription: credentials, the owner's note,
// the traffic counters and the administrative hold. The payment provider owns
// every other column (plan, order, term).
var LocalControlColumns = []string{"token", "uuid", "note", "upload", "download", "status", "finished_at"}
