package repo

import (
	"context"
	"fmt"

	"github.com/perfect-panel/backend/internal/repository/kernel"

	"github.com/perfect-panel/backend/internal/module/platform/entity/task"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ kernel.TaskRepo = (*taskRepo)(nil)

const taskPageSelect = "task.*, COUNT(*) OVER() AS total_count"

type taskRepo struct {
	db *gorm.DB
}

type taskPageRow struct {
	task.Task
	TotalCount int64 `gorm:"column:total_count"`
}

// NewTaskRepo builds the module-owned implementation.
func NewTaskRepo(db *gorm.DB) kernel.TaskRepo {
	return &taskRepo{
		db: db,
	}
}

func (m *taskRepo) Insert(ctx context.Context, data *task.Task) error {
	return m.db.WithContext(ctx).Create(data).Error
}

func (m *taskRepo) FindOne(ctx context.Context, id int64) (*task.Task, error) {
	var data task.Task
	err := m.db.WithContext(ctx).Model(&task.Task{}).Where("id = ?", id).First(&data).Error
	return &data, err
}

func (m *taskRepo) FindOneByType(ctx context.Context, id int64, typ task.Type) (*task.Task, error) {
	var data task.Task
	err := m.db.WithContext(ctx).Model(&task.Task{}).Where("id = ? AND type = ?", id, typ).First(&data).Error
	return &data, err
}

func (m *taskRepo) QueryTaskList(ctx context.Context, filter *task.Filter) (int64, []*task.Task, error) {
	if filter == nil {
		filter = &task.Filter{
			Type: task.Undefined,
			Page: 1,
			Size: kernel.DefaultPageSize,
		}
	}
	filter.Page, filter.Size = kernel.NormalizePage(filter.Page, filter.Size)

	query := m.taskListQuery(ctx, filter)
	var rows []taskPageRow
	err := query.Select(taskPageSelect).
		Offset((filter.Page - 1) * filter.Size).
		Limit(filter.Size).
		Order("created_at DESC, id DESC").
		Scan(&rows).Error
	if err != nil {
		return 0, nil, err
	}
	if len(rows) == 0 {
		// A window aggregate has no row on an out-of-range page. Preserve the
		// existing API contract by counting only in this uncommon case.
		var total int64
		if err := m.taskListQuery(ctx, filter).Count(&total).Error; err != nil {
			return 0, nil, err
		}
		return total, []*task.Task{}, nil
	}

	data := make([]*task.Task, 0, len(rows))
	for i := range rows {
		data = append(data, &rows[i].Task)
	}
	return rows[0].TotalCount, data, nil
}

func (m *taskRepo) taskListQuery(ctx context.Context, filter *task.Filter) *gorm.DB {
	query := m.db.WithContext(ctx).Model(&task.Task{})
	if filter.Type != task.Undefined {
		query = query.Where("type = ?", filter.Type)
	}
	if filter.Status != nil {
		query = query.Where("status = ?", *filter.Status)
	}
	if filter.Scope != nil {
		// Keep JSON filtering in the database. The previous dialect-neutral Go
		// fallback transferred and decoded the entire task history before paging.
		switch m.db.Dialector.Name() {
		case "mysql":
			// MySQL 8 and MariaDB 11.8 index this virtual generated column.
			query = query.Where("scope_type = ?", *filter.Scope)
		case "postgres":
			query = query.Where("CAST(scope AS jsonb) ->> 'Type' = ?", fmt.Sprint(*filter.Scope))
		default:
			query = query.Where("CAST(json_extract(scope, '$.Type') AS INTEGER) = ?", *filter.Scope)
		}
	}
	return query
}

// Update rewrites the task's mutable columns from data; the row's type and
// creation time stay as they were, and a missing row is not inserted, which
// a whole-row save would do.
func (m *taskRepo) Update(ctx context.Context, data *task.Task) error {
	return m.db.WithContext(ctx).Model(&task.Task{}).Where("id = ?", data.Id).
		Select("scope", "content", "status", "errors", "total", "current", "daily_date", "daily_sent").
		Updates(data).Error
}

func (m *taskRepo) UpdateActive(ctx context.Context, data *task.Task) (bool, error) {
	result := m.db.WithContext(ctx).Model(&task.Task{}).
		Where("id = ? AND type = ? AND status IN ?", data.Id, data.Type, []int8{task.StatusPending, task.StatusInProgress}).
		Updates(map[string]any{
			"scope": data.Scope, "content": data.Content, "status": data.Status,
			"errors": data.Errors, "total": data.Total, "current": data.Current,
		})
	return result.RowsAffected == 1, result.Error
}

// UpdateActiveProgress updates only the bounded, mutable execution state. Task
// scope and content can contain very large target lists and templates; rewriting
// them for every processed target creates quadratic write amplification.
func (m *taskRepo) UpdateActiveProgress(ctx context.Context, data *task.Task) (bool, error) {
	result := m.db.WithContext(ctx).Model(&task.Task{}).
		Where("id = ? AND type = ? AND status IN ?", data.Id, data.Type, []int8{task.StatusPending, task.StatusInProgress}).
		Updates(activeProgressUpdates(data))
	return result.RowsAffected == 1, result.Error
}

func activeProgressUpdates(data *task.Task) map[string]any {
	return map[string]any{
		"status": data.Status, "errors": data.Errors, "total": data.Total, "current": data.Current,
		"daily_date": data.DailyDate, "daily_sent": data.DailySent,
	}
}

// UpdateActiveProgressWithError commits the failed-target marker and its
// progress cursor together. A transient DB failure can therefore never make a
// retry inherit a failure row for an attempt whose cursor did not advance.
func (m *taskRepo) UpdateActiveProgressWithError(ctx context.Context, data *task.Task, taskError *task.TaskError) (bool, error) {
	updated := false
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&task.Task{}).
			Where("id = ? AND type = ? AND status IN ?", data.Id, data.Type, []int8{task.StatusPending, task.StatusInProgress}).
			Updates(activeProgressUpdates(data))
		if result.Error != nil || result.RowsAffected != 1 {
			return result.Error
		}
		updated = true
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "task_id"}, {Name: "position"}},
			DoNothing: true,
		}).Create(taskError).Error
	})
	return updated, err
}

func (m *taskRepo) UpdateStatus(ctx context.Context, id int64, status int8) error {
	return m.db.WithContext(ctx).Model(&task.Task{}).Where("id = ?", id).Update("status", status).Error
}

func (m *taskRepo) UpdateStatusFrom(ctx context.Context, id int64, typ task.Type, from []int8, status int8) (bool, error) {
	result := m.db.WithContext(ctx).Model(&task.Task{}).
		Where("id = ? AND type = ? AND status IN ?", id, typ, from).
		Update("status", status)
	return result.RowsAffected == 1, result.Error
}

func (m *taskRepo) UpdateStatusAndErrorFrom(ctx context.Context, id int64, typ task.Type, from []int8, status int8, taskError string) (bool, error) {
	result := m.db.WithContext(ctx).Model(&task.Task{}).
		Where("id = ? AND type = ? AND status IN ?", id, typ, from).
		Updates(map[string]any{"status": status, "errors": taskError})
	return result.RowsAffected == 1, result.Error
}

func (m *taskRepo) InsertError(ctx context.Context, data *task.TaskError) error {
	return m.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "task_id"}, {Name: "position"}},
		DoNothing: true,
	}).Create(data).Error
}

func (m *taskRepo) FindErrors(ctx context.Context, taskIDs []int64) ([]*task.TaskError, error) {
	if len(taskIDs) == 0 {
		return []*task.TaskError{}, nil
	}
	var data []*task.TaskError
	err := m.db.WithContext(ctx).Where("task_id IN ?", taskIDs).
		Order("task_id ASC, position ASC").Find(&data).Error
	return data, err
}
