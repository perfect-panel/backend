package application

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/client"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateSubscribeApplication replaces a client application's settings and
// returns it as stored, with the download links as the request gave them.
func (s *Service) UpdateSubscribeApplication(ctx context.Context, req *dto.UpdateSubscribeApplicationRequest) (*dto.SubscribeApplication, error) {
	log := logger.WithContext(ctx)
	data, err := s.deps.Clients.FindOne(ctx, req.Id)
	if err != nil {
		log.Errorf("Failed to find subscribe application with ID %d: %v", req.Id, err)
		return nil, xerr.Errorf(xerr.DatabaseQueryError, "Failed to find subscribe application with ID %d", req.Id)
	}
	link := client.DownloadLink(req.DownloadLink)
	linkData, err := link.Marshal()
	if err != nil {
		log.Errorf("Failed to marshal download link: %v", err)
		return nil, xerr.Errorf(xerr.ERROR, " Failed to marshal download link")
	}

	data.Name = req.Name
	data.Icon = req.Icon
	data.Description = req.Description
	data.Scheme = req.Scheme
	data.UserAgent = req.UserAgent
	data.IsDefault = req.IsDefault
	data.SubscribeTemplate = req.SubscribeTemplate
	data.OutputFormat = req.OutputFormat
	data.DefaultParams = req.DefaultParams
	data.DownloadLink = string(linkData)
	if err := s.deps.Clients.Update(ctx, data); err != nil {
		log.Errorf("Failed to update subscribe application with ID %d: %v", req.Id, err)
		return nil, xerr.Errorf(xerr.DatabaseUpdateError, "Failed to update subscribe application with ID %d", req.Id)
	}
	resp := applicationView(data, req.DownloadLink)
	return &resp, nil
}
