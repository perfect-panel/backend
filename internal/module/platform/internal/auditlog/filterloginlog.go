package auditlog

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
)

// FilterLoginLog pages a user's login attempts.
func (s *Service) FilterLoginLog(ctx context.Context, req *dto.FilterLoginLogRequest) (*dto.FilterLoginLogResponse, error) {
	total, list, err := logPage(ctx, s.deps.Logs, "login", filterParams(log.TypeLogin, req.UserId, req.FilterLogParams), func(row *log.SystemLog, content *log.Login) dto.LoginLog {
		return withRequestMetadata(&dto.LoginLog{
			UserId:    row.ObjectID,
			Method:    content.Method,
			LoginIP:   content.LoginIP,
			Success:   content.Success,
			Timestamp: row.CreatedAt.UnixMilli(),
		}, content.Request())
	})
	if err != nil {
		return nil, err
	}
	return &dto.FilterLoginLogResponse{Total: total, List: list}, nil
}
