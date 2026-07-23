package types

import (
	"crypto/sha256"
	"encoding/hex"
)

const (
	DeferCloseOrder                 = "defer:order:close"
	ForthwithActivateOrder          = "forthwith:order:activate"
	SchedulerReconcilePaidOrders    = "scheduler:order:reconcile-paid"
	SchedulerReconcilePendingOrders = "scheduler:order:reconcile-pending"
	SchedulerPublishOrderEvents     = "scheduler:order:publish-events"
	SchedulerCleanupOrderEvents     = "scheduler:order:cleanup-events"
)

type (
	DeferCloseOrderPayload struct {
		OrderNo string `json:"order_no"`
	}
	ForthwithActivateOrderPayload struct {
		OrderNo string `json:"order_no"`
	}
)

func ActivationTaskID(orderNo string) string {
	digest := sha256.Sum256([]byte(orderNo))
	return "order-activation:" + hex.EncodeToString(digest[:])
}
