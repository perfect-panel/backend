package auditlog

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetMessageLogList pages the email or the text message log. A row whose
// content does not decode fails the page.
func (s *Service) GetMessageLogList(ctx context.Context, req *dto.GetMessageLogListRequest) (*dto.GetMessageLogListResponse, error) {
	if req == nil || (req.Type != log.TypeEmailMessage.Uint8() && req.Type != log.TypeMobileMessage.Uint8()) {
		return nil, xerr.Errorf(xerr.InvalidParams, "message log type must be email or mobile")
	}
	data, total, err := s.deps.Logs.FilterSystemLog(ctx, &log.FilterParams{
		Page:   req.Page,
		Size:   req.Size,
		Type:   req.Type,
		Search: req.Search,
	})
	if err != nil {
		logger.WithContext(ctx).Errorf("[GetMessageLogList] failed to filter system log: %v", err.Error())
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "failed to filter system log: %v", err.Error())
	}

	var list []dto.MessageLog
	for _, datum := range data {
		var content log.Message
		if err := content.Unmarshal([]byte(datum.Content)); err != nil {
			logger.WithContext(ctx).Errorf("[GetMessageLogList] failed to unmarshal content: %v", err.Error())
			return nil, xerr.Wrapf(err, xerr.ERROR, "corrupt message log %d: %v", datum.Id, err)
		}
		list = append(list, withRequestMetadata(&dto.MessageLog{
			Id:        datum.Id,
			Type:      datum.Type,
			Platform:  content.Platform,
			To:        content.To,
			Subject:   content.Subject,
			Content:   content.Content,
			Status:    content.Status,
			CreatedAt: datum.CreatedAt.UnixMilli(),
		}, content.Metadata))
	}

	return &dto.GetMessageLogListResponse{
		Total: total,
		List:  list,
	}, nil
}
