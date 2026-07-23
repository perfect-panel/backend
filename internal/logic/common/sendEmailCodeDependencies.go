package common

import (
	"context"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/redis/go-redis/v9"
)

// EmailCodePolicy contains the authentication policy required before issuing
// an email verification code.
type EmailCodePolicy interface {
	EnsureRegistrationOpen(ctx context.Context, method string) error
	EnsureMethodEnabled(ctx context.Context, method string) error
}

// EmailCodeStore is the persistence surface used by email code delivery. It
// excludes unrelated application repositories.
type EmailCodeStore interface {
	UserAuth() repository.UserAuthRepo
}

// EmailTaskQueue publishes email delivery tasks.
type EmailTaskQueue interface {
	Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
}

// EmailCodeConfig is the configuration snapshot consumed by email code
// delivery.
type EmailCodeConfig struct {
	DomainSuffixList   string
	EnableDomainSuffix bool
	VerifyCodeInterval int64
	VerifyCodeLimit    int64
	VerifyCodeExpire   int64
	SiteLogo           string
	SiteName           string
}

// SendEmailCodeDependencies explicitly declares the collaborators of email
// code delivery instead of passing ServiceContext to business logic.
type SendEmailCodeDependencies struct {
	Store  EmailCodeStore
	Redis  *redis.Client
	Queue  EmailTaskQueue
	Config EmailCodeConfig
	Policy EmailCodePolicy
}
