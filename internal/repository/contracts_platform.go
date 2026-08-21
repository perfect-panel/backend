package repository

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/module/platform/entity/client"
	"github.com/perfect-panel/server/internal/module/platform/entity/inbox"
	"github.com/perfect-panel/server/internal/module/platform/entity/log"
	"github.com/perfect-panel/server/internal/module/platform/entity/outbox"
	"github.com/perfect-panel/server/internal/module/platform/entity/system"
	"github.com/perfect-panel/server/internal/module/platform/entity/task"
)

// SystemRepo system 数据访问接口
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

// LogRepo log 数据访问接口
type LogRepo interface {
	Insert(ctx context.Context, data *log.SystemLog) error
	FindOne(ctx context.Context, id int64) (*log.SystemLog, error)
	Update(ctx context.Context, data *log.SystemLog) error
	Delete(ctx context.Context, id int64) error
	FilterSystemLog(ctx context.Context, filter *log.FilterParams) ([]*log.SystemLog, int64, error)
	FindFirstByDateType(ctx context.Context, date string, typ uint8) (*log.SystemLog, error)
	FindByDatesType(ctx context.Context, dates []string, typ uint8) ([]*log.SystemLog, error)
	DeleteBefore(ctx context.Context, end time.Time) error
	SumAmountByTypeAndObjectID(ctx context.Context, typ uint8, objectID int64) (int64, error)
}

// TaskRepo task 数据访问接口
type TaskRepo interface {
	Insert(ctx context.Context, data *task.Task) error
	FindOne(ctx context.Context, id int64) (*task.Task, error)
	FindOneByType(ctx context.Context, id int64, typ task.Type) (*task.Task, error)
	QueryTaskList(ctx context.Context, filter *task.Filter) (int64, []*task.Task, error)
	Update(ctx context.Context, data *task.Task) error
	UpdateActive(ctx context.Context, data *task.Task) (bool, error)
	UpdateStatus(ctx context.Context, id int64, status int8) error
	UpdateStatusFrom(ctx context.Context, id int64, typ task.Type, from []int8, status int8) (bool, error)
	UpdateStatusAndErrorFrom(ctx context.Context, id int64, typ task.Type, from []int8, status int8, taskError string) (bool, error)
}

// ClientRepo subscribe application 数据访问接口
type ClientRepo interface {
	Insert(ctx context.Context, data *client.SubscribeApplication) error
	FindOne(ctx context.Context, id int64) (*client.SubscribeApplication, error)
	Update(ctx context.Context, data *client.SubscribeApplication) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context) ([]*client.SubscribeApplication, error)
}

// InboxRepo is the idempotent-consumer inbox (ADR-001 step 2): a domain step
// records that it processed an event inside its own transaction, so
// at-least-once deliveries and reconciliation replays never apply the same
// mutation twice.
// OutboxRepo is the generic domain-event outbox: Append runs inside the
// owning domain's transaction; the dispatcher drains unpublished events and
// marks them published once every subscriber has processed them.
type OutboxRepo interface {
	Append(ctx context.Context, topic, eventKey, payload string) error
	ListUnpublished(ctx context.Context, limit int) ([]*outbox.Event, error)
	MarkPublished(ctx context.Context, id int64) error
	DeletePublishedBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

type InboxRepo interface {
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
