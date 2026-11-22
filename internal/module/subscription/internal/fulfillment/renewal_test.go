package fulfillment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/period"
)

// A renewal resets traffic by the plan's reset rules, never because the
// renewal happens to fall on the expiry's day of the month.
func TestRenewalResetsTrafficFollowsTheResetCycle(t *testing.T) {
	cal := period.In(time.UTC)
	at := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 12, 0, 0, 0, time.UTC) }
	yes := true
	tests := []struct {
		name   string
		plan   subscribe.Subscribe
		start  time.Time
		expire time.Time
		now    time.Time
		want   bool
	}{
		{"the plan resets on every renewal", subscribe.Subscribe{RenewalReset: &yes, ResetCycle: int64(period.CycleMonthly)}, at(1, 15), at(9, 15), at(3, 1), true},
		{"a term without end buys a new allowance", subscribe.Subscribe{}, at(1, 15), usersub.NoLimitExpiry(), at(3, 1), true},
		{"running on the expiry's day of the month", subscribe.Subscribe{}, at(1, 15), at(9, 15), at(3, 15), false},
		{"running monthly plan on its reset day", subscribe.Subscribe{ResetCycle: int64(period.CycleMonthly)}, at(1, 15), at(9, 15), at(3, 15), false},
		{"lapsed plan without calendar reset", subscribe.Subscribe{}, at(1, 15), at(3, 10), at(3, 12), true},
		{"lapsed across its monthly reset day", subscribe.Subscribe{ResetCycle: int64(period.CycleMonthly)}, at(1, 15), at(3, 10), at(3, 16), true},
		{"lapsed on its monthly reset day", subscribe.Subscribe{ResetCycle: int64(period.CycleMonthly)}, at(1, 15), at(3, 15), at(3, 18), true},
		{"lapsed within one monthly cycle", subscribe.Subscribe{ResetCycle: int64(period.CycleMonthly)}, at(1, 15), at(3, 16), at(3, 20), false},
		{"lapsed across the 1st", subscribe.Subscribe{ResetCycle: int64(period.CycleFirstOfMonth)}, at(1, 15), at(3, 28), at(4, 2), true},
		{"lapsed within a month", subscribe.Subscribe{ResetCycle: int64(period.CycleFirstOfMonth)}, at(1, 15), at(3, 2), at(3, 28), false},
		{"lapsed yearly plan before its anniversary", subscribe.Subscribe{ResetCycle: int64(period.CycleYearly)}, at(1, 15), at(3, 2), at(3, 28), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := tt.plan
			sub := &usersub.Subscribe{StartTime: tt.start, ExpireTime: tt.expire}
			if got := renewalResetsTraffic(cal, &plan, sub, tt.now); got != tt.want {
				t.Fatalf("renewalResetsTraffic = %v, want %v", got, tt.want)
			}
		})
	}
}

// A renewal writes only what it owns: the owner's note and the traffic the
// running subscription used stay as stored.
func TestRenewalExtendsTheTermWithoutRewritingTheRow(t *testing.T) {
	f := newPeriodFixture(t)
	ctx := context.Background()
	now := time.Now()
	expire := now.Add(10 * 24 * time.Hour).Truncate(time.Millisecond)
	sub := &usersub.Subscribe{
		UserId: 7, SubscribeId: 1, StartTime: now.Add(-20 * 24 * time.Hour), ExpireTime: expire,
		Traffic: 100, Upload: 30, Download: 20, Token: "renew-token", UUID: "renew-uuid",
		Status: usersub.SubscribeStatusActive, Note: "phone",
	}
	if err := f.store.db.Create(sub).Error; err != nil {
		t.Fatal(err)
	}
	f.orders.rows[5] = &order.Order{Id: 5, OrderNo: "renew-5", UserId: 7, SubscribeId: 1, SubscribeToken: "renew-token", Type: order.TypeRenewal, Status: order.StatusPaid, Quantity: 1}
	if _, err := f.service.FulfillPaidOrder(ctx, "renew-5"); err != nil {
		t.Fatal(err)
	}
	got := f.sub(t, sub.Id)
	if got.Upload != 30 || got.Download != 20 || got.Note != "phone" || got.Status != usersub.SubscribeStatusActive {
		t.Fatalf("renewal rewrote unrelated columns: %+v", got)
	}
	want, err := period.App().TermEnd(period.UnitMonth, 1, expire)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExpireTime.Equal(want) {
		t.Fatalf("expire = %v, want a month after %v", got.ExpireTime, expire)
	}
}

// A plan with an unknown time unit fails the fulfillment instead of granting
// a subscription that expires on arrival.
func TestFulfillmentRejectsAnUnknownTimeUnit(t *testing.T) {
	f := newPeriodFixture(t)
	f.store.plans.unitTime = "Fortnight"
	f.orders.rows[1] = &order.Order{Id: 1, OrderNo: "unknown-unit", UserId: 7, SubscribeId: 1, Type: order.TypeSubscribe, Status: order.StatusPaid, Quantity: 1}
	if _, err := f.service.FulfillPaidOrder(context.Background(), "unknown-unit"); !errors.Is(err, period.ErrUnknownUnit) {
		t.Fatalf("FulfillPaidOrder = %v, want ErrUnknownUnit", err)
	}
	var count int64
	if err := f.store.db.Model(&usersub.Subscribe{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("subscriptions created: %d, %v", count, err)
	}
}

// A paid traffic reset follows the rule of every traffic reset: an exhausted
// subscription inside its term is active again, an expired one stays down.
func TestPaidTrafficResetReactivatesOnlyInsideTheTerm(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     uint8
		expire     time.Duration
		wantStatus uint8
	}{
		{"exhausted in term", usersub.SubscribeStatusFinished, 24 * time.Hour, usersub.SubscribeStatusActive},
		{"expired meanwhile", usersub.SubscribeStatusExpired, -time.Hour, usersub.SubscribeStatusExpired},
		{"active", usersub.SubscribeStatusActive, 24 * time.Hour, usersub.SubscribeStatusActive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPeriodFixture(t)
			now := time.Now()
			finished := now.Add(-time.Minute)
			sub := &usersub.Subscribe{
				UserId: 7, SubscribeId: 1, StartTime: now.Add(-24 * time.Hour), ExpireTime: now.Add(tc.expire), FinishedAt: &finished,
				Traffic: 100, Upload: 60, Download: 40, Token: "reset-token", UUID: "reset-uuid", Status: tc.status, Note: "kept",
			}
			if err := f.store.db.Create(sub).Error; err != nil {
				t.Fatal(err)
			}
			f.orders.rows[3] = &order.Order{Id: 3, OrderNo: "reset-3", UserId: 7, SubscribeId: 1, SubscribeToken: "reset-token", Type: order.TypeResetTraffic, Status: order.StatusPaid}
			if _, err := f.service.FulfillPaidOrder(context.Background(), "reset-3"); err != nil {
				t.Fatal(err)
			}
			got := f.sub(t, sub.Id)
			if got.Upload != 0 || got.Download != 0 || got.Status != tc.wantStatus || got.Note != "kept" {
				t.Fatalf("after the paid reset: %+v, want status %d", got, tc.wantStatus)
			}
		})
	}
}
