package sweep

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/subtest"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"gorm.io/gorm"
)

type expiringReminder struct {
	userID        int64
	planName      string
	expireAt      time.Time
	renewalAmount int64
}

// recordingNotifier records every owner notice.
type recordingNotifier struct {
	reminders []expiringReminder
	expired   []string
	exhausted []string
}

var _ Notifier = (*recordingNotifier)(nil)

func (n *recordingNotifier) NotifySubscriptionExpiring(_ context.Context, userID int64, planName string, expireAt time.Time, renewalAmount int64) {
	n.reminders = append(n.reminders, expiringReminder{userID: userID, planName: planName, expireAt: expireAt, renewalAmount: renewalAmount})
}

func (n *recordingNotifier) NotifySubscriptionExpired(_ context.Context, email string, _ time.Time) {
	n.expired = append(n.expired, email)
}

func (n *recordingNotifier) NotifyTrafficExceeded(_ context.Context, email string) {
	n.exhausted = append(n.exhausted, email)
}

// owners is the identity side: account states and email bindings.
type owners struct {
	deleted map[int64]bool
	emails  map[int64]string
}

var (
	_ OwnerStateReader = owners{}
	_ OwnerEmailReader = owners{}
)

func (o owners) FindAccountState(_ context.Context, id int64) (*user.AccountState, error) {
	state := &user.AccountState{Id: id}
	if o.deleted[id] {
		state.DeletedAt = gorm.DeletedAt{Time: timeutil.Now(), Valid: true}
	}
	return state, nil
}

func (o owners) FindUserAuthMethodsByUserIds(_ context.Context, method string, userIDs []int64) ([]*user.AuthMethods, error) {
	var methods []*user.AuthMethods
	for _, id := range userIDs {
		if email, ok := o.emails[id]; ok && method == "email" {
			methods = append(methods, &user.AuthMethods{UserId: id, AuthType: method, AuthIdentifier: email})
		}
	}
	return methods, nil
}

func newSweepService(f *subtest.Fixture, who owners) (*Service, *recordingNotifier) {
	notifier := &recordingNotifier{}
	return NewService(Deps{
		UserSubs: f.Store.UserSubscription(),
		Plans:    f.Store.Subscribe(),
		Cache:    f.Store.UserSubscription(),
		Store:    f.Store,
		Emails:   who,
		Owners:   who,
		Notify:   notifier,
	}), notifier
}

func newReminderFixture(t *testing.T, deleted ...int64) (*subtest.Fixture, *Service, *recordingNotifier) {
	f := subtest.New(t)
	f.Plan(t, subscribe.Subscribe{Id: 9, Name: "Pro 月付", UnitPrice: 1890})
	who := owners{deleted: map[int64]bool{}}
	for _, id := range deleted {
		who.deleted[id] = true
	}
	svc, notifier := newSweepService(f, who)
	return f, svc, notifier
}

// A subscription sits inside the reminder window for days, so the notice must
// be announced once per expiry rather than on every daily pass; one expiring
// after the window waits for a later pass.
func TestRemindExpiringSubscribesAnnouncesOncePerExpiry(t *testing.T) {
	f, svc, notifier := newReminderFixture(t)
	ctx := context.Background()
	expireAt := timeutil.Now().Add(48 * time.Hour).Truncate(time.Millisecond)
	f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 9, ExpireTime: expireAt, Status: usersub.SubscribeStatusActive})
	f.Subscription(t, usersub.Subscribe{UserId: 8, SubscribeId: 9, ExpireTime: timeutil.Now().Add(expiryReminderWindow + time.Hour), Status: usersub.SubscribeStatusActive})

	for range 2 {
		if err := svc.RemindExpiringSubscribes(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(notifier.reminders) != 1 {
		t.Fatalf("reminders = %+v, want one", notifier.reminders)
	}
	got := notifier.reminders[0]
	if got.userID != 7 || got.planName != "Pro 月付" || got.renewalAmount != 1890 || !got.expireAt.Equal(expireAt) {
		t.Fatalf("reminder = %+v, want the owner, plan name, renewal price and expiry", got)
	}
}

// A renewal moves the expiry, which makes the subscription eligible again —
// the marker is keyed by the expiry it announced.
func TestRemindExpiringSubscribesAnnouncesAgainAfterRenewal(t *testing.T) {
	f, svc, notifier := newReminderFixture(t)
	ctx := context.Background()
	sub := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 9, ExpireTime: timeutil.Now().Add(24 * time.Hour), Status: usersub.SubscribeStatusActive})
	if err := svc.RemindExpiringSubscribes(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.DB.Model(&usersub.Subscribe{}).Where("id = ?", sub.Id).Update("expire_time", timeutil.Now().Add(48*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RemindExpiringSubscribes(ctx); err != nil {
		t.Fatal(err)
	}
	if len(notifier.reminders) != 2 {
		t.Fatalf("reminders = %d, want one per expiry", len(notifier.reminders))
	}
}

// Deleting a user leaves the subscription active, so the sweep still finds
// it; the deleted owner must not be reminded.
func TestRemindExpiringSubscribesSkipsDeletedOwners(t *testing.T) {
	f, svc, notifier := newReminderFixture(t, 8)
	expireAt := timeutil.Now().Add(48 * time.Hour)
	f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 9, ExpireTime: expireAt, Status: usersub.SubscribeStatusActive})
	f.Subscription(t, usersub.Subscribe{UserId: 8, SubscribeId: 9, ExpireTime: expireAt, Status: usersub.SubscribeStatusActive})
	if err := svc.RemindExpiringSubscribes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notifier.reminders) != 1 || notifier.reminders[0].userID != 7 {
		t.Fatalf("reminders = %+v, want the live owner only", notifier.reminders)
	}
}

// An unreadable plan must still produce a notice: the owner needs the warning
// more than the plan's name.
func TestRemindExpiringSubscribesToleratesMissingPlan(t *testing.T) {
	f, svc, notifier := newReminderFixture(t)
	f.Subscription(t, usersub.Subscribe{UserId: 8, SubscribeId: 404, ExpireTime: timeutil.Now().Add(time.Hour), Status: usersub.SubscribeStatusActive})
	if err := svc.RemindExpiringSubscribes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notifier.reminders) != 1 {
		t.Fatalf("reminders = %d, want 1", len(notifier.reminders))
	}
	if got := notifier.reminders[0]; got.planName != "" || got.renewalAmount != 0 {
		t.Fatalf("reminder = %+v, want an empty plan summary", got)
	}
}

// Provider-managed subscriptions renew through their provider, and already
// finished ones need no reminder.
func TestRemindExpiringSubscribesSkipsProviderAndFinishedSubscriptions(t *testing.T) {
	f, svc, notifier := newReminderFixture(t)
	expireAt := timeutil.Now().Add(48 * time.Hour)
	f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 9, ExpireTime: expireAt, Status: usersub.SubscribeStatusActive, EntitlementSource: "apple"})
	f.Subscription(t, usersub.Subscribe{UserId: 8, SubscribeId: 9, ExpireTime: expireAt, Status: usersub.SubscribeStatusFinished})
	if err := svc.RemindExpiringSubscribes(context.Background()); err != nil {
		t.Fatal(err)
	}
	var reminded []int64
	for _, reminder := range notifier.reminders {
		reminded = append(reminded, reminder.userID)
	}
	sort.Slice(reminded, func(i, j int) bool { return reminded[i] < reminded[j] })
	if len(reminded) != 0 {
		t.Fatalf("reminded %v", reminded)
	}
}
