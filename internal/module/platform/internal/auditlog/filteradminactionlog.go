package auditlog

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
)

// FilterAdminActionLog pages the administrators' mutations: every settings
// change, marketing task, ticket action and Telegram bot command that
// changed something, with who did it and from where. user_id narrows the
// trail to one administrator; search matches the action, object and detail
// text.
func (s *Service) FilterAdminActionLog(ctx context.Context, req *dto.FilterAdminActionLogRequest) (*dto.FilterAdminActionLogResponse, error) {
	total, list, err := logList[log.AdminAction](ctx, s.deps.Logs, "admin action", log.TypeAdminAction, req.UserId, req.FilterLogParams, adminActionView)
	if err != nil {
		return nil, err
	}
	return &dto.FilterAdminActionLogResponse{Total: total, List: list}, nil
}

// adminActionView shows one recorded action with the request it came from.
func adminActionView(row *log.SystemLog, content *log.AdminAction) dto.AdminActionLog {
	return withRequestMetadata(&dto.AdminActionLog{
		Id:               row.Id,
		UserId:           row.ObjectID,
		Action:           content.Action,
		Object:           content.Object,
		ObjectId:         content.ObjectID,
		Detail:           content.Detail,
		Source:           content.Source,
		TelegramSenderId: content.TelegramSenderID,
		Timestamp:        content.Timestamp,
		CreatedAt:        row.CreatedAt.UnixMilli(),
	}, content.Metadata)
}
