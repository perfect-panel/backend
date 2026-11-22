package adminserver

import (
	"context"
	"strings"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/slicesx"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// QueryNodeTag lists every tag any node carries, once.
func (s *Service) QueryNodeTag(ctx context.Context) (*dto.QueryNodeTagResponse, error) {
	nodeTags, err := s.deps.Store.Node().QueryNodeTags(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[QueryNodeTag] Query Database Error: ", logger.Field("error", err.Error()))
		return nil, xerr.Errorf(xerr.DatabaseQueryError, "[QueryNodeTag] Query Database Error")
	}
	var tags []string
	for _, item := range nodeTags {
		tags = append(tags, strings.Split(item, ",")...)
	}

	return &dto.QueryNodeTagResponse{
		Tags: slicesx.RemoveDuplicateElements(tags...),
	}, nil
}
