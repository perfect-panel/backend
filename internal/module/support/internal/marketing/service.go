// Package marketing implements the marketing subdomain of the support module
// (batch email campaigns and quota gift tasks). Only the module facade
// (internal/module/support) may reach it.
package marketing

import (
	"context"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/task"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

// EmailRecipientReader is the marketing subdomain's port onto the identity
// domain; the legacy user repository satisfies it structurally.
type EmailRecipientReader interface {
	QueryEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) ([]string, error)
	CountEmailRecipients(ctx context.Context, filter *user.EmailRecipientFilter) (int64, error)
}

// SubscriptionSelector is the port onto the subscription domain for selecting
// quota-task targets; the legacy user-subscription repository satisfies it
// structurally.
type SubscriptionSelector interface {
	QuerySubscribeIdsByFilter(ctx context.Context, filter *user.SubscribeFilter) ([]int64, error)
	CountSubscribesByFilter(ctx context.Context, filter *user.SubscribeFilter) (int64, error)
}

// Queue schedules the asynchronous execution of marketing tasks. The
// composition root adapts the asynq client so queue task types stay out of
// the module.
type Queue interface {
	EnqueueBatchEmail(ctx context.Context, taskID int64, processAt time.Time) (queueTaskID string, err error)
	EnqueueQuota(ctx context.Context, taskID int64) error
}

// BatchEmailStopper aborts a running batch-email worker, if any.
type BatchEmailStopper interface {
	StopBatchEmail(taskID int64)
}

type Service struct {
	tasks      repository.TaskRepo
	recipients EmailRecipientReader
	selector   SubscriptionSelector
	queue      Queue
	stopper    BatchEmailStopper
}

func NewService(tasks repository.TaskRepo, recipients EmailRecipientReader, selector SubscriptionSelector, queue Queue, stopper BatchEmailStopper) *Service {
	return &Service{tasks: tasks, recipients: recipients, selector: selector, queue: queue, stopper: stopper}
}

func (s *Service) CreateBatchSendEmailTask(ctx context.Context, req *dto.CreateBatchSendEmailTaskRequest) error {
	log := logger.WithContext(ctx)
	scope := task.ParseScopeType(req.Scope)
	emails, err := s.recipients.QueryEmailRecipients(ctx, &user.EmailRecipientFilter{
		Scope:             scope.Int8(),
		RegisterStartTime: req.RegisterStartTime,
		RegisterEndTime:   req.RegisterEndTime,
	})
	if err != nil {
		log.Errorf("[CreateBatchSendEmailTask] Failed to fetch email addresses: %v", err.Error())
		return xerr.NewErrCode(xerr.DatabaseQueryError)
	}

	// 邮箱列表为空，返回错误
	if len(emails) == 0 && scope != task.ScopeSkip {
		log.Errorf("[CreateBatchSendEmailTask] No email addresses found for the specified scope")
		return xerr.NewErrMsg("No email addresses found for the specified scope")
	}

	// 邮箱地址去重
	emails = tool.RemoveDuplicateElements(emails...)

	var additionalEmails []string
	// 追加额外的邮箱地址（不覆盖）
	if req.Additional != "" {
		additionalEmails = tool.RemoveDuplicateElements(strings.Split(req.Additional, "\n")...)
	}
	if len(additionalEmails) == 0 && scope == task.ScopeSkip {
		log.Errorf("[CreateBatchSendEmailTask] No additional email addresses provided for skip scope")
		return xerr.NewErrMsg("No additional email addresses provided for skip scope")
	}

	scheduledAt := timeutil.Now().Add(10 * time.Second) // 默认延迟10秒执行,防止任务创建和执行时间过于接近
	if req.Scheduled != 0 {
		scheduledAt = time.Unix(req.Scheduled, 0)
		if scheduledAt.Before(timeutil.Now()) {
			scheduledAt = timeutil.Now()
		}
	}

	scopeInfo := task.EmailScope{
		Type:              scope.Int8(),
		RegisterStartTime: req.RegisterStartTime,
		RegisterEndTime:   req.RegisterEndTime,
		Recipients:        emails,
		Additional:        additionalEmails,
		Scheduled:         req.Scheduled,
		Interval:          req.Interval,
		Limit:             req.Limit,
	}
	scopeBytes, _ := scopeInfo.Marshal()

	taskContent := task.EmailContent{
		Subject: req.Subject,
		Content: req.Content,
	}
	contentBytes, _ := taskContent.Marshal()

	var total uint64
	if additionalEmails != nil {
		list := append(emails, additionalEmails...)
		total = uint64(len(tool.RemoveDuplicateElements(list...)))
	} else {
		total = uint64(len(emails))
	}

	taskInfo := &task.Task{
		Type:    task.TypeEmail,
		Scope:   string(scopeBytes),
		Content: string(contentBytes),
		Status:  0,
		Errors:  "",
		Total:   total,
		Current: 0,
	}

	if err = s.tasks.Insert(ctx, taskInfo); err != nil {
		log.Errorf("[CreateBatchSendEmailTask] Failed to create email task: %v", err.Error())
		return xerr.NewErrCode(xerr.DatabaseInsertError)
	}
	log.Infof("[CreateBatchSendEmailTask] Successfully created email task with ID: %d", taskInfo.Id)

	queueTaskID, err := s.queue.EnqueueBatchEmail(ctx, taskInfo.Id, scheduledAt)
	if err != nil {
		log.Errorf("[CreateBatchSendEmailTask] Failed to enqueue email task: %v", err.Error())
		return xerr.NewErrCode(xerr.QueueEnqueueError)
	}
	log.Infof("[CreateBatchSendEmailTask] Successfully enqueued email task with ID: %s, scheduled at: %s", queueTaskID, scheduledAt.Format(time.DateTime))

	return nil
}

func (s *Service) GetPreSendEmailCount(ctx context.Context, req *dto.GetPreSendEmailCountRequest) (*dto.GetPreSendEmailCountResponse, error) {
	scope := task.ParseScopeType(req.Scope)
	count, err := s.recipients.CountEmailRecipients(ctx, &user.EmailRecipientFilter{
		Scope:             scope.Int8(),
		RegisterStartTime: req.RegisterStartTime,
		RegisterEndTime:   req.RegisterEndTime,
	})
	if err != nil {
		logger.WithContext(ctx).Errorf("[GetPreSendEmailCount] Count error: %v", err)
		return nil, xerr.NewErrMsg("Failed to count emails")
	}
	return &dto.GetPreSendEmailCountResponse{Count: count}, nil
}

func (s *Service) GetBatchSendEmailTaskList(ctx context.Context, req *dto.GetBatchSendEmailTaskListRequest) (*dto.GetBatchSendEmailTaskListResponse, error) {
	log := logger.WithContext(ctx)
	if req.Page == 0 {
		req.Page = 1
	}
	if req.Size == 0 {
		req.Size = 10
	}
	total, tasks, err := s.tasks.QueryTaskList(ctx, &task.Filter{
		Type:   task.TypeEmail,
		Page:   req.Page,
		Size:   req.Size,
		Status: req.Status,
		Scope:  req.Scope,
	})
	if err != nil {
		log.Errorf("failed to get email tasks: %v", err)
		return nil, xerr.NewErrCode(xerr.DatabaseQueryError)
	}

	list := make([]dto.BatchSendEmailTask, 0)
	for _, t := range tasks {
		var scopeInfo task.EmailScope
		if err = scopeInfo.Unmarshal([]byte(t.Scope)); err != nil {
			log.Errorf("[GetBatchSendEmailTaskList] failed to unmarshal email task scope: %v", err.Error())
			continue
		}
		var contentInfo task.EmailContent
		if err = contentInfo.Unmarshal([]byte(t.Content)); err != nil {
			log.Errorf("[GetBatchSendEmailTaskList] failed to unmarshal email task content: %v", err.Error())
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

	return &dto.GetBatchSendEmailTaskListResponse{Total: total, List: list}, nil
}

func (s *Service) GetBatchSendEmailTaskStatus(ctx context.Context, req *dto.GetBatchSendEmailTaskStatusRequest) (*dto.GetBatchSendEmailTaskStatusResponse, error) {
	taskInfo, err := s.tasks.FindOne(ctx, req.Id)
	if err != nil {
		logger.WithContext(ctx).Errorf("failed to get email task status, error: %v", err)
		return nil, xerr.NewErrCode(xerr.DatabaseQueryError)
	}
	return &dto.GetBatchSendEmailTaskStatusResponse{
		Status:  uint8(taskInfo.Status),
		Total:   int64(taskInfo.Total),
		Current: int64(taskInfo.Current),
		Errors:  taskInfo.Errors,
	}, nil
}

func (s *Service) StopBatchSendEmailTask(ctx context.Context, req *dto.StopBatchSendEmailTaskRequest) error {
	if s.stopper != nil {
		s.stopper.StopBatchEmail(req.Id)
	} else {
		logger.Error("[StopBatchSendEmailTaskLogic] email worker manager is nil, cannot stop task")
	}
	if err := s.tasks.UpdateStatus(ctx, req.Id, 2); err != nil {
		logger.WithContext(ctx).Errorf("failed to stop email task, error: %v", err)
		return xerr.NewErrCode(xerr.DatabaseUpdateError)
	}
	return nil
}

func (s *Service) CreateQuotaTask(ctx context.Context, req *dto.CreateQuotaTaskRequest) error {
	log := logger.WithContext(ctx)
	subIds, err := s.selector.QuerySubscribeIdsByFilter(ctx, &user.SubscribeFilter{
		Subscribers: req.Subscribers,
		IsActive:    req.IsActive,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
	})
	if err != nil {
		log.Errorf("[CreateQuotaTask] find subscribers error: %v", err.Error())
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

	newTask := &task.Task{
		Type:    task.TypeQuota,
		Status:  0,
		Scope:   string(scopeBytes),
		Content: string(contentBytes),
		Total:   uint64(len(subIds)),
		Current: 0,
		Errors:  "",
	}

	if err := s.tasks.Insert(ctx, newTask); err != nil {
		log.Errorf("[CreateQuotaTask] create task error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create task error")
	}

	if err := s.queue.EnqueueQuota(ctx, newTask.Id); err != nil {
		log.Errorf("[CreateQuotaTask] enqueue task error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.QueueEnqueueError), "enqueue task error")
	}
	logger.Infof("[CreateQuotaTask] Successfully created task with ID: %d", newTask.Id)
	return nil
}

func (s *Service) QueryQuotaTaskList(ctx context.Context, req *dto.QueryQuotaTaskListRequest) (*dto.QueryQuotaTaskListResponse, error) {
	log := logger.WithContext(ctx)
	if req.Page == 0 {
		req.Page = 1
	}
	if req.Size == 0 {
		req.Size = 20
	}

	count, data, err := s.tasks.QueryTaskList(ctx, &task.Filter{
		Type:   task.TypeQuota,
		Page:   req.Page,
		Size:   req.Size,
		Status: req.Status,
	})
	if err != nil {
		log.Errorf("[QueryQuotaTaskList] failed to get quota tasks: %v", err)
		return nil, err
	}

	var list []dto.QuotaTask
	for _, item := range data {
		var scopeInfo task.QuotaScope
		if err = scopeInfo.Unmarshal([]byte(item.Scope)); err != nil {
			log.Errorf("[QueryQuotaTaskList] failed to unmarshal quota task scope: %v", err.Error())
			continue
		}
		var contentInfo task.QuotaContent
		if err = contentInfo.Unmarshal([]byte(item.Content)); err != nil {
			log.Errorf("[QueryQuotaTaskList] failed to unmarshal quota task content: %v", err.Error())
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

	return &dto.QueryQuotaTaskListResponse{Total: count, List: list}, nil
}

func (s *Service) QueryQuotaTaskPreCount(ctx context.Context, req *dto.QueryQuotaTaskPreCountRequest) (*dto.QueryQuotaTaskPreCountResponse, error) {
	count, err := s.selector.CountSubscribesByFilter(ctx, &user.SubscribeFilter{
		Subscribers: req.Subscribers,
		IsActive:    req.IsActive,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
	})
	if err != nil {
		logger.WithContext(ctx).Errorf("[QueryQuotaTaskPreCount] count error: %v", err.Error())
		return nil, err
	}
	return &dto.QueryQuotaTaskPreCountResponse{Count: count}, nil
}

func (s *Service) QueryQuotaTaskStatus(ctx context.Context, req *dto.QueryQuotaTaskStatusRequest) (*dto.QueryQuotaTaskStatusResponse, error) {
	data, err := s.tasks.FindOneByType(ctx, req.Id, task.TypeQuota)
	if err != nil {
		logger.WithContext(ctx).Errorf("[QueryQuotaTaskStatus] failed to get quota task: %v", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), " failed to get quota task: %v", err.Error())
	}
	return &dto.QueryQuotaTaskStatusResponse{
		Status:  uint8(data.Status),
		Current: int64(data.Current),
		Total:   int64(data.Total),
		Errors:  data.Errors,
	}, nil
}
