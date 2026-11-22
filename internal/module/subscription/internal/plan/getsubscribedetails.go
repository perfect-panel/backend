package plan

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/backend/internal/infra/mapping"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetSubscribeDetails returns a plan with its discounts and node selection
// decoded.
func (s *Service) GetSubscribeDetails(ctx context.Context, req *dto.GetSubscribeDetailsRequest) (*dto.Subscribe, error) {
	log := logger.WithContext(ctx)
	sub, err := s.deps.Plans.FindOne(ctx, req.Id)
	if err != nil {
		log.Error("[GetSubscribeDetailsLogic] get subscribe details failed: ", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "get subscribe details failed: %v", err.Error())
	}
	resp := &dto.Subscribe{}
	if err := mapping.Copy(resp, sub); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "map plan %d", sub.Id)
	}
	if sub.Discount != "" {
		if err := json.Unmarshal([]byte(sub.Discount), &resp.Discount); err != nil {
			log.Error("[GetSubscribeDetailsLogic] JSON unmarshal failed: ", logger.Field("error", err.Error()), logger.Field("discount", sub.Discount))
		}
	}
	nodes, tags, err := sub.NodeScope()
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "plan %d nodes: %v", sub.Id, err)
	}
	resp.Nodes = dto.StringInt64Slice(nodes)
	resp.NodeTags = tags
	return resp, nil
}
