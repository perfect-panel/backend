package plan

import (
	"context"
	"testing"
	"time"
	"uuid"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/subtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

type planFixture struct {
	*subtest.Fixture
	svc      *Service
	notified int
}

func newPlanFixture(t *testing.T) *planFixture {
	f := &planFixture{Fixture: subtest.New(t)}
	f.svc = NewService(Deps{
		Plans:             f.Store.Subscribe(),
		UserSubs:          f.Store.UserSubscription(),
		Store:             f.Store,
		NotifyPlanChanged: func() { f.notified++ },
	})
	return f
}

func (f *planFixture) plan(t *testing.T, id int64) *subscribe.Subscribe {
	t.Helper()
	var plan subscribe.Subscribe
	if err := f.DB.First(&plan, id).Error; err != nil {
		t.Fatalf("load plan %d: %v", id, err)
	}
	return &plan
}

func validCreate(name string) *dto.CreateSubscribeRequest {
	return &dto.CreateSubscribeRequest{Name: name, UnitTime: "Month", UnitPrice: 1000, Inventory: -1, Traffic: 100, ResetCycle: 2}
}

func TestCreateSubscribeValidatesUnitsAndCycles(t *testing.T) {
	f := newPlanFixture(t)
	ctx := context.Background()
	if err := f.svc.CreateSubscribe(ctx, validCreate("gold")); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*dto.CreateSubscribeRequest){
		"unknown unit":         func(r *dto.CreateSubscribeRequest) { r.UnitTime = "Fortnight" },
		"empty unit":           func(r *dto.CreateSubscribeRequest) { r.UnitTime = "" },
		"unknown reset cycle":  func(r *dto.CreateSubscribeRequest) { r.ResetCycle = 4 },
		"negative inventory":   func(r *dto.CreateSubscribeRequest) { r.Inventory = -2 },
		"deduction over 100 %": func(r *dto.CreateSubscribeRequest) { r.DeductionRatio = 101 },
		"zero discount":        func(r *dto.CreateSubscribeRequest) { r.Discount = []dto.SubscribeDiscount{{Quantity: 3}} },
	} {
		req := validCreate(name)
		mutate(req)
		if err := f.svc.CreateSubscribe(ctx, req); xerr.CodeOf(err) != xerr.InvalidParams {
			t.Fatalf("%s: CreateSubscribe = %v, want a parameter error", name, err)
		}
	}
	var count int64
	if err := f.DB.Model(&subscribe.Subscribe{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("plans = %d, %v; the invalid ones must not be stored", count, err)
	}
}

// Node tags take over the node selection: explicit nodes are dropped so the
// two selectors are not AND-combined.
func TestUpdateSubscribeReplacesNodesWithTagsAndNotifies(t *testing.T) {
	f := newPlanFixture(t)
	ctx := context.Background()
	f.Plan(t, subscribe.Subscribe{Id: 1, Name: "gold", UnitTime: "Month", Nodes: "1,2"})
	show := true
	err := f.svc.UpdateSubscribe(ctx, &dto.UpdateSubscribeRequest{
		Id: 1, Name: "gold+", UnitTime: "Year", Inventory: -1, Nodes: dto.StringInt64Slice{3}, NodeTags: []string{"edge"},
		Discount: []dto.SubscribeDiscount{{Quantity: 12, Discount: 80}}, Show: &show, Sell: &show,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := f.plan(t, 1)
	if got.Name != "gold+" || got.UnitTime != "Year" || got.Nodes != "" || got.NodeTags != "edge" || got.Discount == "" || f.notified != 1 {
		t.Fatalf("updated plan %+v, notifications %d", got, f.notified)
	}
	if err := f.svc.UpdateSubscribe(ctx, &dto.UpdateSubscribeRequest{Id: 99, Name: "ghost", UnitTime: "Month"}); xerr.CodeOf(err) != xerr.DatabaseQueryError {
		t.Fatalf("updating a missing plan = %v", err)
	}
}

// An update that leaves a switch out keeps the stored one: writing NULL
// into show or sell failed on their NOT NULL columns.
func TestUpdateSubscribeKeepsTheSwitchesTheRequestOmits(t *testing.T) {
	f := newPlanFixture(t)
	on, off := true, false
	f.Plan(t, subscribe.Subscribe{Id: 1, Name: "gold", UnitTime: "Month", Show: &on, Sell: &on, AllowDeduction: &off, RenewalReset: &on})

	if err := f.svc.UpdateSubscribe(context.Background(), &dto.UpdateSubscribeRequest{Id: 1, Name: "gold+", UnitTime: "Month", Inventory: -1}); err != nil {
		t.Fatalf("UpdateSubscribe without the switches: %v", err)
	}
	got := f.plan(t, 1)
	if got.Name != "gold+" || got.Show == nil || !*got.Show || got.Sell == nil || !*got.Sell ||
		got.AllowDeduction == nil || *got.AllowDeduction || got.RenewalReset == nil || !*got.RenewalReset {
		t.Fatalf("updated plan %+v, want the stored switches kept", got)
	}

	if err := f.svc.UpdateSubscribe(context.Background(), &dto.UpdateSubscribeRequest{Id: 1, Name: "gold+", UnitTime: "Month", Inventory: -1, Sell: &off}); err != nil {
		t.Fatal(err)
	}
	if got := f.plan(t, 1); got.Sell == nil || *got.Sell || !*got.Show {
		t.Fatalf("updated plan %+v, want only sell switched off", got)
	}
}

// A plan with a current user subscription (pending, active, or exhausted
// inside its term) cannot be deleted; one whose subscriptions all ended
// (expired, refunded, stopped) can.
func TestDeleteSubscribeRefusesPlansInUse(t *testing.T) {
	f := newPlanFixture(t)
	ctx := context.Background()
	for id := int64(1); id <= 6; id++ {
		f.Plan(t, subscribe.Subscribe{Id: id})
	}
	f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, Status: usersub.SubscribeStatusActive})
	f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 2, Status: usersub.SubscribeStatusExpired})
	f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 2, Status: usersub.SubscribeStatusDeducted})
	f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 2, Status: usersub.SubscribeStatusStopped})
	f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 4, Status: usersub.SubscribeStatusFinished})
	f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 5, Status: usersub.SubscribeStatusPending})

	for _, id := range []int64{1, 4, 5} {
		if err := f.svc.DeleteSubscribe(ctx, &dto.DeleteSubscribeRequest{Id: id}); xerr.CodeOf(err) != xerr.SubscribeIsUsedError {
			t.Fatalf("deleting plan %d in use = %v", id, err)
		}
	}
	if err := f.svc.DeleteSubscribe(ctx, &dto.DeleteSubscribeRequest{Id: 2}); err != nil {
		t.Fatal(err)
	}
	// The batch is all or nothing.
	for _, ids := range [][]int64{{3, 1}, {3, 4}, {6, 5}} {
		if err := f.svc.BatchDeleteSubscribe(ctx, &dto.BatchDeleteSubscribeRequest{Ids: ids}); xerr.CodeOf(err) != xerr.SubscribeIsUsedError {
			t.Fatalf("batch deleting plans %v in use = %v", ids, err)
		}
	}
	if err := f.svc.BatchDeleteSubscribe(ctx, &dto.BatchDeleteSubscribeRequest{Ids: []int64{3, 6}}); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	if err := f.DB.Model(&subscribe.Subscribe{}).Order("id").Pluck("id", &ids).Error; err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 || ids[0] != 1 || ids[1] != 4 || ids[2] != 5 {
		t.Fatalf("plans left = %v, want [1 4 5]", ids)
	}
}

// The admin list counts each plan's live subscriptions as sold.
func TestGetSubscribeListCountsLiveSubscriptions(t *testing.T) {
	f := newPlanFixture(t)
	ctx := context.Background()
	f.Plan(t, subscribe.Subscribe{Id: 1, Name: "gold", NodeTags: "edge", Nodes: "4"})
	f.Plan(t, subscribe.Subscribe{Id: 2, Name: "silver"})
	for _, status := range []uint8{usersub.SubscribeStatusActive, usersub.SubscribeStatusPending, usersub.SubscribeStatusFinished, usersub.SubscribeStatusExpired} {
		f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, Status: status})
	}
	resp, err := f.svc.GetSubscribeList(ctx, &dto.GetSubscribeListRequest{Page: 1, Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	sold := map[string]int64{}
	for _, item := range resp.List {
		sold[item.Name] = item.Sold
	}
	if resp.Total != 2 || sold["gold"] != 2 || sold["silver"] != 0 {
		t.Fatalf("sold = %v (total %d), want gold's two live subscriptions", sold, resp.Total)
	}
	details, err := f.svc.GetSubscribeDetails(ctx, &dto.GetSubscribeDetailsRequest{Id: 1})
	if err != nil || details.Name != "gold" || len(details.Nodes) != 1 || details.Nodes[0] != 4 || details.NodeTags[0] != "edge" {
		t.Fatalf("details = %+v, %v", details, err)
	}
}

func TestSubscribeSortReordersFromTheLowestPosition(t *testing.T) {
	f := newPlanFixture(t)
	ctx := context.Background()
	f.Plan(t, subscribe.Subscribe{Id: 1, Sort: 5})
	f.Plan(t, subscribe.Subscribe{Id: 2, Sort: 6})
	f.Plan(t, subscribe.Subscribe{Id: 3, Sort: 7})
	if err := f.svc.SubscribeSort(ctx, &dto.SubscribeSortRequest{Sort: []dto.SortItem{{Id: 3}, {Id: 1}, {Id: 2}}}); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int64]int64{3: 5, 1: 6, 2: 7} {
		if got := f.plan(t, id).Sort; got != want {
			t.Fatalf("plan %d sort = %d, want %d", id, got, want)
		}
	}
}

// Rotating every token covers the subscriptions in their term, in batched
// writes: each gets a new token and node credential, the previous tokens stop
// resolving, and the other rows keep theirs.
func TestResetAllSubscribeTokenRotatesSubscriptionsInTerm(t *testing.T) {
	f := newPlanFixture(t)
	ctx := context.Background()
	f.Plan(t, subscribe.Subscribe{Id: 1})
	future := time.Now().Add(time.Hour)
	active := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 1, ExpireTime: future, Status: usersub.SubscribeStatusActive, Token: "active-token", UUID: "active-uuid", Upload: 5, Note: "kept"})
	finished := f.Subscription(t, usersub.Subscribe{UserId: 8, SubscribeId: 1, ExpireTime: future, Status: usersub.SubscribeStatusFinished, Token: "finished-token"})
	expired := f.Subscription(t, usersub.Subscribe{UserId: 9, SubscribeId: 1, Status: usersub.SubscribeStatusExpired, Token: "expired-token"})
	if _, err := f.Store.UserSubscription().FindOneSubscribeByToken(ctx, "active-token"); err != nil {
		t.Fatal(err)
	}

	resp, err := f.svc.ResetAllSubscribeToken(ctx)
	if err != nil || !resp.Success {
		t.Fatalf("ResetAllSubscribeToken = %+v, %v", resp, err)
	}
	gotActive, gotFinished := f.Load(t, active.Id), f.Load(t, finished.Id)
	if gotActive.Token == "active-token" || gotActive.UUID == "active-uuid" || gotActive.Upload != 5 || gotActive.Note != "kept" {
		t.Fatalf("active subscription after rotation: %+v", gotActive)
	}
	// The rotated credentials are random: a 32-hex token and a version 4 UUID.
	if !usersub.AcceptableToken(gotActive.Token) || len(gotActive.Token) != 32 {
		t.Fatalf("rotated token %q is not an issued token", gotActive.Token)
	}
	if parsed, err := uuid.Parse(gotActive.UUID); err != nil || parsed[6]>>4 != 4 {
		t.Fatalf("rotated node credential %q is not a version 4 UUID (%v)", gotActive.UUID, err)
	}
	if gotFinished.Token == "finished-token" {
		t.Fatal("the exhausted subscription kept its token")
	}
	if got := f.Load(t, expired.Id); got.Token != "expired-token" {
		t.Fatalf("an ended subscription was rotated: %+v", got)
	}
	if f.Cached("cache:user:subscribe:token:active-token") {
		t.Fatal("the previous token still resolves from the cache")
	}
}

func TestSubscribeGroups(t *testing.T) {
	f := newPlanFixture(t)
	ctx := context.Background()
	for _, name := range []string{"a", "b", "c"} {
		if err := f.svc.CreateSubscribeGroup(ctx, &dto.CreateSubscribeGroupRequest{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.svc.UpdateSubscribeGroup(ctx, &dto.UpdateSubscribeGroupRequest{Id: 1, Name: "renamed"}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteSubscribeGroup(ctx, &dto.DeleteSubscribeGroupRequest{Id: 2}); err != nil {
		t.Fatal(err)
	}
	resp, err := f.svc.GetSubscribeGroupList(ctx)
	if err != nil || resp.Total != 2 || resp.List[0].Name != "renamed" {
		t.Fatalf("groups = %+v, %v", resp, err)
	}
	if err := f.svc.BatchDeleteSubscribeGroup(ctx, &dto.BatchDeleteSubscribeGroupRequest{Ids: []int64{1, 3}}); err != nil {
		t.Fatal(err)
	}
	if resp, err := f.svc.GetSubscribeGroupList(ctx); err != nil || resp.Total != 0 {
		t.Fatalf("groups after batch delete = %+v, %v", resp, err)
	}
}
