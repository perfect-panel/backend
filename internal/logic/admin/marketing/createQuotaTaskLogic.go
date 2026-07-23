package marketing

import (
	"context"
	"strconv"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/task"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	queueType "github.com/perfect-panel/server/queue/types"
	"github.com/pkg/errors"
)

type CreateQuotaTaskLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewCreateQuotaTaskLogic Create a quota task
func NewCreateQuotaTaskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateQuotaTaskLogic {
	return &CreateQuotaTaskLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateQuotaTaskLogic) CreateQuotaTask(req *dto.CreateQuotaTaskRequest) error {
	subIds, err := l.svcCtx.Store.UserSubscription().QuerySubscribeIdsByFilter(l.ctx, &user.SubscribeFilter{
		Subscribers: req.Subscribers,
		IsActive:    req.IsActive,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
	})
	if err != nil {
		l.Errorf("[CreateQuotaTask] find subscribers error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribers error")
	}
	if len(subIds) == 0 {
		return errors.Wrapf(xerr.NewErrMsg("No subscribers found"), "no subscribers found")
	}

	scopeInfo := task.QuotaScope{
		Subscribers: req.Subscribers,
		IsActive:    req.IsActive,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
		Objects:     subIds,
	}
	scopeBytes, _ := scopeInfo.Marshal()
	contentInfo := task.QuotaContent{
		ResetTraffic: req.ResetTraffic,
		Days:         req.Days,
		GiftType:     req.GiftType,
		GiftValue:    req.GiftValue,
	}
	contentBytes, _ := contentInfo.Marshal()
	// create task
	newTask := &task.Task{
		Type:    task.TypeQuota,
		Status:  0,
		Scope:   string(scopeBytes),
		Content: string(contentBytes),
		Total:   uint64(len(subIds)),
		Current: 0,
		Errors:  "",
	}

	if err := l.svcCtx.Store.Task().Insert(l.ctx, newTask); err != nil {
		l.Errorf("[CreateQuotaTask] create task error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create task error")
	}

	// enqueue task
	queueTask := asynq.NewTask(queueType.ForthwithQuotaTask, []byte(strconv.FormatInt(newTask.Id, 10)))
	if _, err := l.svcCtx.Queue.EnqueueContext(l.ctx, queueTask); err != nil {
		l.Errorf("[CreateQuotaTask] enqueue task error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.QueueEnqueueError), "enqueue task error")
	}
	logger.Infof("[CreateQuotaTask] Successfully created task with ID: %d", newTask.Id)
	return nil
}
