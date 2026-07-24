package storefront

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type QuerySubscribeGroupListLogic struct {
	logger.Logger
	ctx  context.Context
	deps Deps
}

// Get subscribe group list
func newQuerySubscribeGroupListLogic(ctx context.Context, deps Deps) *QuerySubscribeGroupListLogic {
	return &QuerySubscribeGroupListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *QuerySubscribeGroupListLogic) QuerySubscribeGroupList() (resp *dto.QuerySubscribeGroupListResponse, err error) {
	total, list, err := l.deps.Plans.QueryGroupList(l.ctx)
	if err != nil {
		l.Logger.Error("[QuerySubscribeGroupListLogic] get subscribe group list failed: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get subscribe group list failed: %v", err.Error())
	}
	groupList := make([]dto.SubscribeGroup, 0)
	tool.DeepCopy(&groupList, list)
	return &dto.QuerySubscribeGroupListResponse{
		Total: total,
		List:  groupList,
	}, nil
}
