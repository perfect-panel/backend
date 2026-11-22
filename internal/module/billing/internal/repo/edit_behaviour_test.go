package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/coupon"
	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
)

// An administrator's coupon edit writes the editable columns only: the
// creation time is not rewritten from the edited row, and the use count is
// increased in the database rather than from a possibly stale row.
func TestCouponRepoUpdatesAreColumnScoped(t *testing.T) {
	h := billingtest.New(t)
	ctx := context.Background()
	repo := h.Store.Coupon()
	created := h.Coupon("SAVE", func(c *coupon.Coupon) { c.Count = 5; c.UsedCount = 1 })
	createdAt := h.ReloadCoupon("SAVE").CreatedAt

	enabled := false
	edited := &coupon.Coupon{
		Id: created.Id, Name: "renamed", Code: "SAVE", Count: 9, Type: coupon.TypeFixed, Discount: 250,
		StartTime: created.StartTime, ExpireTime: created.ExpireTime, UserLimit: 2, UsedCount: 1, Enable: &enabled,
		CreatedAt: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := repo.Update(ctx, edited); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got := h.ReloadCoupon("SAVE")
	if got.Name != "renamed" || got.Count != 9 || got.Discount != 250 || got.UserLimit != 2 || got.IsEnabled() {
		t.Fatalf("coupon = %+v, want the edit applied", got)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("created_at = %v, want %v kept", got.CreatedAt, createdAt)
	}
	if err := repo.Update(ctx, &coupon.Coupon{Id: created.Id, Code: "SAVE"}); err == nil {
		t.Fatal("an edit without the enable flag was applied")
	}

	for range 2 {
		if err := repo.UpdateCount(ctx, "SAVE"); err != nil {
			t.Fatalf("UpdateCount: %v", err)
		}
	}
	if used := h.ReloadCoupon("SAVE").UsedCount; used != 3 {
		t.Fatalf("used count = %d, want 3", used)
	}
}

// An administrator's payment method edit cannot rewrite the platform, fixed
// at creation, or the notify token that routes callbacks to the method.
func TestPaymentRepoUpdateKeepsPlatformAndToken(t *testing.T) {
	h := billingtest.New(t)
	ctx := context.Background()
	method := h.Payment("EPay", `{"pid":"1001","url":"https://pay.example","key":"secret","type":"alipay"}`)

	enabled := false
	edited := *method
	edited.Name, edited.Platform, edited.Token, edited.Enable, edited.Sort = "renamed", "Stripe", "forged-token", &enabled, 7
	if err := h.Store.Payment().Update(ctx, &edited); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := h.Store.Payment().FindOne(ctx, method.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "renamed" || got.Sort != 7 || got.Enable == nil || *got.Enable {
		t.Fatalf("method = %+v, want the edit applied", got)
	}
	if got.Platform != "EPay" || got.Token != method.Token {
		t.Fatalf("method = %+v, want the platform and token kept", got)
	}
	if err := h.Store.Payment().Update(ctx, &payment.Payment{Id: method.Id, Name: "x"}); err == nil {
		t.Fatal("an edit without the enable flag was applied")
	}
}
