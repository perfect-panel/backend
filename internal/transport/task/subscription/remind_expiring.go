package subscription

import (
	"context"

	"github.com/pars-aria-labs/asynq"
	module "github.com/perfect-panel/backend/internal/module/subscription"
)

// RemindExpiringHandler is the queue shell of the pre-expiry reminder.
type RemindExpiringHandler struct {
	service module.Service
}

// NewRemindExpiringHandler builds the shell over the subscription facade.
func NewRemindExpiringHandler(service module.Service) *RemindExpiringHandler {
	return &RemindExpiringHandler{service: service}
}

func (h *RemindExpiringHandler) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	return h.service.RemindExpiringSubscriptions(ctx)
}
