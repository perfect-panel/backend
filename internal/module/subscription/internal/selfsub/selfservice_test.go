package selfsub

import (
	"context"
	"sort"
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// racingSubs is the real subscription repository with a write landing right
// after the note edit reads the subscription.
type racingSubs struct {
	subs      UserSubscriptions
	afterRead func()
}

var _ UserSubscriptions = (*racingSubs)(nil)

func (r *racingSubs) FindOneSubscribe(ctx context.Context, id int64) (*usersub.Subscribe, error) {
	return r.subs.FindOneSubscribe(ctx, id)
}

func (r *racingSubs) FindOneUserSubscribe(ctx context.Context, id int64) (*usersub.SubscribeDetails, error) {
	details, err := r.subs.FindOneUserSubscribe(ctx, id)
	r.afterRead()
	return details, err
}

func (r *racingSubs) QueryUserSubscribe(ctx context.Context, userID int64, statuses ...int64) ([]*usersub.SubscribeDetails, error) {
	return r.subs.QueryUserSubscribe(ctx, userID, statuses...)
}

func (r *racingSubs) UpdateSubscribeColumns(ctx context.Context, data *usersub.Subscribe, columns ...string) error {
	return r.subs.UpdateSubscribeColumns(ctx, data, columns...)
}

// A note edit writes the note alone: traffic accounted between its read and
// its write stays, and the cached row is dropped. Only the owner may edit.
func TestUpdateUserSubscribeNoteWritesOnlyTheNote(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.Plan(t, subscribe.Subscribe{Id: refundPlan})
	sub := f.Subscription(t, usersub.Subscribe{UserId: refundBuyer, SubscribeId: refundPlan, ExpireTime: time.Now().Add(24 * time.Hour), Status: usersub.SubscribeStatusActive, Token: "note-token", Upload: 5})
	if _, err := f.Store.UserSubscription().FindOneSubscribeByToken(ctx, "note-token"); err != nil {
		t.Fatal(err)
	}
	svc := NewService(Deps{UserSubs: &racingSubs{
		subs: f.Store.UserSubscription(),
		afterRead: func() {
			if err := f.DB.Model(&usersub.Subscribe{}).Where("id = ?", sub.Id).Update("upload", 99).Error; err != nil {
				t.Error(err)
			}
		},
	}})

	if err := svc.UpdateUserSubscribeNote(as(refundBuyer), &dto.UpdateUserSubscribeNoteRequest{UserSubscribeId: sub.Id, Note: "office"}); err != nil {
		t.Fatal(err)
	}
	if got := f.Load(t, sub.Id); got.Note != "office" || got.Upload != 99 || got.Token != "note-token" || got.Status != usersub.SubscribeStatusActive {
		t.Fatalf("subscription after the note edit: %+v", got)
	}
	if f.Cached("cache:user:subscribe:token:note-token") {
		t.Fatal("the cached row still has the old note")
	}

	for name, ctx := range map[string]context.Context{"another user": as(refundBuyer + 1), "anonymous": context.Background()} {
		if err := svc.UpdateUserSubscribeNote(ctx, &dto.UpdateUserSubscribeNoteRequest{UserSubscribeId: sub.Id, Note: "mine"}); xerr.CodeOf(err) != xerr.InvalidAccess {
			t.Fatalf("%s: UpdateUserSubscribeNote = %v, want InvalidAccess", name, err)
		}
	}
	if got := f.Load(t, sub.Id); got.Note != "office" {
		t.Fatalf("a refused edit wrote the note %q", got.Note)
	}
}

// A token reset gives the owner's subscription a new token and node
// credential and keeps everything else. The previous token stops resolving,
// and the plan's cache entries, which carry the node credentials, are
// dropped. Another user's subscription is refused.
func TestResetUserSubscribeTokenRotatesTheOwnersCredentials(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.Plan(t, subscribe.Subscribe{Id: refundPlan})
	sub := f.Subscription(t, usersub.Subscribe{
		UserId: refundBuyer, SubscribeId: refundPlan, ExpireTime: time.Now().Add(24 * time.Hour), Status: usersub.SubscribeStatusActive,
		Token: "old-token", UUID: "old-uuid", Upload: 5, Note: "kept",
	})
	subs := f.Store.UserSubscription()
	if _, err := subs.FindOneSubscribeByToken(ctx, "old-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Store.Subscribe().FindOne(ctx, refundPlan); err != nil {
		t.Fatal(err)
	}

	for name, ctx := range map[string]context.Context{"another user": as(refundBuyer + 1), "anonymous": context.Background()} {
		if err := f.svc.ResetUserSubscribeToken(ctx, &dto.ResetUserSubscribeTokenRequest{UserSubscribeId: sub.Id}); xerr.CodeOf(err) != xerr.InvalidAccess {
			t.Fatalf("%s: ResetUserSubscribeToken = %v, want InvalidAccess", name, err)
		}
	}
	if got := f.Load(t, sub.Id); got.Token != "old-token" || got.UUID != "old-uuid" {
		t.Fatalf("a refused reset rotated the credentials: %+v", got)
	}

	if err := f.svc.ResetUserSubscribeToken(as(refundBuyer), &dto.ResetUserSubscribeTokenRequest{UserSubscribeId: sub.Id}); err != nil {
		t.Fatal(err)
	}
	got := f.Load(t, sub.Id)
	if got.Token == "" || got.Token == "old-token" || got.UUID == "" || got.UUID == "old-uuid" {
		t.Fatalf("credentials after the reset: token %q uuid %q", got.Token, got.UUID)
	}
	if got.Upload != 5 || got.Note != "kept" || got.Status != usersub.SubscribeStatusActive {
		t.Fatalf("the reset changed more than the credentials: %+v", got)
	}
	if f.Cached("cache:user:subscribe:token:old-token") || f.Cached("cache:subscribe:id:9") {
		t.Fatal("the previous token or the plan stays cached")
	}
	if _, err := subs.FindOneSubscribeByToken(ctx, "old-token"); err == nil {
		t.Fatal("the previous token still resolves")
	}
	if resolved, err := subs.FindOneSubscribeByToken(ctx, got.Token); err != nil || resolved.Id != sub.Id {
		t.Fatalf("the new token resolves to %+v, %v", resolved, err)
	}
}

// The owner's list shows their pending, active, finished and expired
// subscriptions with the plan's renewal discounts and the next calendar
// traffic reset; cancelled and stopped ones are left out.
func TestQueryUserSubscribeListsTheOwnersSubscriptions(t *testing.T) {
	f := newFixture(t)
	f.Plan(t, subscribe.Subscribe{Id: refundPlan, ResetCycle: 2, Discount: `[{"quantity":3,"discount":90}]`})
	future := time.Now().Add(90 * 24 * time.Hour)
	for _, status := range []uint8{
		usersub.SubscribeStatusPending, usersub.SubscribeStatusActive, usersub.SubscribeStatusFinished,
		usersub.SubscribeStatusExpired, usersub.SubscribeStatusDeducted, usersub.SubscribeStatusStopped,
	} {
		f.Subscription(t, usersub.Subscribe{UserId: refundBuyer, SubscribeId: refundPlan, ExpireTime: future, Status: status})
	}
	f.Subscription(t, usersub.Subscribe{UserId: refundBuyer + 1, SubscribeId: refundPlan, ExpireTime: future, Status: usersub.SubscribeStatusActive})

	resp, err := f.svc.QueryUserSubscribe(as(refundBuyer))
	if err != nil {
		t.Fatal(err)
	}
	var statuses []int
	for _, item := range resp.List {
		statuses = append(statuses, int(item.Status))
		if item.UserId != refundBuyer || item.ResetTime <= time.Now().UnixMilli() || item.Short == "" || len(item.Subscribe.Discount) != 1 || item.Subscribe.Discount[0].Quantity != 3 {
			t.Fatalf("listed subscription %+v", item)
		}
	}
	sort.Ints(statuses)
	if resp.Total != 4 || len(statuses) != 4 || statuses[0] != 0 || statuses[3] != 3 {
		t.Fatalf("listed statuses %v (total %d), want pending through finished", statuses, resp.Total)
	}
	if _, err := f.svc.QueryUserSubscribe(context.Background()); xerr.CodeOf(err) != xerr.InvalidAccess {
		t.Fatalf("anonymous list = %v", err)
	}
}
