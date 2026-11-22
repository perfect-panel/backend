// Package quotatask processes the admin-scheduled quota grants: extending
// subscription time and crediting gift money for a scope of subscriptions.
// The old single cross-domain transaction is staged into one subscription
// transaction and one billing transaction (the billing module's) per
// subscription — each idempotent via a per-(task, subscription) inbox marker
// — followed by a platform transaction for the task bookkeeping, so a retry
// after a mid-scope failure resumes where it stopped without double-granting.
// Only the module facade may reach it.
package quotatask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/entity/task"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/slicesx"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// Inbox consumer name of the grant stage; the gift stage's marker is the
// billing module's. It is a persisted identity: renaming it makes committed
// grants replay.
const inboxQuotaGrant = "subscription.quota_grant"

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	// Accounts refreshes the owners' cached accounts after a gift.
	Accounts Accounts
	// Store carries the staged domain transactions (subscription grant,
	// platform task bookkeeping) and the post-commit cache invalidation.
	Store Store
	// Gifts credits the gift money in the billing module's own transaction.
	Gifts GiftLedger
}

// Accounts is the identity port refreshing the cached balances of the gift
// recipients' accounts.
type Accounts interface {
	FindUsersByIds(ctx context.Context, ids []int64) ([]*user.User, error)
	ClearUserCache(ctx context.Context, userIDs ...int64) error
}

// accountIDs returns the ids of the loaded accounts.
func accountIDs(users []*user.User) []int64 {
	ids := make([]int64, 0, len(users))
	for _, u := range users {
		if u != nil {
			ids = append(ids, u.Id)
		}
	}
	return ids
}

// ErrorInfo is one subscription's soft failure in the task's error report.
type ErrorInfo struct {
	UserSubscribeId int64  `json:"user_subscribe_id"`
	Error           string `json:"error"`
}

// ErrUnretryable marks failures a retry cannot fix (missing or malformed
// task data); the queue shell maps it to its skip-retry sentinel.
var ErrUnretryable = errors.New("quota task is not retryable")

// errQuotaIneligible marks a subscription the grant skips; the run reports
// it as a soft failure instead of stopping.
var errQuotaIneligible = errors.New("deducted subscription is not eligible for quota grants")

// Service is the quota-task entry point used by the subscription facade.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// ProcessQuotaTask runs the quota task over its scope of subscriptions and
// records the progress on the task. Every grant stage is idempotent, so a
// retried run skips what an earlier run granted; a task already processed is
// left alone.
func (s *Service) ProcessQuotaTask(ctx context.Context, taskID int64) error {
	taskInfo, err := s.getTaskInfo(ctx, taskID)
	if err != nil {
		return err
	}
	ctx = restoreTaskRequestMetadata(ctx, taskInfo.Scope)

	if taskInfo.Status == task.StatusCompleted || taskInfo.Status == task.StatusCancelled || taskInfo.Status == task.StatusEnqueueFailed ||
		(taskInfo.Status == task.StatusFailed && taskInfo.Current >= taskInfo.Total) {
		logger.WithContext(ctx).Info("[QuotaTaskLogic.ProcessTask] task already processed",
			logger.Field("taskID", taskID),
			logger.Field("status", taskInfo.Status),
		)
		return nil
	}

	scope, content, err := s.parseTaskData(ctx, taskInfo)
	if err != nil {
		return s.failTask(ctx, taskInfo, err)
	}
	if err := validateContent(content); err != nil {
		return s.failTask(ctx, taskInfo, err)
	}
	if len(scope.Objects) == 0 {
		return s.failTask(ctx, taskInfo, fmt.Errorf("quota task has no targets"))
	}

	subscribes, err := s.getSubscribes(ctx, scope.Objects)
	if err != nil {
		return err
	}
	if len(subscribes) != len(scope.Objects) {
		return s.failTask(ctx, taskInfo, fmt.Errorf("quota task target set changed: expected %d subscriptions, found %d", len(scope.Objects), len(subscribes)))
	}
	taskInfo.Status = task.StatusInProgress
	if err := s.updateTask(ctx, taskInfo); err != nil {
		return err
	}
	if err = s.processSubscribes(ctx, subscribes, content, taskInfo); err != nil {
		return err
	}
	// A gift changed the owners' balances: drop their cached accounts.
	if content.GiftValue != 0 {
		var userIds []int64
		for _, sub := range subscribes {
			userIds = append(userIds, sub.UserId)
		}
		userIds = slicesx.RemoveDuplicateElements(userIds...)
		users, err := s.deps.Accounts.FindUsersByIds(ctx, userIds)
		if err != nil {
			logger.WithContext(ctx).Error("[QuotaTaskLogic.ProcessTask] find users error",
				logger.Field("error", err.Error()),
				logger.Field("user_count", len(userIds)))
		}
		err = s.deps.Accounts.ClearUserCache(ctx, accountIDs(users)...)
		if err != nil {
			logger.WithContext(ctx).Error("[QuotaTaskLogic.ProcessTask] clear user cache error",
				logger.Field("error", err.Error()),
				logger.Field("user_count", len(userIds)))
		}
	}

	err = s.deps.Store.UserSubscription().ClearSubscribeCache(ctx, subscribes...)
	if err != nil {
		logger.WithContext(ctx).Error("[QuotaTaskLogic.ProcessTask] clear subscribe cache error",
			logger.Field("error", err.Error()))
	}

	return nil
}

func restoreTaskRequestMetadata(ctx context.Context, scopeJSON string) context.Context {
	var metadata requestmeta.Metadata
	if json.Unmarshal([]byte(scopeJSON), &metadata) != nil {
		return ctx
	}
	ctx = requestmeta.With(ctx, metadata)
	return logger.ContextWithRequestMetadata(ctx, metadata)
}

func (s *Service) getTaskInfo(ctx context.Context, taskID int64) (*task.Task, error) {
	taskInfo, err := s.deps.Store.Task().FindOneByType(ctx, taskID, task.TypeQuota)
	if err != nil {
		logger.WithContext(ctx).Error("[QuotaTaskLogic.getTaskInfo] find task error",
			logger.Field("error", err.Error()),
			logger.Field("taskID", taskID),
		)
		return nil, err
	}
	return taskInfo, nil
}

func (s *Service) parseTaskData(ctx context.Context, taskInfo *task.Task) (task.QuotaScope, task.QuotaContent, error) {
	var scope task.QuotaScope
	if err := scope.Unmarshal([]byte(taskInfo.Scope)); err != nil {
		logger.WithContext(ctx).Error("[QuotaTaskLogic.parseTaskData] unmarshal scope error",
			logger.Field("error", err.Error()),
		)
		return scope, task.QuotaContent{}, fmt.Errorf("%w: parse quota scope: %w", ErrUnretryable, err)
	}

	var content task.QuotaContent
	if err := content.Unmarshal([]byte(taskInfo.Content)); err != nil {
		logger.WithContext(ctx).Error("[QuotaTaskLogic.parseTaskData] unmarshal content error",
			logger.Field("error", err.Error()),
		)
		return scope, content, fmt.Errorf("%w: parse quota content: %w", ErrUnretryable, err)
	}
	return scope, content, nil
}

func (s *Service) getSubscribes(ctx context.Context, subscriberIDs []int64) ([]*usersub.Subscribe, error) {
	subscribes, err := s.deps.Store.UserSubscription().FindSubscribesByIds(ctx, subscriberIDs)
	if err != nil {
		logger.WithContext(ctx).Error("[QuotaTaskLogic.getSubscribes] find subscribes error",
			logger.Field("error", err.Error()),
			logger.Field("subscriber_count", len(subscriberIDs)),
		)
		return nil, err
	}
	return subscribes, nil
}

// inboxKey identifies one subscription's grant within one task run for the
// idempotency markers.
func inboxKey(taskID, subscribeID int64) string {
	return fmt.Sprintf("%d:%d", taskID, subscribeID)
}

// processSubscribes stages the grant per subscription: a subscription
// transaction (time extension, traffic reset) then a billing transaction
// (gift credit), each skipping via its inbox marker on retry. A hard failure
// stops the run with the task still pending, so the queue retry resumes at
// the first unprocessed subscription. Soft per-subscription failures are
// accumulated into the task's error report, matching the old behavior.
func (s *Service) processSubscribes(ctx context.Context, subscribes []*usersub.Subscribe, content task.QuotaContent, taskInfo *task.Task) error {
	var errs []ErrorInfo
	now := timeutil.Now()

	for index, sub := range subscribes {
		if sub == nil {
			errs = append(errs, ErrorInfo{
				UserSubscribeId: 0,
				Error:           "subscription is nil",
			})
			continue
		}
		err := errQuotaIneligible
		if sub.Status != usersub.SubscribeStatusDeducted {
			err = s.grantSubscription(ctx, taskInfo.Id, sub, content, now)
		}
		switch {
		case errors.Is(err, errQuotaIneligible):
			// Deducted when the task read its targets, or by the time the
			// grant locked the row: neither stage applies.
			errs = append(errs, ErrorInfo{UserSubscribeId: sub.Id, Error: err.Error()})
		case err != nil:
			return err
		case content.GiftValue != 0:
			if err := s.grantGift(ctx, taskInfo.Id, sub, content, now); err != nil {
				return err
			}
		}
		if err := s.advanceTaskProgress(ctx, taskInfo, uint64(index+1)); err != nil {
			return err
		}
	}

	return s.finishTask(ctx, taskInfo, len(subscribes), errs)
}

func (s *Service) advanceTaskProgress(ctx context.Context, taskInfo *task.Task, current uint64) error {
	if current <= taskInfo.Current {
		return nil
	}
	taskInfo.Current = current
	return s.updateTask(ctx, taskInfo)
}

// grantSubscription applies the time extension and traffic reset in a
// subscription-domain transaction, exactly once per (task, subscription).
// The task read its targets when it started; traffic accounting, credential
// resets and status changes may have landed since, so the row is re-read
// under lock and only the columns the grant changes are written back. On
// success sub is refreshed to the stored row.
func (s *Service) grantSubscription(ctx context.Context, taskID int64, sub *usersub.Subscribe, content task.QuotaContent, now time.Time) error {
	key := inboxKey(taskID, sub.Id)
	var granted *usersub.Subscribe
	err := s.deps.Store.InSubscriptionTx(ctx, func(store repository.SubscriptionStore) error {
		mark, err := store.Inbox().Find(ctx, inboxQuotaGrant, key)
		if err != nil {
			return err
		}
		if mark != nil {
			return nil
		}

		current, err := store.UserSubscription().FindOneSubscribeForUpdate(ctx, sub.Id)
		if err != nil {
			return fmt.Errorf("lock subscription %d: %w", sub.Id, err)
		}
		if current.EntitlementSource != "" {
			return store.Inbox().Insert(ctx, inboxQuotaGrant, key, "skipped: provider-managed subscription")
		}
		if current.Status == usersub.SubscribeStatusDeducted {
			return errQuotaIneligible
		}

		columns := applyQuotaGrant(current, content, now)
		if content.ResetTraffic {
			if err := s.createResetTrafficLog(ctx, store.Log(), current.Id, current.UserId, now); err != nil {
				return fmt.Errorf("create reset traffic log for subscription %d: %w", current.Id, err)
			}
		}
		if err := store.UserSubscription().UpdateSubscribeColumns(ctx, current, columns...); err != nil {
			return fmt.Errorf("update subscription %d: %w", current.Id, err)
		}
		granted = current

		// The marker commits with the mutation (or records a reported soft
		// failure), so a retried run never re-applies this stage.
		return store.Inbox().Insert(ctx, inboxQuotaGrant, key, "")
	})
	if err == nil && granted != nil {
		*sub = *granted
	}
	return err
}

// applyQuotaGrant applies the grant to the locked row and names the columns
// it changed. An admin hold (Stopped) or a refund (Deducted) is never lifted:
// the term is still extended and usage still reset, but the status stays.
func applyQuotaGrant(sub *usersub.Subscribe, content task.QuotaContent, now time.Time) []string {
	var columns []string
	activate := func() {
		switch sub.Status {
		case usersub.SubscribeStatusActive, usersub.SubscribeStatusStopped, usersub.SubscribeStatusDeducted:
			return
		}
		sub.Status = usersub.SubscribeStatusActive
		sub.FinishedAt = nil
		columns = append(columns, "status", "finished_at")
	}

	if content.Days != 0 {
		switch {
		case usersub.NoExpiry(sub.ExpireTime):
			// Adding finite days must never downgrade an unlimited
			// subscription to a finite term.
			activate()
		case sub.ExpireTime.Before(now):
			// Already expired: the extension starts now.
			sub.ExpireTime = now.AddDate(0, 0, int(content.Days))
			columns = append(columns, "expire_time")
		default:
			sub.ExpireTime = sub.ExpireTime.AddDate(0, 0, int(content.Days))
			columns = append(columns, "expire_time")
		}
		// A term extended into the future reactivates the subscription.
		if !sub.ExpiredAt(now) {
			activate()
		}
	}

	if content.ResetTraffic {
		// The rule every traffic reset follows: an exhausted subscription
		// inside its term is active again.
		columns = append(columns, sub.ResetTraffic(now)...)
	}
	return columns
}

// GiftLedger is the billing port of the gift stage; the billing facade
// provides it.
type GiftLedger interface {
	// QuotaGiftCredited reports whether the task's gift for the subscription
	// was credited.
	QuotaGiftCredited(ctx context.Context, taskID, subscriptionID int64) (bool, error)
	// CreditQuotaGift credits amount to the gift balance of the
	// subscription's owner with a gift log dated at, in a billing-domain
	// transaction, exactly once per (task, subscription); a zero amount only
	// records that the stage ran.
	CreditQuotaGift(ctx context.Context, taskID, subscriptionID, userID, amount int64, at time.Time) error
}

// grantGift has the billing module credit the gift money in a
// billing-domain transaction, exactly once per (task, subscription). The plan
// lookup for the percentage gift is this module's read, done before the
// billing transaction — reference data, not billing state.
func (s *Service) grantGift(ctx context.Context, taskID int64, sub *usersub.Subscribe, content task.QuotaContent, now time.Time) error {
	credited, err := s.deps.Gifts.QuotaGiftCredited(ctx, taskID, sub.Id)
	if err != nil {
		return err
	}
	if credited {
		s.clearGiftUserCache(ctx, sub.UserId)
		return nil
	}

	var giftAmount int64
	switch content.GiftType {
	case 1:
		giftAmount = int64(content.GiftValue)
	case 2:
		// The gift is a percentage of the plan's unit price.
		subscribeInfo, err := s.deps.Store.Subscribe().FindOne(ctx, sub.SubscribeId)
		if err != nil {
			return fmt.Errorf("find plan for subscription %d: %w", sub.Id, err)
		}
		if subscribeInfo.UnitPrice > 0 {
			amount := new(big.Int).Mul(big.NewInt(subscribeInfo.UnitPrice), new(big.Int).SetUint64(content.GiftValue))
			amount.Div(amount, big.NewInt(100))
			if !amount.IsInt64() {
				return fmt.Errorf("calculated gift amount overflows int64 for subscription %d", sub.Id)
			}
			giftAmount = amount.Int64()
		}
	}

	if err := s.deps.Gifts.CreditQuotaGift(ctx, taskID, sub.Id, sub.UserId, giftAmount, now); err != nil {
		return err
	}
	if giftAmount > 0 {
		// The gift transaction may commit before a later subscription fails.
		// Clear this user's balance projection immediately so a partial task
		// never leaves committed money hidden behind stale cache state.
		s.clearGiftUserCache(ctx, sub.UserId)
	}
	return nil
}

func (s *Service) clearGiftUserCache(ctx context.Context, userID int64) {
	if err := s.deps.Accounts.ClearUserCache(ctx, userID); err != nil {
		logger.WithContext(ctx).Error("[QuotaTaskLogic.grantGift] clear user cache error",
			logger.Field("error", err.Error()))
	}
}

// finishTask records the outcome on the task row in a platform-domain
// transaction. The task stays in progress if an earlier stage hard-failed,
// so the queue retry resumes through the inbox markers.
func (s *Service) finishTask(ctx context.Context, taskInfo *task.Task, total int, errs []ErrorInfo) error {
	status := task.StatusCompleted
	taskInfo.Errors = ""
	if len(errs) > 0 {
		logger.WithContext(ctx).Error("[QuotaTaskLogic.processSubscribes] some subscriptions failed",
			logger.Field("total", total),
			logger.Field("failed", len(errs)),
		)
		// The task failed when every subscription did.
		failedSubscriptions := make(map[int64]struct{}, len(errs))
		for _, item := range errs {
			failedSubscriptions[item.UserSubscribeId] = struct{}{}
		}
		if len(failedSubscriptions) >= total {
			status = task.StatusFailed
		}
		marshaled, err := json.Marshal(errs)
		if err != nil {
			logger.WithContext(ctx).Error("[QuotaTaskLogic.processSubscribes] marshal errors failed",
				logger.Field("error", err.Error()),
			)
			return err
		}
		taskInfo.Errors = string(marshaled)
	}

	taskInfo.Current = uint64(total)
	taskInfo.Status = status
	return s.updateTask(ctx, taskInfo)
}

func (s *Service) updateTask(ctx context.Context, taskInfo *task.Task) error {
	return s.deps.Store.InPlatformTx(ctx, func(store repository.PlatformStore) error {
		updated, err := store.Task().UpdateActiveProgress(ctx, taskInfo)
		if err != nil {
			logger.WithContext(ctx).Error("[QuotaTaskLogic.processSubscribes] update task status error",
				logger.Field("error", err.Error()),
				logger.Field("taskID", taskInfo.Id),
			)
			return err
		}
		if !updated {
			return fmt.Errorf("quota task %d is no longer active", taskInfo.Id)
		}
		return nil
	})
}

func (s *Service) failTask(ctx context.Context, taskInfo *task.Task, cause error) error {
	taskInfo.Status = task.StatusFailed
	taskInfo.Errors = cause.Error()
	return s.updateTask(ctx, taskInfo)
}

func validateContent(content task.QuotaContent) error {
	if !content.ResetTraffic && content.Days == 0 && content.GiftValue == 0 {
		return fmt.Errorf("quota task has no action")
	}
	if content.Days > uint64(^uint(0)>>1) {
		return fmt.Errorf("quota task days overflow")
	}
	if content.GiftValue == 0 {
		if content.GiftType != 0 {
			return fmt.Errorf("quota task gift type has no value")
		}
		return nil
	}
	if content.GiftType != 1 && content.GiftType != 2 {
		return fmt.Errorf("invalid quota task gift type: %d", content.GiftType)
	}
	if content.GiftValue > math.MaxInt64 {
		return fmt.Errorf("quota task gift value overflow")
	}
	return nil
}

func (s *Service) createResetTrafficLog(ctx context.Context, logs repository.LogRepo, subscribeId, userId int64, now time.Time) error {
	trafficLog := &log.ResetSubscribe{
		Type:      log.ResetSubscribeTypeQuota,
		UserId:    userId,
		OrderNo:   "",
		Timestamp: now.UnixMilli(),
	}

	logString, err := trafficLog.Marshal()
	if err != nil {
		return fmt.Errorf("marshal traffic log error: %w", err)
	}
	return logs.Insert(ctx, &log.SystemLog{
		Type:     log.TypeResetSubscribe.Uint8(),
		Content:  string(logString),
		ObjectID: subscribeId,
		Date:     now.Format(time.DateOnly),
	})
}

// Store is the persistence capability required by this package. It excludes
// unrelated repositories and application-wide transactions.
type Store interface {
	repository.PlatformTransactor
	repository.SubscriptionTransactor
	Inbox() repository.InboxRepo
	Subscribe() repository.SubscribeRepo
	Task() repository.TaskRepo
	UserSubscription() repository.UserSubscriptionRepo
}
