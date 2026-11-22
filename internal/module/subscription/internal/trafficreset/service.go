// Package trafficreset applies the plans' calendar traffic resets: on the 1st
// of every month, monthly on the start day and yearly on the start date (the
// rules live in period).
//
// A run resets each due subscription at most once per day. Every batch
// commits its resets, their done-markers (user_subscribe.traffic_reset_at)
// and their audit rows in one subscription transaction, so a run that fails
// part way can be repeated: the repeat skips what the failed run committed
// and resets the rest. The marker also makes a run missed on the reset day
// (no run at all, or every retry failed) good on the next run: a subscription
// is due while its most recent reset day is later than the day it was last
// reset for (see resetOwed). Repeating is the task queue's job; this package
// never schedules itself. Only the module facade may reach it.
package trafficreset

import (
	"context"
	"errors"
	"time"

	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/period"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// defaultBatchSize bounds the rows one reset transaction locks.
const defaultBatchSize = 500

// Store is the persistence the reset needs: the plans and candidates it
// reads, and the subscription transaction it resets in.
type Store interface {
	repository.SubscriptionTransactor
	SubscriptionTraffic() repository.SubscriptionTrafficRepo
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Store Store
	Plans repository.SubscribeRepo
	// Now returns the current time; nil means timeutil.Now.
	Now func() time.Time
	// Calendar evaluates the reset days; the zero value means period.App().
	Calendar *period.Calendar
	// BatchSize bounds the subscriptions reset per transaction; zero means
	// defaultBatchSize.
	BatchSize int
}

// Service applies the calendar traffic resets due today.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// resetOrder is the order the cycles run in, the order the old task used.
var resetOrder = []struct {
	cycle period.Cycle
	name  string
}{
	{period.CycleYearly, "yearly"},
	{period.CycleFirstOfMonth, "first of month"},
	{period.CycleMonthly, "monthly"},
}

// ResetDue clears the traffic of every subscription owed a calendar reset
// today: its reset falls on today, or fell on an earlier day it has not been
// reset for. The cycles are independent: one failing does not stop the
// others, and the returned error joins every failure so the task queue
// retries the run.
func (s *Service) ResetDue(ctx context.Context) error {
	now := s.now()
	cal := s.calendar()
	day := cal.DayStart(now)
	var errs []error
	for _, step := range resetOrder {
		reset, err := s.resetCycle(ctx, cal, step.cycle, now, day)
		fields := []logger.LogField{
			logger.Field("cycle", step.name),
			logger.Field("day", day.Format(time.DateOnly)),
			logger.Field("reset", reset),
		}
		if err != nil {
			logger.WithContext(ctx).Errorw("[TrafficReset] Reset failed; the retried run resumes it", append(fields, logger.Field("error", err.Error()))...)
			errs = append(errs, xerr.Wrapf(err, xerr.ERROR, "%s traffic reset", step.name))
			continue
		}
		logger.WithContext(ctx).Infow("[TrafficReset] Reset completed", fields...)
	}
	return errors.Join(errs...)
}

// resetCycle resets the subscriptions of cycle owed a reset on day and
// returns how many it reset.
func (s *Service) resetCycle(ctx context.Context, cal period.Calendar, cycle period.Cycle, now, day time.Time) (int, error) {
	planIDs, err := s.deps.Plans.QueryResetCycleSubscribeIds(ctx, int(cycle))
	if err != nil {
		return 0, xerr.Wrapf(err, xerr.DatabaseQueryError, "query plans")
	}
	if len(planIDs) == 0 {
		return 0, nil
	}
	candidates, err := s.deps.Store.SubscriptionTraffic().FindTrafficResetCandidates(ctx, planIDs, now, day)
	if err != nil {
		return 0, xerr.Wrapf(err, xerr.DatabaseQueryError, "query reset candidates")
	}
	due := make([]int64, 0, len(candidates))
	for _, sub := range candidates {
		if resetOwed(cal, cycle, sub, day) {
			due = append(due, sub.Id)
		}
	}

	reset := 0
	batchSize := s.batchSize()
	for start := 0; start < len(due); start += batchSize {
		n, err := s.resetBatch(ctx, due[start:min(start+batchSize, len(due))], now, day)
		reset += n
		if err != nil {
			s.clearPlanCaches(ctx, reset, planIDs)
			return reset, err
		}
	}
	s.clearPlanCaches(ctx, reset, planIDs)
	return reset, nil
}

// resetOwed reports whether the subscription is owed cycle's reset on day: its
// most recent reset day at or before day is later than the day it was last
// reset for. A run missed on the reset day (the scheduler or the database was
// down, or every retry failed) is thus made good by the next run instead of
// waiting a whole cycle, and two missed cycles are made good by one reset.
//
// A subscription never reset yet is owed a reset on its reset days only. The
// done-marker is newer than the reset itself: rows written before it existed
// carry none although the resets of their current cycle ran, so reading
// "never" as "missed" would clear every such subscription once at the
// upgrade. From its first recorded reset on, every subscription catches up.
func resetOwed(cal period.Calendar, cycle period.Cycle, sub *usersub.Subscribe, day time.Time) bool {
	if sub.TrafficResetAt == nil {
		return cal.ResetsOn(cycle, sub.StartTime, day)
	}
	last, ok := cal.LastReset(cycle, sub.StartTime, day)
	return ok && last.After(cal.DayStart(*sub.TrafficResetAt))
}

// resetBatch resets the batch's subscriptions that are still due, with their
// done-markers and audit rows, in one transaction.
func (s *Service) resetBatch(ctx context.Context, ids []int64, now, day time.Time) (int, error) {
	var reset []*usersub.Subscribe
	err := s.deps.Store.InSubscriptionTx(ctx, func(store repository.SubscriptionStore) error {
		var err error
		reset, err = store.SubscriptionTraffic().ResetSubscribeTrafficOnce(ctx, ids, now, day)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "reset traffic")
		}
		if len(reset) == 0 {
			return nil
		}
		logs, err := auditRows(reset, now)
		if err != nil {
			return err
		}
		return xerr.Wrapf(store.Log().InsertBatch(ctx, logs, len(logs)), xerr.DatabaseInsertError, "insert reset audit rows")
	})
	if err != nil {
		return 0, err
	}
	return len(reset), nil
}

// auditRows records one automatic reset per subscription.
func auditRows(subs []*usersub.Subscribe, now time.Time) ([]*log.SystemLog, error) {
	rows := make([]*log.SystemLog, 0, len(subs))
	for _, sub := range subs {
		content, err := (&log.ResetSubscribe{Type: log.ResetSubscribeTypeAuto, UserId: sub.UserId, Timestamp: now.UnixMilli()}).Marshal()
		if err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "marshal reset audit row of subscription %d", sub.Id)
		}
		rows = append(rows, &log.SystemLog{
			Type:     log.TypeResetSubscribe.Uint8(),
			ObjectID: sub.Id,
			Date:     now.Format(time.DateOnly),
			Content:  string(content),
		})
	}
	return rows, nil
}

// clearPlanCaches drops the server user lists of the plans once any
// subscription changed: a reactivated subscription must reach its nodes. The
// subscriptions' own entries were invalidated with their commit.
func (s *Service) clearPlanCaches(ctx context.Context, reset int, planIDs []int64) {
	if reset == 0 {
		return
	}
	if err := s.deps.Plans.ClearCache(ctx, planIDs...); err != nil {
		logger.WithContext(ctx).Errorw("[TrafficReset] Clear plan caches failed",
			logger.Field("plan_ids", planIDs), logger.Field("error", err.Error()))
	}
}

func (s *Service) now() time.Time {
	if s.deps.Now != nil {
		return s.deps.Now()
	}
	return timeutil.Now()
}

func (s *Service) calendar() period.Calendar {
	if s.deps.Calendar != nil {
		return *s.deps.Calendar
	}
	return period.App()
}

func (s *Service) batchSize() int {
	if s.deps.BatchSize > 0 {
		return s.deps.BatchSize
	}
	return defaultBatchSize
}
