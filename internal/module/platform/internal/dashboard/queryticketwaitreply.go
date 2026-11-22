package dashboard

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
)

// QueryTicketWaitReply counts the tickets waiting for a staff reply.
func (s *Service) QueryTicketWaitReply(ctx context.Context) (*dto.TicketWaitRelpyResponse, error) {
	count, err := s.deps.Tickets.QueryWaitReplyTotal(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[QueryTicketWaitReply] Query Database Error", logger.Field("error", err.Error()))
		return nil, err
	}
	return &dto.TicketWaitRelpyResponse{Count: count}, nil
}
