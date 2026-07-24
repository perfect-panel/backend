package svc

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/module/billing"
	"github.com/perfect-panel/server/internal/module/support"
	"github.com/perfect-panel/server/internal/report"
	"github.com/perfect-panel/server/internal/repository"
	emailworker "github.com/perfect-panel/server/internal/worker/email"
	"github.com/perfect-panel/server/pkg/logger"
	queuetypes "github.com/perfect-panel/server/queue/types"
)

// newBillingModule wires the billing module against the legacy store and the
// asynq client (ADR-001 step 4).
func newBillingModule(c config.Config, store repository.Store, queue *asynq.Client) billing.Service {
	return billing.New(billing.Deps{
		Orders:        store.Order(),
		Payments:      store.Payment(),
		Coupons:       store.Coupon(),
		Plans:         store.Subscribe(),
		UserSubs:      store.UserSubscription(),
		Store:         store,
		Tx:            store,
		Queue:         activationQueue{client: queue},
		SingleModel:   c.Subscribe.SingleModel,
		CurrencyUnit:  c.Currency.Unit,
		Host:          c.Host,
		IsGatewayMode: report.IsGatewayMode,
	})
}

// activationQueue adapts the asynq client to the billing module's activation
// port. A task-id conflict means a delivery already exists for the order,
// which is success, not an error.
type activationQueue struct {
	client *asynq.Client
}

func (q activationQueue) EnqueueActivation(ctx context.Context, orderNo string) error {
	payload, err := json.Marshal(queuetypes.ForthwithActivateOrderPayload{OrderNo: orderNo})
	if err != nil {
		return err
	}
	task := asynq.NewTask(queuetypes.ForthwithActivateOrder, payload)
	_, err = q.client.EnqueueContext(ctx, task, asynq.TaskID(queuetypes.ActivationTaskID(orderNo)))
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}

// EnqueueDeferredClose schedules the pending order's expiry close after the
// payment window elapses.
func (q activationQueue) EnqueueDeferredClose(ctx context.Context, orderNo string) error {
	payload, err := json.Marshal(queuetypes.DeferCloseOrderPayload{OrderNo: orderNo})
	if err != nil {
		return err
	}
	task := asynq.NewTask(queuetypes.DeferCloseOrder, payload, asynq.MaxRetry(3))
	_, err = q.client.EnqueueContext(ctx, task, asynq.ProcessIn(billing.CloseOrderTimeMinutes*time.Minute))
	return err
}

// newSupportModule wires the support module against the legacy store. The
// adapters below satisfy the module's ports until the owning modules exist
// (ADR-001).
func newSupportModule(store repository.Store, queue *asynq.Client) support.Service {
	return support.New(support.Deps{
		Announcements: store.Announcement(),
		Ads:           store.Ads(),
		Documents:     store.Document(),
		Tickets:       store.Ticket(),
		Tasks:         store.Task(),
		Subscriptions: subscriptionReader{store: store},
		Recipients:    store.User(),
		QuotaTargets:  store.UserSubscription(),
		Queue:         marketingQueue{client: queue},
		EmailStopper:  emailWorkerStopper{},
	})
}

// marketingQueue adapts the asynq client to the support module's
// MarketingQueue port, keeping queue task types out of the module.
type marketingQueue struct {
	client *asynq.Client
}

func (q marketingQueue) EnqueueBatchEmail(ctx context.Context, taskID int64, processAt time.Time) (string, error) {
	t := asynq.NewTask(queuetypes.ScheduledBatchSendEmail, []byte(strconv.FormatInt(taskID, 10)))
	info, err := q.client.EnqueueContext(ctx, t, asynq.ProcessAt(processAt))
	if err != nil {
		return "", err
	}
	return info.ID, nil
}

func (q marketingQueue) EnqueueQuota(ctx context.Context, taskID int64) error {
	t := asynq.NewTask(queuetypes.ForthwithQuotaTask, []byte(strconv.FormatInt(taskID, 10)))
	_, err := q.client.EnqueueContext(ctx, t)
	return err
}

// emailWorkerStopper adapts the global batch-email worker manager to the
// support module's BatchEmailStopper port.
type emailWorkerStopper struct{}

func (emailWorkerStopper) StopBatchEmail(taskID int64) {
	if emailworker.Manager == nil {
		logger.Error("[StopBatchSendEmailTaskLogic] email worker manager is nil, cannot stop task")
		return
	}
	emailworker.Manager.RemoveWorker(taskID)
}

// subscriptionReader adapts the legacy user-subscription repository to the
// support module's SubscriptionReader port.
type subscriptionReader struct {
	store repository.Store
}

func (r subscriptionReader) HasActiveSubscription(ctx context.Context, userID int64) (bool, error) {
	// status 1 = active
	subs, err := r.store.UserSubscription().QueryUserSubscribe(ctx, userID, 1)
	if err != nil {
		return false, err
	}
	return len(subs) > 0, nil
}
