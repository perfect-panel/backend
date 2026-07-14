package marketing

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/email"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
)

type StopBatchSendEmailTaskLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewStopBatchSendEmailTaskLogic Stop a batch send email task
func NewStopBatchSendEmailTaskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StopBatchSendEmailTaskLogic {
	return &StopBatchSendEmailTaskLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *StopBatchSendEmailTaskLogic) StopBatchSendEmailTask(req *dto.StopBatchSendEmailTaskRequest) (err error) {
	if email.Manager != nil {
		email.Manager.RemoveWorker(req.Id)
	} else {
		logger.Error("[StopBatchSendEmailTaskLogic] email.Manager is nil, cannot stop task")
	}
	err = l.svcCtx.Store.Task().UpdateStatus(l.ctx, req.Id, 2)

	if err != nil {
		l.Errorf("failed to stop email task, error: %v", err)
		return xerr.NewErrCode(xerr.DatabaseUpdateError)
	}
	return
}
