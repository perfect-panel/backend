package marketing

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/task"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type QueryQuotaTaskStatusLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryQuotaTaskStatusLogic Query quota task status
func NewQueryQuotaTaskStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryQuotaTaskStatusLogic {
	return &QueryQuotaTaskStatusLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryQuotaTaskStatusLogic) QueryQuotaTaskStatus(req *dto.QueryQuotaTaskStatusRequest) (resp *dto.QueryQuotaTaskStatusResponse, err error) {
	data, err := l.svcCtx.Store.Task().FindOneByType(l.ctx, req.Id, task.TypeQuota)
	if err != nil {
		l.Errorf("[QueryQuotaTaskStatus] failed to get quota task: %v", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), " failed to get quota task: %v", err.Error())
	}
	return &dto.QueryQuotaTaskStatusResponse{
		Status:  uint8(data.Status),
		Current: int64(data.Current),
		Total:   int64(data.Total),
		Errors:  data.Errors,
	}, nil
}
