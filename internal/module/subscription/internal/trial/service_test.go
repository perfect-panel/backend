package trial

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/period"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/subtest"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

func newTrialService(f *subtest.Fixture, policy *Policy) *Service {
	return NewService(Deps{
		Plans:       f.Store.Subscribe(),
		Cache:       f.Store.UserSubscription(),
		Store:       f.Store,
		TrialPolicy: func() Policy { return *policy },
	})
}

// trialUser is the account every test registers.
const trialUser int64 = 7

// trials lists the test account's subscriptions.
func (f trialFixture) trials(t *testing.T) []usersub.Subscribe {
	t.Helper()
	var subs []usersub.Subscribe
	if err := f.DB.Where("user_id = ?", trialUser).Find(&subs).Error; err != nil {
		t.Fatal(err)
	}
	return subs
}

type trialFixture struct{ *subtest.Fixture }

// A registration grants the configured trial once, however often the event
// is delivered.
func TestGrantTrialGrantsOncePerRegistration(t *testing.T) {
	f := trialFixture{subtest.New(t)}
	f.Plan(t, subscribe.Subscribe{Id: 3, Traffic: 1 << 30})
	policy := &Policy{Enabled: true, PlanID: 3, Duration: 2, TimeUnit: "Day"}
	svc := newTrialService(f.Fixture, policy)
	ctx := context.Background()

	before := timeutil.Now()
	for range 3 {
		if err := svc.GrantTrial(ctx, trialUser); err != nil {
			t.Fatal(err)
		}
	}
	subs := f.trials(t)
	if len(subs) != 1 {
		t.Fatalf("trials granted = %d, want 1", len(subs))
	}
	trial := subs[0]
	if trial.SubscribeId != 3 || trial.Traffic != 1<<30 || trial.Status != usersub.SubscribeStatusActive || trial.Token == "" || trial.UUID == "" || trial.OrderId != 0 {
		t.Fatalf("trial = %+v", trial)
	}
	// The node credential is a random UUID, not a time-ordered one.
	if parsed, err := uuid.Parse(trial.UUID); err != nil || parsed[6]>>4 != 4 {
		t.Fatalf("trial node credential %q is not a version 4 UUID (%v)", trial.UUID, err)
	}
	if want, err := period.App().TermEnd(period.UnitDay, 2, trial.StartTime); err != nil || !trial.ExpireTime.Equal(want) {
		t.Fatalf("trial ends %v, want two days after %v (%v)", trial.ExpireTime, trial.StartTime, err)
	}
	if trial.StartTime.Before(before.Add(-time.Second)) {
		t.Fatalf("trial starts %v, before the grant", trial.StartTime)
	}
}

// A disabled trial still consumes the event: enabling it later grants no
// trial to that registration.
func TestGrantTrialConsumesTheEventWhenDisabled(t *testing.T) {
	f := trialFixture{subtest.New(t)}
	f.Plan(t, subscribe.Subscribe{Id: 3})
	policy := &Policy{Enabled: false, PlanID: 3, Duration: 1, TimeUnit: "Day"}
	svc := newTrialService(f.Fixture, policy)
	ctx := context.Background()
	if err := svc.GrantTrial(ctx, trialUser); err != nil {
		t.Fatal(err)
	}
	policy.Enabled = true
	if err := svc.GrantTrial(ctx, trialUser); err != nil {
		t.Fatal(err)
	}
	if subs := f.trials(t); len(subs) != 0 {
		t.Fatalf("a late policy change granted %d trials", len(subs))
	}
}

// A trial unit no rule knows fails the grant instead of granting a trial that
// ends where it starts; the event is retried and grants once the setting is
// fixed.
func TestGrantTrialRejectsAnUnknownUnitUntilFixed(t *testing.T) {
	f := trialFixture{subtest.New(t)}
	f.Plan(t, subscribe.Subscribe{Id: 3})
	policy := &Policy{Enabled: true, PlanID: 3, Duration: 1, TimeUnit: ""}
	svc := newTrialService(f.Fixture, policy)
	ctx := context.Background()
	if err := svc.GrantTrial(ctx, trialUser); !errors.Is(err, period.ErrUnknownUnit) {
		t.Fatalf("GrantTrial = %v, want ErrUnknownUnit", err)
	}
	if subs := f.trials(t); len(subs) != 0 {
		t.Fatalf("a failed grant created %d trials", len(subs))
	}
	policy.TimeUnit = "Hour"
	if err := svc.GrantTrial(ctx, trialUser); err != nil {
		t.Fatal(err)
	}
	if subs := f.trials(t); len(subs) != 1 {
		t.Fatalf("the retried grant created %d trials, want 1", len(subs))
	}
}

// A grant that fails to commit leaves no marker, so the retry grants.
func TestGrantTrialRetriesAfterARolledBackGrant(t *testing.T) {
	f := trialFixture{subtest.New(t)}
	f.Plan(t, subscribe.Subscribe{Id: 3})
	svc := newTrialService(f.Fixture, &Policy{Enabled: true, PlanID: 3, Duration: 1, TimeUnit: "Day"})
	ctx := context.Background()
	f.Store.FailNextCommits(1)
	if err := svc.GrantTrial(ctx, trialUser); !errors.Is(err, subtest.ErrInjectedRollback) {
		t.Fatalf("GrantTrial = %v, want the rollback", err)
	}
	if err := svc.GrantTrial(ctx, trialUser); err != nil {
		t.Fatal(err)
	}
	if subs := f.trials(t); len(subs) != 1 {
		t.Fatalf("trials after the retry = %d, want 1", len(subs))
	}
}
