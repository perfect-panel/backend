package auditlog

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
)

// FilterEmailLog pages the emails sent.
func (s *Service) FilterEmailLog(ctx context.Context, req *dto.FilterLogParams) (*dto.FilterEmailLogResponse, error) {
	total, list, err := messageLogPage(ctx, s.deps.Logs, "email", log.TypeEmailMessage, req)
	if err != nil {
		return nil, err
	}
	return &dto.FilterEmailLogResponse{Total: total, List: list}, nil
}

// FilterMobileLog pages the text messages sent.
func (s *Service) FilterMobileLog(ctx context.Context, req *dto.FilterLogParams) (*dto.FilterMobileLogResponse, error) {
	total, list, err := messageLogPage(ctx, s.deps.Logs, "mobile", log.TypeMobileMessage, req)
	if err != nil {
		return nil, err
	}
	return &dto.FilterMobileLogResponse{Total: total, List: list}, nil
}

// messageLogPage pages the message log of kind, the emails or the text
// messages sent.
func messageLogPage(ctx context.Context, logs logFilter, name string, kind log.Type, req *dto.FilterLogParams) (int64, []dto.MessageLog, error) {
	return logPage(ctx, logs, name, filterParams(kind, 0, *req), func(row *log.SystemLog, content *log.Message) dto.MessageLog {
		return withRequestMetadata(&dto.MessageLog{
			Id:        row.Id,
			Type:      row.Type,
			Platform:  content.Platform,
			To:        content.To,
			Subject:   content.Subject,
			Content:   content.Content,
			Status:    content.Status,
			CreatedAt: row.CreatedAt.UnixMilli(),
		}, content.Metadata)
	})
}
