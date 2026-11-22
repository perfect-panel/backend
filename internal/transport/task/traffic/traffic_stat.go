package traffic

import (
	"context"

	"github.com/pars-aria-labs/asynq"
)

// StatRecorder is the network module's daily traffic statistics.
type StatRecorder interface {
	RecordDailyTrafficStatistics(ctx context.Context) error
}

// StatHandler is the queue shell of the daily traffic statistics; the network
// module records each day once however often the task runs.
type StatHandler struct {
	stats StatRecorder
}

// NewStatHandler builds the shell over the network facade.
func NewStatHandler(stats StatRecorder) *StatHandler {
	return &StatHandler{stats: stats}
}

func (h *StatHandler) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	if h.stats == nil {
		return nil
	}
	return h.stats.RecordDailyTrafficStatistics(ctx)
}
