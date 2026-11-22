package auditlog

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
)

// FilterSubscribeLog pages a user's subscription fetches, optionally those of
// one subscription. The subscription is matched on the content's
// user_subscribe_id field itself: a text pattern for 12 would also find 120
// and 1200.
func (s *Service) FilterSubscribeLog(ctx context.Context, req *dto.FilterSubscribeLogRequest) (*dto.FilterSubscribeLogResponse, error) {
	params := filterParams(log.TypeSubscribe, req.UserId, req.FilterLogParams)
	// The subscription log is searched by subscription only.
	params.Search = ""
	if req.UserSubscribeId != 0 {
		params.ContentInt64 = map[string]int64{"user_subscribe_id": req.UserSubscribeId}
	}
	total, list, err := logPage(ctx, s.deps.Logs, "subscription", params, func(row *log.SystemLog, content *log.Subscribe) dto.SubscribeLog {
		return withRequestMetadata(&dto.SubscribeLog{
			UserId:          row.ObjectID,
			Token:           content.Token,
			UserSubscribeId: content.UserSubscribeId,
			Timestamp:       row.CreatedAt.UnixMilli(),
		}, content.Request())
	})
	if err != nil {
		return nil, err
	}
	return &dto.FilterSubscribeLogResponse{Total: total, List: list}, nil
}
