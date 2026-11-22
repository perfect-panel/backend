package application

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/client"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// CreateSubscribeApplication stores a client application and returns it as
// stored, with the download links as the request gave them.
func (s *Service) CreateSubscribeApplication(ctx context.Context, req *dto.CreateSubscribeApplicationRequest) (*dto.SubscribeApplication, error) {
	log := logger.WithContext(ctx)
	link := client.DownloadLink(req.DownloadLink)
	linkData, err := link.Marshal()
	if err != nil {
		log.Errorf("Failed to marshal download link: %v", err)
		return nil, xerr.Errorf(xerr.ERROR, " Failed to marshal download link")
	}
	data := &client.SubscribeApplication{
		Name:              req.Name,
		Icon:              req.Icon,
		Description:       req.Description,
		Scheme:            req.Scheme,
		UserAgent:         req.UserAgent,
		IsDefault:         req.IsDefault,
		SubscribeTemplate: req.SubscribeTemplate,
		OutputFormat:      req.OutputFormat,
		DefaultParams:     req.DefaultParams,
		DownloadLink:      string(linkData),
	}

	if err := s.deps.Clients.Insert(ctx, data); err != nil {
		log.Errorf("Failed to create subscribe application: %v", err)
		return nil, xerr.Errorf(xerr.DatabaseInsertError, "Failed to create subscribe application")
	}

	resp := applicationView(data, req.DownloadLink)
	return &resp, nil
}
