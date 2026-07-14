package marketing

import (
	"context"
	"strings"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/task"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
)

type GetBatchSendEmailTaskListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewGetBatchSendEmailTaskListLogic Get batch send email task list
func NewGetBatchSendEmailTaskListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetBatchSendEmailTaskListLogic {
	return &GetBatchSendEmailTaskListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetBatchSendEmailTaskListLogic) GetBatchSendEmailTaskList(req *dto.GetBatchSendEmailTaskListRequest) (resp *dto.GetBatchSendEmailTaskListResponse, err error) {

	if req.Page == 0 {
		req.Page = 1
	}
	if req.Size == 0 {
		req.Size = 10
	}
	total, tasks, err := l.svcCtx.Store.Task().QueryTaskList(l.ctx, &task.Filter{
		Type:   task.TypeEmail,
		Page:   req.Page,
		Size:   req.Size,
		Status: req.Status,
		Scope:  req.Scope,
	})
	if err != nil {
		l.Errorf("failed to get email tasks: %v", err)
		return nil, xerr.NewErrCode(xerr.DatabaseQueryError)
	}

	list := make([]dto.BatchSendEmailTask, 0)

	for _, t := range tasks {
		var scopeInfo task.EmailScope
		if err = scopeInfo.Unmarshal([]byte(t.Scope)); err != nil {
			l.Errorf("[GetBatchSendEmailTaskList] failed to unmarshal email task scope: %v", err.Error())
			continue
		}
		var contentInfo task.EmailContent
		if err = contentInfo.Unmarshal([]byte(t.Content)); err != nil {
			l.Errorf("[GetBatchSendEmailTaskList] failed to unmarshal email task content: %v", err.Error())
			continue
		}

		list = append(list, dto.BatchSendEmailTask{
			Id:                t.Id,
			Subject:           contentInfo.Subject,
			Content:           contentInfo.Content,
			Recipients:        strings.Join(scopeInfo.Recipients, "\n"),
			Scope:             scopeInfo.Type,
			RegisterStartTime: scopeInfo.RegisterStartTime,
			RegisterEndTime:   scopeInfo.RegisterEndTime,
			Additional:        strings.Join(scopeInfo.Additional, "\n"),
			Scheduled:         scopeInfo.Scheduled,
			Interval:          scopeInfo.Interval,
			Limit:             scopeInfo.Limit,
			Status:            uint8(t.Status),
			Errors:            t.Errors,
			Total:             t.Total,
			Current:           t.Current,
			CreatedAt:         t.CreatedAt.UnixMilli(),
			UpdatedAt:         t.UpdatedAt.UnixMilli(),
		})
	}

	return &dto.GetBatchSendEmailTaskListResponse{
		Total: total,
		List:  list,
	}, nil
}
