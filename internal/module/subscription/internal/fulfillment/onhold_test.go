package fulfillment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
)

// A renewal or reset order can be created before the subscription is refunded
// or put on hold and paid afterwards; paying it must not bring the
// subscription back.
func TestFulfillPaidOrderKeepsRefundedAndStoppedSubscriptionsDown(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    uint8
		orderType uint8
	}{
		{"renewal of a refunded subscription", usersub.SubscribeStatusDeducted, order.TypeRenewal},
		{"reset of a refunded subscription", usersub.SubscribeStatusDeducted, order.TypeResetTraffic},
		{"renewal of a stopped subscription", usersub.SubscribeStatusStopped, order.TypeRenewal},
		{"reset of a stopped subscription", usersub.SubscribeStatusStopped, order.TypeResetTraffic},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPeriodFixture(t)
			now := time.Now()
			sub := &usersub.Subscribe{
				UserId: 7, OrderId: 1, SubscribeId: 1, StartTime: now.Add(-24 * time.Hour), ExpireTime: now.Add(24 * time.Hour),
				Traffic: 100, Download: 60, Upload: 40, Token: "held-token", UUID: "held-uuid", Status: tc.status,
			}
			if err := f.store.db.Create(sub).Error; err != nil {
				t.Fatal(err)
			}
			f.orders.rows[2] = &order.Order{
				Id: 2, OrderNo: "held-order", UserId: 7, SubscribeId: 1, Type: tc.orderType,
				Quantity: 1, SubscribeToken: "held-token", Status: 2,
			}

			_, err := f.service.FulfillPaidOrder(context.Background(), "held-order")

			if !errors.Is(err, usersub.ErrSubscriptionOnHold) {
				t.Fatalf("FulfillPaidOrder() error = %v, want ErrSubscriptionOnHold", err)
			}
			got := f.sub(t, sub.Id)
			if got.Status != tc.status || got.Download != 60 || !got.ExpireTime.Equal(sub.ExpireTime) {
				t.Fatalf("subscription changed to status=%d download=%d expire=%v", got.Status, got.Download, got.ExpireTime)
			}
		})
	}
}
