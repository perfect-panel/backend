package application

import (
	"context"
	"encoding/json"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/client"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetSubscribeApplicationList lists every client application in its stored
// order; the request's paging is not applied.
func (s *Service) GetSubscribeApplicationList(ctx context.Context, _ *dto.GetSubscribeApplicationListRequest) (*dto.GetSubscribeApplicationListResponse, error) {
	data, err := s.deps.Clients.List(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorf("Failed to get subscribe application list: %v", err)
		return nil, xerr.Errorf(xerr.DatabaseQueryError, "Failed to get subscribe application list")
	}
	var list []dto.SubscribeApplication
	for _, item := range data {
		var link dto.DownloadLink
		if item.DownloadLink != "" {
			_ = json.Unmarshal([]byte(item.DownloadLink), &link)
		}
		list = append(list, applicationView(item, link))
	}
	return &dto.GetSubscribeApplicationListResponse{
		Total: int64(len(list)),
		List:  list,
	}, nil
}

// applicationView is the admin view of a stored client application; link is
// its download links, which the row stores encoded.
func applicationView(app *client.SubscribeApplication, link dto.DownloadLink) dto.SubscribeApplication {
	return dto.SubscribeApplication{
		Id:                app.Id,
		Name:              app.Name,
		Description:       app.Description,
		Icon:              app.Icon,
		Scheme:            app.Scheme,
		UserAgent:         app.UserAgent,
		IsDefault:         app.IsDefault,
		SubscribeTemplate: app.SubscribeTemplate,
		OutputFormat:      app.OutputFormat,
		DefaultParams:     app.DefaultParams,
		DownloadLink:      link,
		CreatedAt:         app.CreatedAt.UnixMilli(),
		UpdatedAt:         app.UpdatedAt.UnixMilli(),
	}
}
