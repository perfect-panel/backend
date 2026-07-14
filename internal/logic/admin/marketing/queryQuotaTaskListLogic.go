package marketing

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/task"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type QueryQuotaTaskListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryQuotaTaskListLogic Query quota task list
func NewQueryQuotaTaskListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryQuotaTaskListLogic {
	return &QueryQuotaTaskListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryQuotaTaskListLogic) QueryQuotaTaskList(req *dto.QueryQuotaTaskListRequest) (resp *dto.QueryQuotaTaskListResponse, err error) {
	if req.Page == 0 {
		req.Page = 1
	}
	if req.Size == 0 {
		req.Size = 20
	}

	count, data, err := l.svcCtx.Store.Task().QueryTaskList(l.ctx, &task.Filter{
		Type:   task.TypeQuota,
		Page:   req.Page,
		Size:   req.Size,
		Status: req.Status,
	})
	if err != nil {
		l.Errorf("[QueryQuotaTaskList] failed to get quota tasks: %v", err)
		return nil, err
	}

	var list []dto.QuotaTask
	for _, item := range data {
		var scopeInfo task.QuotaScope
		if err = scopeInfo.Unmarshal([]byte(item.Scope)); err != nil {
			l.Errorf("[QueryQuotaTaskList] failed to unmarshal quota task scope: %v", err.Error())
			continue
		}
		var contentInfo task.QuotaContent
		if err = contentInfo.Unmarshal([]byte(item.Content)); err != nil {
			l.Errorf("[QueryQuotaTaskList] failed to unmarshal quota task content: %v", err.Error())
			continue
		}
		list = append(list, dto.QuotaTask{
			Id:           item.Id,
			Subscribers:  scopeInfo.Subscribers,
			IsActive:     scopeInfo.IsActive,
			StartTime:    scopeInfo.StartTime,
			EndTime:      scopeInfo.EndTime,
			ResetTraffic: contentInfo.ResetTraffic,
			Days:         contentInfo.Days,
			GiftType:     contentInfo.GiftType,
			GiftValue:    contentInfo.GiftValue,
			Objects:      scopeInfo.Objects,
			Status:       uint8(item.Status),
			Total:        int64(item.Total),
			Current:      int64(item.Current),
			Errors:       item.Errors,
			CreatedAt:    item.CreatedAt.UnixMilli(),
			UpdatedAt:    item.UpdatedAt.UnixMilli(),
		})
	}

	return &dto.QueryQuotaTaskListResponse{
		Total: count,
		List:  list,
	}, nil
}
