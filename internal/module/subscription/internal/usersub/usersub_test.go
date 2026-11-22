package usersub

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/subtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// owners is a compile-checked OwnerReader over a fixed set of accounts; it
// records the account cache refreshes the admin flows ask for.
type owners struct {
	accounts  map[int64]*user.User
	refreshed []int64
}

var _ OwnerReader = (*owners)(nil)

func newOwners(accounts ...*user.User) *owners {
	o := &owners{accounts: make(map[int64]*user.User, len(accounts))}
	for _, u := range accounts {
		o.accounts[u.Id] = u
	}
	return o
}

func (o *owners) FindOne(_ context.Context, id int64) (*user.User, error) {
	if u, ok := o.accounts[id]; ok {
		return u, nil
	}
	return nil, errors.New("user not found")
}

func (o *owners) ClearUserCacheOf(_ context.Context, users ...*user.User) error {
	for _, u := range users {
		o.refreshed = append(o.refreshed, u.Id)
	}
	return nil
}

func newAdminService(f *subtest.Fixture, singleModel bool) *Service {
	return NewService(Deps{
		Plans:       f.Store.Subscribe(),
		UserSubs:    f.Store.UserSubscription(),
		Users:       newOwners(&user.User{Id: 7}),
		Cache:       f.Store.UserSubscription(),
		Store:       f.Store,
		SingleModel: func() bool { return singleModel },
	})
}

// An administrator's edit sets plan, term and traffic and nothing else: the
// owner's note and the credentials survive it, and the status follows the
// new term.
func TestUpdateUserSubscribeKeepsTheOwnersNoteAndCredentials(t *testing.T) {
	tests := []struct {
		name        string
		expiredAt   func() int64
		wantStatus  uint8
		wantNoLimit bool
	}{
		// ExpiredAt 0 means no time limit: the stored marker stays active;
		// the node user list would drop an Expired one.
		{"no-limit marker stays active", func() int64 { return 0 }, usersub.SubscribeStatusActive, true},
		{"past expiry marks expired", func() int64 { return time.Now().Add(-time.Hour).UnixMilli() }, usersub.SubscribeStatusExpired, false},
		{"future expiry is active", func() int64 { return time.Now().Add(time.Hour).UnixMilli() }, usersub.SubscribeStatusActive, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := subtest.New(t)
			f.Plan(t, subscribe.Subscribe{Id: 1})
			f.Plan(t, subscribe.Subscribe{Id: 2})
			finished := time.Now().Add(-time.Minute)
			sub := f.Subscription(t, usersub.Subscribe{
				UserId: 7, SubscribeId: 1, ExpireTime: time.Now().Add(-time.Hour), FinishedAt: &finished,
				Status: usersub.SubscribeStatusExpired, Note: "work laptop", Token: "keep-token", UUID: "keep-uuid",
			})

			err := newAdminService(f, false).UpdateUserSubscribe(context.Background(), &dto.UpdateUserSubscribeRequest{
				UserSubscribeId: sub.Id, SubscribeId: 2, ExpiredAt: tt.expiredAt(), Traffic: 100, Upload: 3, Download: 4,
			})
			if err != nil {
				t.Fatal(err)
			}
			got := f.Load(t, sub.Id)
			if got.Note != "work laptop" || got.Token != "keep-token" || got.UUID != "keep-uuid" {
				t.Fatalf("edit overwrote unrelated columns: %+v", got)
			}
			if got.SubscribeId != 2 || got.Traffic != 100 || got.Upload != 3 || got.Download != 4 || got.FinishedAt != nil || got.Status != tt.wantStatus {
				t.Fatalf("edit not applied: %+v, want status %d", got, tt.wantStatus)
			}
			if usersub.NoExpiry(got.ExpireTime) != tt.wantNoLimit || (tt.wantNoLimit && !got.ExpireTime.Equal(usersub.NoLimitExpiry())) {
				t.Fatalf("stored expiry = %v, want no limit %v", got.ExpireTime, tt.wantNoLimit)
			}
		})
	}
}

// An administrator's subscription without a term end stores the no-limit
// marker and is active.
func TestCreateUserSubscribeWithoutTermEndHasNoLimit(t *testing.T) {
	f := subtest.New(t)
	f.Plan(t, subscribe.Subscribe{Id: 1, Traffic: 50})
	if err := newAdminService(f, false).CreateUserSubscribe(context.Background(), &dto.CreateUserSubscribeRequest{UserId: 7, SubscribeId: 1}); err != nil {
		t.Fatal(err)
	}
	var created usersub.Subscribe
	if err := f.DB.Where("user_id = 7").First(&created).Error; err != nil {
		t.Fatal(err)
	}
	if !usersub.NoExpiry(created.ExpireTime) || !created.ExpireTime.Equal(usersub.NoLimitExpiry()) || created.Status != usersub.SubscribeStatusActive || !created.ServableAt(time.Now()) {
		t.Fatalf("created %+v, want an active subscription without a time limit", created)
	}
}

func TestUpdateUserSubscribeRefusesProviderManagedSubscriptions(t *testing.T) {
	f := subtest.New(t)
	sub := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, EntitlementSource: "apple", ExpireTime: time.Now().Add(time.Hour), Status: usersub.SubscribeStatusActive})
	err := newAdminService(f, false).UpdateUserSubscribe(context.Background(), &dto.UpdateUserSubscribeRequest{UserSubscribeId: sub.Id, SubscribeId: 1})
	if !errors.Is(err, usersub.ErrProviderManaged) {
		t.Fatalf("provider-managed edit error = %v", err)
	}
}

// The administrator's traffic reset follows the rule of every other reset:
// an exhausted subscription inside its term is active again; a hold or an
// expired term stays.
func TestResetUserSubscribeTrafficReactivatesExhaustedSubscriptions(t *testing.T) {
	future, past := time.Now().Add(time.Hour), time.Now().Add(-time.Hour)
	tests := []struct {
		name       string
		status     uint8
		expire     time.Time
		wantStatus uint8
	}{
		{"exhausted in term", usersub.SubscribeStatusFinished, future, usersub.SubscribeStatusActive},
		{"exhausted without time limit", usersub.SubscribeStatusFinished, usersub.NoLimitExpiry(), usersub.SubscribeStatusActive},
		{"exhausted and expired", usersub.SubscribeStatusFinished, past, usersub.SubscribeStatusFinished},
		{"expired", usersub.SubscribeStatusExpired, past, usersub.SubscribeStatusExpired},
		{"stopped", usersub.SubscribeStatusStopped, future, usersub.SubscribeStatusStopped},
		{"refunded", usersub.SubscribeStatusDeducted, future, usersub.SubscribeStatusDeducted},
		{"active", usersub.SubscribeStatusActive, future, usersub.SubscribeStatusActive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := subtest.New(t)
			f.Plan(t, subscribe.Subscribe{Id: 1})
			finished := time.Now().Add(-time.Minute)
			sub := f.Subscription(t, usersub.Subscribe{
				UserId: 7, SubscribeId: 1, ExpireTime: tt.expire, Traffic: 100, Upload: 60, Download: 40,
				Status: tt.status, FinishedAt: &finished, Note: "kept",
			})
			if err := newAdminService(f, false).ResetUserSubscribeTraffic(context.Background(), &dto.ResetUserSubscribeTrafficRequest{UserSubscribeId: sub.Id}); err != nil {
				t.Fatal(err)
			}
			got := f.Load(t, sub.Id)
			if got.Upload != 0 || got.Download != 0 || got.Status != tt.wantStatus || got.Note != "kept" {
				t.Fatalf("after reset: %+v, want status %d", got, tt.wantStatus)
			}
			if (got.FinishedAt == nil) != (tt.wantStatus == usersub.SubscribeStatusActive && tt.status == usersub.SubscribeStatusFinished) {
				t.Fatalf("finished_at = %v after the reset", got.FinishedAt)
			}
			// The reactivated subscription is servable again: the rule every
			// node-access check applies agrees with the reset.
			if want := tt.wantStatus == usersub.SubscribeStatusActive; got.ServableAt(time.Now()) != want {
				t.Fatalf("servable after reset = %v, want %v", !want, want)
			}
		})
	}
}

// The toggle reads the status under the row lock: a status the lifecycle
// sweep wrote after a cached read is what the toggle decides on.
func TestToggleUserSubscribeStatusDecidesOnTheStoredRow(t *testing.T) {
	f := subtest.New(t)
	f.Plan(t, subscribe.Subscribe{Id: 1})
	svc := newAdminService(f, false)
	ctx := context.Background()
	sub := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, ExpireTime: time.Now().Add(time.Hour), Status: usersub.SubscribeStatusActive, Upload: 5})

	if err := svc.ToggleUserSubscribeStatus(ctx, &dto.ToggleUserSubscribeStatusRequest{UserSubscribeId: sub.Id}); err != nil {
		t.Fatal(err)
	}
	if got := f.Load(t, sub.Id); got.Status != usersub.SubscribeStatusStopped || got.Upload != 5 {
		t.Fatalf("stop: %+v", got)
	}
	if err := svc.ToggleUserSubscribeStatus(ctx, &dto.ToggleUserSubscribeStatusRequest{UserSubscribeId: sub.Id}); err != nil {
		t.Fatal(err)
	}
	if got := f.Load(t, sub.Id); got.Status != usersub.SubscribeStatusActive {
		t.Fatalf("resume: %+v", got)
	}

	// Cache the Active row, then let the sweep expire it behind the cache.
	if _, err := f.Store.UserSubscription().FindOneSubscribe(ctx, sub.Id); err != nil {
		t.Fatal(err)
	}
	if err := f.DB.Model(&usersub.Subscribe{}).Where("id = ?", sub.Id).Update("status", usersub.SubscribeStatusExpired).Error; err != nil {
		t.Fatal(err)
	}
	err := svc.ToggleUserSubscribeStatus(ctx, &dto.ToggleUserSubscribeStatusRequest{UserSubscribeId: sub.Id})
	if err == nil || !strings.Contains(err.Error(), "status 3") {
		t.Fatalf("toggle of an expired subscription = %v, want refused", err)
	}
	if got := f.Load(t, sub.Id); got.Status != usersub.SubscribeStatusExpired {
		t.Fatalf("toggle rewrote the sweep's status: %+v", got)
	}
}

// An operator confirms a stop or a resume against the status they saw. If
// someone changed the subscription meanwhile, the change is refused rather
// than toggled from the new status, which would do the opposite.
func TestChangeUserSubscribeStatusIsConditional(t *testing.T) {
	f := subtest.New(t)
	f.Plan(t, subscribe.Subscribe{Id: 1})
	svc := newAdminService(f, false)
	ctx := context.Background()
	sub := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, ExpireTime: time.Now().Add(time.Hour), Status: usersub.SubscribeStatusActive})

	if err := svc.ChangeUserSubscribeStatus(ctx, sub.Id, usersub.SubscribeStatusActive, usersub.SubscribeStatusStopped); err != nil {
		t.Fatal(err)
	}
	if got := f.Load(t, sub.Id); got.Status != usersub.SubscribeStatusStopped {
		t.Fatalf("stop: %+v", got)
	}
	// A second "stop" confirmed against the Active status it saw earlier.
	err := svc.ChangeUserSubscribeStatus(ctx, sub.Id, usersub.SubscribeStatusActive, usersub.SubscribeStatusStopped)
	if xerr.CodeOf(err) != xerr.SubscriptionStatusChanged {
		t.Fatalf("stale stop = %v, want SubscriptionStatusChanged", err)
	}
	if got := f.Load(t, sub.Id); got.Status != usersub.SubscribeStatusStopped {
		t.Fatalf("a stale stop resumed the subscription: %+v", got)
	}
	// Only the stop/resume pair is an operator's change.
	for _, change := range [][2]uint8{
		{usersub.SubscribeStatusExpired, usersub.SubscribeStatusActive},
		{usersub.SubscribeStatusStopped, usersub.SubscribeStatusFinished},
	} {
		if err := svc.ChangeUserSubscribeStatus(ctx, sub.Id, change[0], change[1]); xerr.CodeOf(err) != xerr.SubscriptionStatusNotToggleable {
			t.Fatalf("change %d -> %d = %v, want SubscriptionStatusNotToggleable", change[0], change[1], err)
		}
	}
}

// A provider-managed subscription can be held and released, but not
// reactivated once its provider period is over.
func TestToggleUserSubscribeStatusKeepsTheProviderTerm(t *testing.T) {
	f := subtest.New(t)
	f.Plan(t, subscribe.Subscribe{Id: 1})
	svc := newAdminService(f, false)
	ctx := context.Background()
	current := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, EntitlementSource: "apple", ExpireTime: time.Now().Add(time.Hour), Status: usersub.SubscribeStatusActive})
	for _, want := range []uint8{usersub.SubscribeStatusStopped, usersub.SubscribeStatusActive} {
		if err := svc.ToggleUserSubscribeStatus(ctx, &dto.ToggleUserSubscribeStatusRequest{UserSubscribeId: current.Id}); err != nil {
			t.Fatal(err)
		}
		if got := f.Load(t, current.Id); got.Status != want {
			t.Fatalf("status = %d, want %d", got.Status, want)
		}
	}
	lapsed := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, EntitlementSource: "apple", ExpireTime: time.Now().Add(-time.Hour), Status: usersub.SubscribeStatusStopped})
	err := svc.ToggleUserSubscribeStatus(ctx, &dto.ToggleUserSubscribeStatusRequest{UserSubscribeId: lapsed.Id})
	if !errors.Is(err, usersub.ErrProviderManaged) {
		t.Fatalf("reactivating a lapsed provider period = %v, want ErrProviderManaged", err)
	}
	if got := f.Load(t, lapsed.Id); got.Status != usersub.SubscribeStatusStopped {
		t.Fatalf("lapsed provider subscription became %d", got.Status)
	}
}

// Resetting the token rotates the node credential too, and the previous
// token stops resolving at once: its cache entry goes with the commit.
func TestResetUserSubscribeTokenRotatesCredentialsAndDropsTheOldToken(t *testing.T) {
	f := subtest.New(t)
	f.Plan(t, subscribe.Subscribe{Id: 1})
	ctx := context.Background()
	sub := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, ExpireTime: time.Now().Add(time.Hour), Status: usersub.SubscribeStatusActive, Token: "old-token", UUID: "old-uuid", Note: "kept"})
	if _, err := f.Store.UserSubscription().FindOneSubscribeByToken(ctx, "old-token"); err != nil {
		t.Fatal(err)
	}
	if !f.Cached("cache:user:subscribe:token:old-token") {
		t.Fatal("fixture did not cache the token lookup")
	}

	if err := newAdminService(f, false).ResetUserSubscribeToken(ctx, &dto.ResetUserSubscribeTokenRequest{UserSubscribeId: sub.Id}); err != nil {
		t.Fatal(err)
	}
	got := f.Load(t, sub.Id)
	if got.Token == "old-token" || got.Token == "" || got.UUID == "old-uuid" || got.UUID == "" || got.Note != "kept" {
		t.Fatalf("credentials not rotated cleanly: %+v", got)
	}
	if f.Cached("cache:user:subscribe:token:old-token") {
		t.Fatal("the previous token still resolves from the cache")
	}
	if _, err := f.Store.UserSubscription().FindOneSubscribeByToken(ctx, "old-token"); err == nil {
		t.Fatal("the previous token still resolves")
	}
}

func TestCreateUserSubscribeHonoursSingleSubscriptionMode(t *testing.T) {
	f := subtest.New(t)
	f.Plan(t, subscribe.Subscribe{Id: 1, Traffic: 50})
	ctx := context.Background()
	f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, Status: usersub.SubscribeStatusExpired})

	err := newAdminService(f, true).CreateUserSubscribe(ctx, &dto.CreateUserSubscribeRequest{UserId: 7, SubscribeId: 1})
	if err == nil || !strings.Contains(err.Error(), "Single subscribe mode exceeds limit") {
		t.Fatalf("CreateUserSubscribe = %v, want the single-mode refusal", err)
	}
	// A refunded subscription does not block a new one.
	if err := f.DB.Model(&usersub.Subscribe{}).Where("user_id = 7").Update("status", usersub.SubscribeStatusDeducted).Error; err != nil {
		t.Fatal(err)
	}
	accounts := newOwners(&user.User{Id: 7})
	svc := NewService(Deps{Plans: f.Store.Subscribe(), UserSubs: f.Store.UserSubscription(), Users: accounts, Cache: f.Store.UserSubscription(), Store: f.Store, SingleModel: func() bool { return true }})
	if err := svc.CreateUserSubscribe(ctx, &dto.CreateUserSubscribeRequest{UserId: 7, SubscribeId: 1, ExpiredAt: time.Now().Add(time.Hour).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	var created usersub.Subscribe
	if err := f.DB.Where("user_id = 7 AND status = ?", usersub.SubscribeStatusActive).First(&created).Error; err != nil {
		t.Fatal(err)
	}
	if created.Traffic != 50 || created.Token == "" || created.UUID == "" || len(accounts.refreshed) != 1 {
		t.Fatalf("created %+v, account cache refreshes %v", created, accounts.refreshed)
	}
}
