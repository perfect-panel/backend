package server

import (
	"context"
	"strings"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type QueryNodeTagLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryNodeTagLogic Query all node tags
func NewQueryNodeTagLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryNodeTagLogic {
	return &QueryNodeTagLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryNodeTagLogic) QueryNodeTag() (resp *dto.QueryNodeTagResponse, err error) {

	nodeTags, err := l.svcCtx.Store.Node().QueryNodeTags(l.ctx)
	if err != nil {
		l.Errorw("[QueryNodeTag] Query Database Error: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "[QueryNodeTag] Query Database Error")
	}
	var tags []string
	for _, item := range nodeTags {
		tags = append(tags, strings.Split(item, ",")...)
	}

	return &dto.QueryNodeTagResponse{
		Tags: tool.RemoveDuplicateElements(tags...),
	}, nil
}
