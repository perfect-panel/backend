package traffic

import (
	"context"

	"github.com/pars-aria-labs/asynq"
)

// LogCleaner is the platform module's log retention, which also prunes the
// network's raw traffic log.
type LogCleaner interface {
	CleanupLogs(ctx context.Context) error
}

// LogCleanupHandler is the queue shell of the log retention. It runs on its
// own schedule, apart from the traffic statistics, so a failed statistics
// run cannot silently disable the cleanup (or the reverse).
type LogCleanupHandler struct {
	cleaner LogCleaner
}

// NewLogCleanupHandler builds the shell over the platform facade.
func NewLogCleanupHandler(cleaner LogCleaner) *LogCleanupHandler {
	return &LogCleanupHandler{cleaner: cleaner}
}

func (h *LogCleanupHandler) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	if h.cleaner == nil {
		return nil
	}
	return h.cleaner.CleanupLogs(ctx)
}
