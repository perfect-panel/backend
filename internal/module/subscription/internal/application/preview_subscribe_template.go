package application

import (
	"context"
	"fmt"

	dto "github.com/perfect-panel/server/internal/module/subscription/contract"
	"github.com/perfect-panel/server/internal/module/subscription/internal/render"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/xerr"
)

// previewNodeLimit bounds the nodes a template preview renders.
const previewNodeLimit = 1000

// PreviewSubscribeTemplate renders the application's template over the
// enabled nodes for a sample subscriber.
func (s *Service) PreviewSubscribeTemplate(ctx context.Context, req *dto.PreviewSubscribeTemplateRequest) (*dto.PreviewSubscribeTemplateResponse, error) {
	log := logger.WithContext(ctx)
	servers, err := s.deps.Nodes.ListEnabledNodes(ctx, previewNodeLimit)
	if err != nil {
		log.Errorf("[PreviewSubscribeTemplateLogic] FindAllServer error: %v", err.Error())
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "list the enabled nodes")
	}

	data, err := s.deps.Clients.FindOne(ctx, req.Id)
	if err != nil {
		log.Errorf("[PreviewSubscribeTemplateLogic] FindOne error: %v", err.Error())
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "FindOneClient error: %v", err.Error())
	}

	// Preview renders with the application's own defaults so it matches what a
	// client receives when its subscription URL carries no params of its own.
	defaultParams, err := data.DefaultParamValues()
	if err != nil {
		log.Errorf("[PreviewSubscribeTemplateLogic] Ignoring malformed default params %q: %v", data.DefaultParams, err)
	}

	sub := render.NewAdapter(data.SubscribeTemplate, render.WithServers(servers),
		render.WithParams(defaultParams),
		render.WithSiteName("PerfectPanel"),
		render.WithSubscribeName("Test Subscribe"),
		render.WithOutputFormat(data.OutputFormat),
		render.WithUserInfo(render.User{
			ID:           10000,
			Password:     "00000000-0000-4000-8000-000000000001",
			ExpiredAt:    timeutil.Now().AddDate(1, 0, 0),
			Download:     0,
			Upload:       0,
			Traffic:      1000,
			SubscribeURL: "https://example.com/subscribe",
		}))
	// The response carries the renderer's message: it tells the administrator
	// what is wrong with the template.
	a, err := sub.Client()
	if err != nil {
		log.Errorf("[PreviewSubscribeTemplateLogic] Client error: %v", err.Error())
		return nil, fmt.Errorf("client error: %v: %w", err.Error(), xerr.NewErrMsg(err.Error()))
	}
	bytes, err := a.Build()
	if err != nil {
		log.Errorf("[PreviewSubscribeTemplateLogic] Build error: %v", err.Error())
		return nil, fmt.Errorf("build error: %v: %w", err.Error(), xerr.NewErrMsg(err.Error()))
	}
	return &dto.PreviewSubscribeTemplateResponse{
		Template: string(bytes),
	}, nil
}
