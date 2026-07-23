package orderLogic

import (
	"context"
	"strconv"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/orderstream"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

// PublishOrderEventsLogic drains the durable order event outbox. Publishing
// is intentionally separate from writing the event: a Redis outage may delay
// a notification but can never roll back a committed payment state.
type PublishOrderEventsLogic struct {
	svcCtx *svc.ServiceContext
}

func NewPublishOrderEventsLogic(svcCtx *svc.ServiceContext) *PublishOrderEventsLogic {
	return &PublishOrderEventsLogic{svcCtx: svcCtx}
}

func (l *PublishOrderEventsLogic) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	events, err := l.svcCtx.Store.OrderEvent().ListUnpublished(ctx, 500)
	if err != nil {
		return err
	}
	for _, event := range events {
		if err := l.svcCtx.Redis.Publish(ctx, orderstream.Channel(event.OrderNo), strconv.FormatInt(event.ID, 10)).Err(); err != nil {
			return err
		}
		if _, err := l.svcCtx.Store.OrderEvent().MarkPublished(ctx, event.ID, time.Now()); err != nil {
			return err
		}
	}
	if len(events) > 0 {
		logger.WithContext(ctx).Debugf("published %d order events", len(events))
	}
	return nil
}
