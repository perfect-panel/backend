// Package kernel holds the persistence contracts of the shared kernel: the
// platform-owned repositories every module may use (system settings, the
// audit log, task bookkeeping and the event inbox and outbox), the platform
// transaction view and the pagination helpers.
//
// It imports nothing but platform entities, so the platform module can
// depend on its own contracts without depending on any other module
// (ADR-001: the shared kernel depends on no module). internal/repository
// re-exports every declaration for the other consumers.
package kernel

import (
	"context"
	"time"

	"github.com/perfect-panel/backend/internal/module/platform/entity/inbox"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/entity/outbox"
	"github.com/perfect-panel/backend/internal/module/platform/entity/system"
	"github.com/perfect-panel/backend/internal/module/platform/entity/task"
)

// SystemRepo manages the system table: the settings administrators edit,
// grouped by category.
type SystemRepo interface {
	Insert(ctx context.Context, data *system.System) error
	FindOne(ctx context.Context, id int64) (*system.System, error)
	FindOneByKey(ctx context.Context, email string) (*system.System, error)
	Update(ctx context.Context, data *system.System) error
	Delete(ctx context.Context, id int64) error
	GetSmsConfig(ctx context.Context) ([]*system.System, error)
	GetSiteConfig(ctx context.Context) ([]*system.System, error)
	GetEmailConfig(ctx context.Context) ([]*system.System, error)
	GetSubscribeConfig(ctx context.Context) ([]*system.System, error)
	GetRegisterConfig(ctx context.Context) ([]*system.System, error)
	GetVerifyConfig(ctx context.Context) ([]*system.System, error)
	GetNodeConfig(ctx context.Context) ([]*system.System, error)
	GetInviteConfig(ctx context.Context) ([]*system.System, error)
	GetTelegramConfig(ctx context.Context) ([]*system.System, error)
	GetTosConfig(ctx context.Context) ([]*system.System, error)
	GetCurrencyConfig(ctx context.Context) ([]*system.System, error)
	GetVerifyCodeConfig(ctx context.Context) ([]*system.System, error)
	GetLogConfig(ctx context.Context) ([]*system.System, error)
	UpdateValueByCategoryKey(ctx context.Context, category, key, value string, valueType ...string) error
	UpdateNodeMultiplierConfig(ctx context.Context, config string) error
	FindNodeMultiplierConfig(ctx context.Context) (*system.System, error)
}

// LogRepo manages the system_logs table: the audit and message logs
// (sign-ins, balance changes, messages sent, traffic statistics and more).
type LogRepo interface {
	Insert(ctx context.Context, data *log.SystemLog) error
	InsertBatch(ctx context.Context, data []*log.SystemLog, batchSize int) error
	FindOne(ctx context.Context, id int64) (*log.SystemLog, error)
	Update(ctx context.Context, data *log.SystemLog) error
	Delete(ctx context.Context, id int64) error
	FilterSystemLog(ctx context.Context, filter *log.FilterParams) ([]*log.SystemLog, int64, error)
	FindFirstByDateType(ctx context.Context, date string, typ uint8) (*log.SystemLog, error)
	FindByDatesType(ctx context.Context, dates []string, typ uint8) ([]*log.SystemLog, error)
	DeleteBefore(ctx context.Context, end time.Time) error
	DeleteBeforeBatch(ctx context.Context, end time.Time, limit int) (int64, error)
	SumAmountByTypeAndObjectID(ctx context.Context, typ uint8, objectID int64) (int64, error)
}

// TaskRepo manages the bookkeeping of the batch tasks administrators start
// (email campaigns, quota grants): progress, status and the errors of each.
type TaskRepo interface {
	Insert(ctx context.Context, data *task.Task) error
	FindOne(ctx context.Context, id int64) (*task.Task, error)
	FindOneByType(ctx context.Context, id int64, typ task.Type) (*task.Task, error)
	QueryTaskList(ctx context.Context, filter *task.Filter) (int64, []*task.Task, error)
	Update(ctx context.Context, data *task.Task) error
	UpdateActive(ctx context.Context, data *task.Task) (bool, error)
	UpdateActiveProgress(ctx context.Context, data *task.Task) (bool, error)
	UpdateActiveProgressWithError(ctx context.Context, data *task.Task, taskError *task.TaskError) (bool, error)
	UpdateStatus(ctx context.Context, id int64, status int8) error
	UpdateStatusFrom(ctx context.Context, id int64, typ task.Type, from []int8, status int8) (bool, error)
	UpdateStatusAndErrorFrom(ctx context.Context, id int64, typ task.Type, from []int8, status int8, taskError string) (bool, error)
	InsertError(ctx context.Context, data *task.TaskError) error
	FindErrors(ctx context.Context, taskIDs []int64) ([]*task.TaskError, error)
}

// OutboxRepo is the generic domain-event outbox: Append runs inside the
// owning domain's transaction; the dispatcher drains unpublished events and
// marks them published once every subscriber has processed them.
type OutboxRepo interface {
	Append(ctx context.Context, topic, eventKey, payload string) error
	ListUnpublished(ctx context.Context, limit int) ([]*outbox.Event, error)
	MarkPublished(ctx context.Context, id int64) error
	DeletePublishedBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// InboxRepo is the idempotent-consumer inbox (ADR-001 step 2): a domain step
// records that it processed an event inside its own transaction, so
// at-least-once deliveries and reconciliation replays never apply the same
// mutation twice.
type TrafficReportReceipts interface {
	AdmitTrafficReport(context.Context, *inbox.TrafficReport) error
	PendingTrafficReports(context.Context, int64, time.Time, int) ([]inbox.TrafficReport, error)
	ClaimTrafficReportUsage(context.Context, string) (bool, error)
	FinishTrafficReport(context.Context, string) (bool, error)
}
type InboxRepo interface {
	TrafficReportReceipts
	// Find returns the processed marker, or (nil, nil) when the step has not
	// run yet.
	Find(ctx context.Context, consumer, eventKey string) (*inbox.Record, error)
	// Insert records the step as processed. It must run inside the same
	// transaction as the step's mutations; a duplicate-key error means a
	// concurrent delivery won the race and this transaction must roll back.
	Insert(ctx context.Context, consumer, eventKey, result string) error
	// DeleteProcessedBefore removes markers older than the replay contract;
	// every flow that consults the inbox resolves well inside the retention
	// window (deferred closes in minutes, bucket replays in hours).
	DeleteProcessedBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// PlatformStore is the platform domain's transactional surface: system
// settings, scheduled-task bookkeeping and audit/message logs.
type PlatformStore interface {
	System() SystemRepo
	Task() TaskRepo
	Log() LogRepo
	Inbox() InboxRepo
	Outbox() OutboxRepo
}

// PlatformTransactor runs a transaction scoped to the platform domain.
type PlatformTransactor interface {
	InPlatformTx(context.Context, func(PlatformStore) error) error
}

const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// NormalizePage clamps pagination inputs; module repo implementations share it.
func NormalizePage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = DefaultPageSize
	}
	if size > MaxPageSize {
		size = MaxPageSize
	}
	return page, size
}

// NormalizePageFloor clamps pagination inputs without a minimum page size.
func NormalizePageFloor(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = DefaultPageSize
	}
	return page, size
}
