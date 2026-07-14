package server

import (
	"context"
	"strings"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type FilterNodeListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewFilterNodeListLogic Filter Node List
func NewFilterNodeListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FilterNodeListLogic {
	return &FilterNodeListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *FilterNodeListLogic) FilterNodeList(req *dto.FilterNodeListRequest) (resp *dto.FilterNodeListResponse, err error) {
	total, data, err := l.svcCtx.Store.Node().FilterNodeList(l.ctx, &node.FilterNodeParams{
		Page:   req.Page,
		Size:   req.Size,
		Search: req.Search,
	})

	if err != nil {
		l.Errorw("[FilterNodeList] Query Database Error: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "[FilterNodeList] Query Database Error")
	}

	list := make([]dto.Node, 0)
	for _, datum := range data {
		list = append(list, dto.Node{
			Id:        datum.Id,
			Name:      datum.Name,
			Tags:      tool.RemoveDuplicateElements(strings.Split(datum.Tags, ",")...),
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
