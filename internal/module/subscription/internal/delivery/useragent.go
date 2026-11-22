package delivery

import (
	"context"
	"strings"

	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/slicesx"
)

func (s *Service) IsUserAgentAllowed(ctx context.Context, userAgent string) bool {
	if userAgent == "" {
		return false
	}

	keywords := slicesx.RemoveDuplicateElements(strings.Split(s.deps.config().UserAgentList, "\n")...)
	clients, err := s.deps.Clients.List(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[Subscribe] Query client list failed", logger.Field("error", err.Error()))
	}
	for _, item := range clients {
		keywords = append(keywords, item.UserAgent)
	}

	userAgent = strings.ToLower(userAgent)
	for _, keyword := range keywords {
		keyword = strings.ToLower(strings.TrimSpace(keyword))
		if keyword == "" {
			continue
		}
		if strings.Contains(userAgent, keyword) {
			return true
		}
	}
	return false
}
