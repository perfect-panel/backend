package publicinfo

import (
	"context"
	"encoding/json"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetClient lists the client applications of the public download page in
// their stored order. A download link that does not decode is left empty.
func (s *Service) GetClient(ctx context.Context) (*dto.GetSubscribeClientResponse, error) {
	data, err := s.deps.Clients.ListClientApplications(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorf("Failed to get subscribe application list: %v", err)
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "list the subscribe applications")
	}
	var list []dto.SubscribeClient
	for _, item := range data {
		var links dto.PlatformDownloadLinkSnapshot
		if item.DownloadLink != "" {
			_ = json.Unmarshal([]byte(item.DownloadLink), &links)
		}
		list = append(list, dto.SubscribeClient{
			Id:           item.Id,
			Name:         item.Name,
			Description:  item.Description,
			Icon:         item.Icon,
			Scheme:       item.Scheme,
			IsDefault:    item.IsDefault,
			DownloadLink: links,
		})
	}
	return &dto.GetSubscribeClientResponse{
		Total: int64(len(list)),
		List:  list,
	}, nil
}
