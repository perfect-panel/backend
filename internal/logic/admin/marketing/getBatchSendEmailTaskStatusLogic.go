package marketing

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
)

type GetBatchSendEmailTaskStatusLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewGetBatchSendEmailTaskStatusLogic Get batch send email task status
func NewGetBatchSendEmailTaskStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetBatchSendEmailTaskStatusLogic {
	return &GetBatchSendEmailTaskStatusLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetBatchSendEmailTaskStatusLogic) GetBatchSendEmailTaskStatus(req *dto.GetBatchSendEmailTaskStatusRequest) (resp *dto.GetBatchSendEmailTaskStatusResponse, err error) {
	taskInfo, err := l.svcCtx.Store.Task().FindOne(l.ctx, req.Id)
	if err != nil {
		l.Errorf("failed to get email task status, error: %v", err)
		return nil, xerr.NewErrCode(xerr.DatabaseQueryError)
	}

	return &dto.GetBatchSendEmailTaskStatusResponse{
		Status:  uint8(taskInfo.Status),
		Total:   int64(taskInfo.Total),
		Current: int64(taskInfo.Current),
		Errors:  taskInfo.Errors,
	}, nil
}
