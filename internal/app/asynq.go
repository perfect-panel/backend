package app

import (
	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/infra/taskqueue"
)

// QueueRedisOpt is shared by producers, workers and inspectors. Asynq scopes
// its internal keys and Pub/Sub channels with the same prefix as the panel.
func QueueRedisOpt(c config.Config) asynq.RedisClientOpt {
	return asynq.RedisClientOpt{
		Addr: c.Redis.Host, Username: c.Redis.Username, Password: c.Redis.Pass,
		DB: c.Redis.QueueDB, Prefix: c.Redis.KeyPrefix,
	}
}

// NewAsynqClient returns the tracing asynq client: EnqueueContext stamps the
// caller's trace context onto the task for the worker-side middleware to
// resume. Pass task options to EnqueueContext, not NewTask — wrapping
// rebuilds the task.
func NewAsynqClient(c config.Config) *taskqueue.Client {
	return taskqueue.NewClient(asynq.NewClient(QueueRedisOpt(c)))
}

// NewAsynqInspector returns the queue inspector with which the paid-order
// reconciler repairs conflicting activation tasks.
func NewAsynqInspector(c config.Config) *asynq.Inspector {
	return asynq.NewInspector(QueueRedisOpt(c))
}
