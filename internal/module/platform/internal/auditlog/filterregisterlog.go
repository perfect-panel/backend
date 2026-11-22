package auditlog

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
)

// FilterRegisterLog pages the registrations.
func (s *Service) FilterRegisterLog(ctx context.Context, req *dto.FilterRegisterLogRequest) (*dto.FilterRegisterLogResponse, error) {
	total, list, err := logPage(ctx, s.deps.Logs, "registration", filterParams(log.TypeRegister, req.UserId, req.FilterLogParams), func(row *log.SystemLog, content *log.Register) dto.RegisterLog {
		return withRequestMetadata(&dto.RegisterLog{
			UserId:     row.ObjectID,
			AuthMethod: content.AuthMethod,
			Identifier: content.Identifier,
			RegisterIP: content.RegisterIP,
			Timestamp:  row.CreatedAt.UnixMilli(),
		}, content.Request())
	})
	if err != nil {
		return nil, err
	}
	return &dto.FilterRegisterLogResponse{Total: total, List: list}, nil
}
