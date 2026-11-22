package repo_test

import (
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/internal/billingtest"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

func TestDateUserCountsUsesLocalCalendarDays(t *testing.T) {
	const zone = "America/Los_Angeles"
	previous := timeutil.LocationName()
	if err := timeutil.LoadLocation(zone); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = timeutil.LoadLocation(previous) })
	loc := timeutil.Location()
	for _, tc := range []struct {
		name  string
		month time.Month
		day   int
	}{{"spring", time.March, 8}, {"fall", time.November, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			h := billingtest.New(t)
			start := time.Date(2026, tc.month, tc.day, 0, 0, 0, 0, loc)
			end := start.AddDate(0, 0, 1)
			for i, stamp := range []time.Time{start.Add(12 * time.Hour), end.Add(-30 * time.Minute), end.Add(30 * time.Minute)} {
				h.Order(&order.Order{OrderNo: stamp.String(), UserId: int64(i + 1), Status: order.StatusFinished, Method: "epay", IsNew: i == 0, CreatedAt: stamp})
			}
			fresh, renew, err := h.Store.Order().QueryDateUserCounts(t.Context(), end.Add(-15*time.Minute))
			if err != nil || fresh != 1 || renew != 1 {
				t.Fatalf("new/renewal users = %d/%d, %v; want 1/1", fresh, renew, err)
			}
		})
	}
}
