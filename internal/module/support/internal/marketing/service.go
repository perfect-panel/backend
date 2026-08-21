// Package marketing implements the marketing subdomain of the support module
// (batch email campaigns and quota gift tasks). Only the module facade
// (internal/module/support) may reach it.
package marketing

import (
	"context"
	"fmt"
	"math"
	"net/mail"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/module/identity/entity/user"
	"github.com/perfect-panel/server/internal/module/platform/entity/task"
	"github.com/perfect-panel/server/internal/module/subscription/entity/usersub"
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
	QuerySubscribeIdsByFilter(ctx context.Context, filter *usersub.SubscribeFilter) ([]int64, error)
	CountSubscribesByFilter(ctx context.Context, filter *usersub.SubscribeFilter) (int64, error)
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
	if err := validateBatchEmailRequest(req); err != nil {
		return err
	}
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

	// 邮箱地址去重
	emails = tool.RemoveDuplicateElements(emails...)

	var additionalEmails []string
	// 追加额外的邮箱地址（不覆盖）
	if req.Additional != "" {
		additionalEmails, err = normalizeAdditionalEmails(req.Additional)
		if err != nil {
			return xerr.NewErrMsg(err.Error())
		}
	}
	if len(tool.RemoveDuplicateElements(append(emails, additionalEmails...)...)) == 0 {
		log.Errorf("[CreateBatchSendEmailTask] No email addresses provided for campaign")
		return xerr.NewErrMsg("No email addresses found for the campaign")
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
		Scheduled:         scheduledAt.Unix(),
		Interval:          req.Interval,
		Limit:             req.Limit,
	}
	scopeBytes, err := scopeInfo.Marshal()
	if err != nil {
		return errors.Wrap(err, "marshal email task scope")
	}

	taskContent := task.EmailContent{
		Subject: req.Subject,
		Content: req.Content,
	}
	contentBytes, err := taskContent.Marshal()
	if err != nil {
		return errors.Wrap(err, "marshal email task content")
	}

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
		Status:  task.StatusPending,
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
		s.markTaskEnqueueFailed(ctx, taskInfo, fmt.Sprintf("enqueue email task: %v", err))
		return xerr.NewErrCode(xerr.QueueEnqueueError)
	}
	log.Infof("[CreateBatchSendEmailTask] Successfully enqueued email task with ID: %s, scheduled at: %s", queueTaskID, scheduledAt.Format(time.DateTime))

	return nil
}

func (s *Service) GetPreSendEmailCount(ctx context.Context, req *dto.GetPreSendEmailCountRequest) (*dto.GetPreSendEmailCountResponse, error) {
	if req == nil || req.Scope < task.ScopeAll.Int8() || req.Scope > task.ScopeSkip.Int8() {
		return nil, xerr.NewErrMsg("invalid email scope")
	}
	if req.RegisterStartTime != 0 && req.RegisterEndTime != 0 && req.RegisterStartTime > req.RegisterEndTime {
		return nil, xerr.NewErrMsg("register_start_time must not be after register_end_time")
	}
	if req.RegisterStartTime < 0 || req.RegisterEndTime < 0 {
		return nil, xerr.NewErrMsg("registration timestamps must not be negative")
	}
	scope := task.ParseScopeType(req.Scope)
	emails, err := s.recipients.QueryEmailRecipients(ctx, &user.EmailRecipientFilter{
		Scope:             scope.Int8(),
		RegisterStartTime: req.RegisterStartTime,
		RegisterEndTime:   req.RegisterEndTime,
	})
	if err != nil {
		logger.WithContext(ctx).Errorf("[GetPreSendEmailCount] Count error: %v", err)
		return nil, xerr.NewErrMsg("Failed to count emails")
	}
	additional, err := normalizeAdditionalEmails(req.Additional)
	if err != nil {
		return nil, xerr.NewErrMsg(err.Error())
	}
	count := len(tool.RemoveDuplicateElements(append(emails, additional...)...))
	return &dto.GetPreSendEmailCountResponse{Count: int64(count)}, nil
}

func (s *Service) GetBatchSendEmailTaskList(ctx context.Context, req *dto.GetBatchSendEmailTaskListRequest) (*dto.GetBatchSendEmailTaskListResponse, error) {
	log := logger.WithContext(ctx)
	if req == nil {
		req = &dto.GetBatchSendEmailTaskListRequest{}
	}
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
			return nil, xerr.NewErrCode(xerr.DatabaseQueryError)
		}
		var contentInfo task.EmailContent
		if err = contentInfo.Unmarshal([]byte(t.Content)); err != nil {
			log.Errorf("[GetBatchSendEmailTaskList] failed to unmarshal email task content: %v", err.Error())
			return nil, xerr.NewErrCode(xerr.DatabaseQueryError)
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
	if req == nil || req.Id <= 0 {
		return nil, xerr.NewErrMsg("invalid task id")
	}
	taskInfo, err := s.tasks.FindOneByType(ctx, req.Id, task.TypeEmail)
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
	if req == nil || req.Id <= 0 {
		return xerr.NewErrMsg("invalid task id")
	}
	updated, err := s.tasks.UpdateStatusFrom(ctx, req.Id, task.TypeEmail, []int8{task.StatusPending, task.StatusInProgress}, task.StatusCancelled)
	if err != nil {
		logger.WithContext(ctx).Errorf("failed to stop email task, error: %v", err)
		return xerr.NewErrCode(xerr.DatabaseUpdateError)
	}
	if !updated {
		return xerr.NewErrMsg("email task is not stoppable")
	}
	if s.stopper != nil {
		s.stopper.StopBatchEmail(req.Id)
	} else {
		logger.Error("[StopBatchSendEmailTaskLogic] email worker manager is nil, cannot stop task")
	}
	return nil
}

func (s *Service) CreateQuotaTask(ctx context.Context, req *dto.CreateQuotaTaskRequest) error {
	if err := validateQuotaRequest(req); err != nil {
		return err
	}
	log := logger.WithContext(ctx)
	subIds, err := s.selector.QuerySubscribeIdsByFilter(ctx, &usersub.SubscribeFilter{
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
	scopeBytes, err := scopeInfo.Marshal()
	if err != nil {
		return errors.Wrap(err, "marshal quota task scope")
	}
	contentInfo := task.QuotaContent{
		ResetTraffic: req.ResetTraffic,
		Days:         req.Days,
		GiftType:     req.GiftType,
		GiftValue:    req.GiftValue,
	}
	contentBytes, err := contentInfo.Marshal()
	if err != nil {
		return errors.Wrap(err, "marshal quota task content")
	}

	newTask := &task.Task{
		Type:    task.TypeQuota,
		Status:  task.StatusPending,
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
		s.markTaskEnqueueFailed(ctx, newTask, fmt.Sprintf("enqueue quota task: %v", err))
		return errors.Wrapf(xerr.NewErrCode(xerr.QueueEnqueueError), "enqueue task error")
	}
	logger.Infof("[CreateQuotaTask] Successfully created task with ID: %d", newTask.Id)
	return nil
}

func (s *Service) QueryQuotaTaskList(ctx context.Context, req *dto.QueryQuotaTaskListRequest) (*dto.QueryQuotaTaskListResponse, error) {
	log := logger.WithContext(ctx)
	if req == nil {
		req = &dto.QueryQuotaTaskListRequest{}
	}
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
		return nil, xerr.NewErrCode(xerr.DatabaseQueryError)
	}

	var list []dto.QuotaTask
	for _, item := range data {
		var scopeInfo task.QuotaScope
		if err = scopeInfo.Unmarshal([]byte(item.Scope)); err != nil {
			log.Errorf("[QueryQuotaTaskList] failed to unmarshal quota task scope: %v", err.Error())
			return nil, xerr.NewErrCode(xerr.DatabaseQueryError)
		}
		var contentInfo task.QuotaContent
		if err = contentInfo.Unmarshal([]byte(item.Content)); err != nil {
			log.Errorf("[QueryQuotaTaskList] failed to unmarshal quota task content: %v", err.Error())
			return nil, xerr.NewErrCode(xerr.DatabaseQueryError)
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
	if req == nil {
		return nil, xerr.NewErrMsg("request is required")
	}
	if req.StartTime != 0 && req.EndTime != 0 && req.StartTime > req.EndTime {
		return nil, xerr.NewErrMsg("start_time must not be after end_time")
	}
	if req.StartTime < 0 || req.EndTime < 0 || !validPositiveIDs(req.Subscribers) {
		return nil, xerr.NewErrMsg("invalid quota task filter")
	}
	count, err := s.selector.CountSubscribesByFilter(ctx, &usersub.SubscribeFilter{
		Subscribers: req.Subscribers,
		IsActive:    req.IsActive,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
	})
	if err != nil {
		logger.WithContext(ctx).Errorf("[QueryQuotaTaskPreCount] count error: %v", err.Error())
		return nil, xerr.NewErrCode(xerr.DatabaseQueryError)
	}
	return &dto.QueryQuotaTaskPreCountResponse{Count: count}, nil
}

func (s *Service) QueryQuotaTaskStatus(ctx context.Context, req *dto.QueryQuotaTaskStatusRequest) (*dto.QueryQuotaTaskStatusResponse, error) {
	if req == nil || req.Id <= 0 {
		return nil, xerr.NewErrMsg("invalid task id")
	}
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

func (s *Service) markTaskEnqueueFailed(ctx context.Context, data *task.Task, reason string) {
	updateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	updated, err := s.tasks.UpdateStatusAndErrorFrom(updateCtx, data.Id, task.Type(data.Type), []int8{task.StatusPending}, task.StatusEnqueueFailed, reason)
	if err != nil {
		logger.WithContext(updateCtx).Errorf("failed to record marketing task enqueue failure: %v", err)
		return
	}
	if !updated {
		logger.WithContext(updateCtx).Errorw("marketing task enqueue failed after task execution had already started",
			logger.Field("task_id", data.Id))
		return
	}
	data.Status = task.StatusEnqueueFailed
	data.Errors = reason
}

func validateBatchEmailRequest(req *dto.CreateBatchSendEmailTaskRequest) error {
	if req == nil {
		return xerr.NewErrMsg("request is required")
	}
	if strings.TrimSpace(req.Subject) == "" || strings.TrimSpace(req.Content) == "" {
		return xerr.NewErrMsg("email subject and content are required")
	}
	if req.Scope < task.ScopeAll.Int8() || req.Scope > task.ScopeSkip.Int8() {
		return xerr.NewErrMsg("invalid email scope")
	}
	if req.RegisterStartTime != 0 && req.RegisterEndTime != 0 && req.RegisterStartTime > req.RegisterEndTime {
		return xerr.NewErrMsg("register_start_time must not be after register_end_time")
	}
	if req.RegisterStartTime < 0 || req.RegisterEndTime < 0 {
		return xerr.NewErrMsg("registration timestamps must not be negative")
	}
	if req.Scheduled < 0 {
		return xerr.NewErrMsg("scheduled must not be negative")
	}
	return nil
}

func validateQuotaRequest(req *dto.CreateQuotaTaskRequest) error {
	if req == nil {
		return xerr.NewErrMsg("request is required")
	}
	if req.StartTime != 0 && req.EndTime != 0 && req.StartTime > req.EndTime {
		return xerr.NewErrMsg("start_time must not be after end_time")
	}
	if req.StartTime < 0 || req.EndTime < 0 || !validPositiveIDs(req.Subscribers) {
		return xerr.NewErrMsg("invalid quota task filter")
	}
	if !req.ResetTraffic && req.Days == 0 && req.GiftValue == 0 {
		return xerr.NewErrMsg("at least one quota action is required")
	}
	maxInt := uint64(^uint(0) >> 1)
	if req.Days > maxInt {
		return xerr.NewErrMsg("days is too large")
	}
	if req.GiftValue == 0 {
		if req.GiftType != 0 {
			return xerr.NewErrMsg("gift_type requires a positive gift_value")
		}
		return nil
	}
	if req.GiftType != 1 && req.GiftType != 2 {
		return xerr.NewErrMsg("gift_type must be fixed or ratio when gift_value is set")
	}
	if req.GiftValue > math.MaxInt64 {
		return xerr.NewErrMsg("gift_value is too large")
	}
	return nil
}

func normalizeAdditionalEmails(raw string) ([]string, error) {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	emails := make([]string, 0, len(lines))
	for _, line := range lines {
		address := strings.TrimSpace(line)
		if address == "" {
			continue
		}
		parsed, err := mail.ParseAddress(address)
		if err != nil || !strings.EqualFold(parsed.Address, address) {
			return nil, fmt.Errorf("invalid additional email address: %s", address)
		}
		emails = append(emails, strings.ToLower(parsed.Address))
	}
	return tool.RemoveDuplicateElements(emails...), nil
}

func validPositiveIDs(ids []int64) bool {
	for _, id := range ids {
		if id <= 0 {
			return false
		}
	}
	return true
}
