package adminserver

import (
	"context"
	"strings"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/slicesx"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// FilterNodeList lists a page of the nodes matching the search.
func (s *Service) FilterNodeList(ctx context.Context, req *dto.FilterNodeListRequest) (*dto.FilterNodeListResponse, error) {
	total, data, err := s.deps.Store.Node().FilterNodeList(ctx, &node.FilterNodeParams{
		Page:   req.Page,
		Size:   req.Size,
		Search: req.Search,
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("[FilterNodeList] Query Database Error: ", logger.Field("error", err.Error()))
		return nil, xerr.Errorf(xerr.DatabaseQueryError, "[FilterNodeList] Query Database Error")
	}

	list := make([]dto.Node, 0)
	for _, datum := range data {
		list = append(list, dto.Node{
			Id:        datum.Id,
			Name:      datum.Name,
			Tags:      slicesx.RemoveDuplicateElements(strings.Split(datum.Tags, ",")...),
			Port:      datum.Port,
			Address:   datum.Address,
			ServerId:  datum.ServerId,
			Protocol:  datum.Protocol,
			Enabled:   datum.Enabled,
			Sort:      datum.Sort,
			CreatedAt: datum.CreatedAt.UnixMilli(),
			UpdatedAt: datum.UpdatedAt.UnixMilli(),
		})
	}

	return &dto.FilterNodeListResponse{
		List:  list,
		Total: total,
	}, nil
}
