package orderLogic

import (
	"context"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

const orderEventRetention = 30 * 24 * time.Hour

// CleanupOrderEventsLogic removes only events that have already reached Redis
// and are older than the replay contract. Unpublished events are never
// deleted, even if an outage lasts longer than the normal retention period.
type CleanupOrderEventsLogic struct {
	svcCtx *svc.ServiceContext
}

func NewCleanupOrderEventsLogic(svcCtx *svc.ServiceContext) *CleanupOrderEventsLogic {
	return &CleanupOrderEventsLogic{svcCtx: svcCtx}
}

func (l *CleanupOrderEventsLogic) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	deleted, err := l.svcCtx.Store.OrderEvent().DeletePublishedBefore(ctx, time.Now().Add(-orderEventRetention))
	if err != nil {
		return err
	}
	if deleted > 0 {
		logger.WithContext(ctx).Infof("removed %d expired order events", deleted)
	}
	return nil
}
