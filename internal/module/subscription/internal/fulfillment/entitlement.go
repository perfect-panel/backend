package fulfillment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"uuid"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/entitlement"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

var ErrEntitlementConflict = errors.New("entitlement ownership or revision conflict")

func entitlementKey(parts ...string) string {
	data, _ := json.Marshal(parts)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// ReconcileEntitlement projects a verified, reconciled billing snapshot. It
// deliberately has no HTTP adapter and performs no Apple signature validation.
// The billing adapter must supply a durable revision after resolving historical
// transactions and revocations; raw notification delivery order is not valid.
func (s *Service) ReconcileEntitlement(ctx context.Context, cmd dto.ReconcileEntitlementCommand) (*dto.EntitlementResult, error) {
	if err := validateEntitlement(cmd); err != nil {
		return nil, err
	}
	cmd.PeriodStart = cmd.PeriodStart.UTC().Truncate(time.Millisecond)
	cmd.PeriodEnd = cmd.PeriodEnd.UTC().Truncate(time.Millisecond)
	cmd.GraceUntil = canonicalTime(cmd.GraceUntil)
	cmd.RevokedAt = canonicalTime(cmd.RevokedAt)
	if err := validateEntitlement(cmd); err != nil {
		return nil, err
	}
	orderInfo, err := s.deps.Orders.FindOne(ctx, cmd.OrderID)
	if err != nil {
		return nil, err
	}
	if orderInfo.UserId != cmd.UserID || orderInfo.SubscribeId != cmd.PlanID || orderInfo.TradeNo != cmd.TransactionKey || orderInfo.Method != appleIAPMethod || (orderInfo.Status != order.StatusPaid && orderInfo.Status != order.StatusFinished) {
		return nil, ErrEntitlementConflict
	}
	return s.reconcileEntitlement(ctx, cmd)
}

func canonicalTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC().Truncate(time.Millisecond)
	return &v
}

func (s *Service) reconcileEntitlement(ctx context.Context, cmd dto.ReconcileEntitlementCommand) (*dto.EntitlementResult, error) {
	payload, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("encode the entitlement snapshot: %w", err)
	}
	r := &reconciliation{
		cmd:         cmd,
		stateID:     entitlementKey(cmd.Source, cmd.Scope, cmd.SubscriptionKey),
		periodID:    entitlementKey(cmd.Source, cmd.Scope, cmd.TransactionKey),
		payload:     string(payload),
		fingerprint: entitlementKey(string(payload)),
		singleModel: s.deps.SingleModel != nil && s.deps.SingleModel(),
		result:      &dto.EntitlementResult{},
	}
	if err := s.deps.Store.InSubscriptionTx(ctx, func(store repository.SubscriptionStore) error {
		return r.apply(ctx, store)
	}); err != nil {
		return nil, err
	}
	// Return cache failures so durable callers retry even after DB commit.
	// Replays perform these invalidations again without mutating periods.
	if err := s.deps.Cache.ClearSubscribeCache(ctx, r.projected); err != nil {
		return nil, err
	}
	// Include every previously referenced plan: several reconciliations may
	// commit while caches are unavailable, before any retry can invalidate.
	planIDs, err := s.deps.Store.Entitlement().PlanIDs(ctx, r.stateID)
	if err != nil {
		return nil, err
	}
	if err := s.deps.Plans.ClearCache(ctx, planIDs...); err != nil {
		return nil, err
	}
	return r.result, nil
}

// reconciliation applies one entitlement snapshot inside a subscription
// transaction: it projects the snapshot onto the user subscription it owns,
// records the billing period and the entitlement state, and keeps the
// snapshot as a revision.
type reconciliation struct {
	cmd                  dto.ReconcileEntitlementCommand
	stateID, periodID    string
	payload, fingerprint string
	singleModel          bool
	result               *dto.EntitlementResult
	projected            *usersub.Subscribe
}

func (r *reconciliation) apply(ctx context.Context, store repository.SubscriptionStore) error {
	cmd := r.cmd
	// The user lock serializes first purchases and quota checks. The state
	// primary key additionally protects the same chain across users.
	if err := store.UserSubscription().LockUserSerial(ctx, cmd.UserID); err != nil {
		return err
	}
	state, err := store.Entitlement().FindStateForUpdate(ctx, r.stateID)
	if err != nil {
		return err
	}
	if state != nil {
		current, err := r.loadProjection(ctx, store, state)
		if err != nil || current {
			return err
		}
	}
	plan, err := store.Subscribe().FindOne(ctx, cmd.PlanID)
	if err != nil {
		return err
	}
	period, err := store.Entitlement().FindPeriod(ctx, r.periodID)
	if err != nil {
		return err
	}
	if period != nil && (period.EntitlementID != r.stateID || period.PlanID != cmd.PlanID || period.OrderID != cmd.OrderID || period.BillingInterval != cmd.BillingInterval || !period.StartAt.Equal(cmd.PeriodStart)) {
		return ErrEntitlementConflict
	}
	trafficLimit := plan.Traffic
	if period != nil {
		trafficLimit = period.TrafficLimit
	}
	now := timeutil.Now()
	until := entitlementAccessUntil(cmd)
	if state == nil {
		err = r.createProjection(ctx, store, plan, trafficLimit, until, now)
	} else {
		// Switching plan or refunding does not grant another quota reset.
		// A reset requires a previously unseen provider transaction.
		r.projected.SubscribeId = cmd.PlanID
		r.projected.Traffic = trafficLimit
		projectEntitlement(r.projected, cmd, until, now, period == nil && resetsTraffic(cmd, plan, now))
		err = store.UserSubscription().ApplyEntitlementProjection(ctx, r.projected)
	}
	if err != nil {
		return err
	}
	if err := r.recordPeriod(ctx, store, period, plan, trafficLimit, now); err != nil {
		return err
	}
	if err := r.recordState(ctx, store, state); err != nil {
		return err
	}
	r.result.UserSubscribeID, r.result.Revision, r.result.AccessUntil, r.result.Applied = r.projected.Id, cmd.Revision, until, true
	return nil
}

// loadProjection locks the user subscription an existing entitlement owns.
// current reports that the snapshot is not newer than the recorded one:
// the result then describes the recorded state and nothing changes. An
// equal revision with other content is a conflict.
func (r *reconciliation) loadProjection(ctx context.Context, store repository.SubscriptionStore, state *entitlement.State) (current bool, err error) {
	cmd := r.cmd
	if state.UserID != cmd.UserID || state.Mode != cmd.Mode {
		return false, ErrEntitlementConflict
	}
	r.projected, err = store.UserSubscription().FindOneSubscribeForUpdate(ctx, state.UserSubscribeID)
	if err != nil {
		return false, err
	}
	if r.projected.UserId != cmd.UserID || r.projected.EntitlementSource != cmd.Source {
		return false, ErrEntitlementConflict
	}
	if cmd.Revision > state.Revision {
		return false, nil
	}
	if cmd.Revision == state.Revision && r.fingerprint != state.Fingerprint {
		return false, ErrEntitlementConflict
	}
	r.result.UserSubscribeID, r.result.Revision, r.result.AccessUntil = r.projected.Id, state.Revision, r.projected.ExpireTime
	return true, nil
}

// createProjection creates the user subscription of a first entitlement,
// under the same single-subscription and quota rules as a purchase. A refusal
// asks the provider to deliver the entitlement again later.
func (r *reconciliation) createProjection(ctx context.Context, store repository.SubscriptionStore, plan *subscribe.Subscribe, trafficLimit int64, until, now time.Time) error {
	cmd := r.cmd
	if r.singleModel {
		blocked, err := store.UserSubscription().HasBlockingSubscription(ctx, cmd.UserID)
		if err != nil {
			return err
		}
		if blocked {
			return fmt.Errorf("single subscription mode exceeds limit; entitlement delivery must be retried")
		}
	}
	if plan.Quota > 0 {
		count, err := store.UserSubscription().CountQuotaConsumingSubscriptions(ctx, cmd.UserID, cmd.PlanID)
		if err != nil {
			return err
		}
		if count >= plan.Quota {
			return fmt.Errorf("subscription quota exceeded; entitlement delivery must be retried")
		}
	}
	r.projected = &usersub.Subscribe{UserId: cmd.UserID, OrderId: cmd.OrderID, SubscribeId: cmd.PlanID,
		StartTime: cmd.PeriodStart, ExpireTime: until, Traffic: trafficLimit,
		Token: usersub.NewToken(), UUID: uuid.NewV4().String(), EntitlementSource: cmd.Source}
	projectEntitlement(r.projected, cmd, until, now, false)
	return store.UserSubscription().InsertSubscribe(ctx, r.projected)
}

// recordPeriod records the snapshot's billing period, or extends the end of
// a period seen before: providers correct and extend end dates, and replaying
// a period never repeats its original traffic reset.
func (r *reconciliation) recordPeriod(ctx context.Context, store repository.SubscriptionStore, period *entitlement.Period, plan *subscribe.Subscribe, trafficLimit int64, now time.Time) error {
	cmd := r.cmd
	if period != nil {
		period.EndAt = cmd.PeriodEnd
		return store.Entitlement().UpdatePeriod(ctx, period)
	}
	return store.Entitlement().InsertPeriod(ctx, &entitlement.Period{ID: r.periodID, EntitlementID: r.stateID, TransactionKey: cmd.TransactionKey,
		BillingInterval: cmd.BillingInterval,
		TrafficLimit:    trafficLimit,
		UserSubscribeID: r.projected.Id, OrderID: cmd.OrderID, PlanID: cmd.PlanID, StartAt: cmd.PeriodStart,
		EndAt: cmd.PeriodEnd, TrafficReset: resetsTraffic(cmd, plan, now)})
}

// recordState records the entitlement's new state and keeps the snapshot as
// its revision.
func (r *reconciliation) recordState(ctx context.Context, store repository.SubscriptionStore, state *entitlement.State) error {
	cmd := r.cmd
	newState := &entitlement.State{ID: r.stateID, Source: cmd.Source, Scope: cmd.Scope, SubscriptionKey: cmd.SubscriptionKey,
		UserID: cmd.UserID, UserSubscribeID: r.projected.Id, Revision: cmd.Revision, Fingerprint: r.fingerprint,
		PeriodID: r.periodID, Mode: cmd.Mode, Status: cmd.Status, AutoRenew: cmd.AutoRenew,
		GraceUntil: cmd.GraceUntil, RevokedAt: cmd.RevokedAt}
	var err error
	if state == nil {
		err = store.Entitlement().InsertState(ctx, newState)
	} else {
		err = store.Entitlement().UpdateState(ctx, newState)
	}
	if err != nil {
		return err
	}
	return store.Entitlement().InsertRevision(ctx, &entitlement.Revision{ID: r.stateID + ":" + strconv.FormatInt(cmd.Revision, 10), EntitlementID: r.stateID, Payload: r.payload})
}

// resetsTraffic reports whether the snapshot's period resets the plan's
// traffic: an active period in force now, of a plan without its own reset
// cycle, whose provider asked for a reset.
func resetsTraffic(cmd dto.ReconcileEntitlementCommand, plan *subscribe.Subscribe, now time.Time) bool {
	return cmd.ResetTraffic && plan.ResetCycle == 0 && cmd.Status == "active" && !cmd.PeriodStart.After(now) && cmd.PeriodEnd.After(now)
}

func entitlementAccessUntil(cmd dto.ReconcileEntitlementCommand) time.Time {
	until := cmd.PeriodEnd
	if cmd.Status == "grace" && cmd.GraceUntil != nil {
		until = *cmd.GraceUntil
	}
	if cmd.RevokedAt != nil && cmd.RevokedAt.Before(until) {
		until = *cmd.RevokedAt
	}
	return until
}

func projectEntitlement(sub *usersub.Subscribe, cmd dto.ReconcileEntitlementCommand, until, now time.Time, reset bool) {
	sub.ExpireTime = until
	if reset {
		sub.Upload, sub.Download = 0, 0
	}
	// Administrative holds and local cancellation are independent of payment.
	if usersub.OnHold(sub.Status) {
		return
	}
	if cmd.Status == "revoked" || cmd.Status == "expired" || cmd.Status == "billing_retry" || !until.After(now) || cmd.PeriodStart.After(now) {
		sub.Status = usersub.SubscribeStatusExpired
		sub.FinishedAt = &now
	} else if sub.TrafficExhausted() {
		sub.Status = usersub.SubscribeStatusFinished
		sub.FinishedAt = &now
	} else {
		sub.Status = usersub.SubscribeStatusActive
		sub.FinishedAt = nil
	}
}

func validateEntitlement(c dto.ReconcileEntitlementCommand) error {
	// A future scheduled product is not the current entitlement. The billing
	// scheduler must retain it and issue a new snapshot when it takes effect.
	now := timeutil.Now()
	if c.PeriodStart.After(now) {
		return fmt.Errorf("entitlement period has not started; retry at period start")
	}
	if (c.Status == "expired" || c.Status == "billing_retry") && c.PeriodEnd.After(now) {
		return fmt.Errorf("inactive entitlement has a future paid deadline")
	}
	if c.RevokedAt != nil && c.RevokedAt.After(now) {
		return fmt.Errorf("revocation has not taken effect")
	}
	for _, s := range []string{c.Scope, c.SubscriptionKey, c.TransactionKey} {
		if s == "" || len(s) > 255 || strings.TrimSpace(s) != s {
			return fmt.Errorf("invalid entitlement identity")
		}
	}
	if c.Source != "apple" || c.UserID <= 0 || c.PlanID <= 0 || c.OrderID <= 0 || c.Revision <= 0 || c.PeriodStart.UnixMilli() <= 0 || !c.PeriodEnd.After(c.PeriodStart) {
		return fmt.Errorf("invalid entitlement period")
	}
	if c.Mode != "auto_renewable" {
		return fmt.Errorf("only auto-renewable Apple subscriptions are supported")
	}
	if c.BillingInterval != "month" && c.BillingInterval != "year" {
		return fmt.Errorf("only monthly or yearly Apple subscriptions are supported")
	}
	switch c.Status {
	case "active", "expired", "billing_retry", "revoked", "grace":
	default:
		return fmt.Errorf("invalid entitlement status")
	}
	if c.Status == "grace" && (c.GraceUntil == nil || !c.GraceUntil.After(c.PeriodEnd)) {
		return fmt.Errorf("invalid grace period")
	}
	if c.Status != "grace" && c.GraceUntil != nil {
		return fmt.Errorf("grace deadline without grace state")
	}
	if (c.Status == "revoked") != (c.RevokedAt != nil) || (c.RevokedAt != nil && c.RevokedAt.UnixMilli() <= 0) {
		return fmt.Errorf("invalid revocation")
	}
	return nil
}
