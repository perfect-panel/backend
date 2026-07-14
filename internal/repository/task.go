package repository

import (
	"context"

	"github.com/perfect-panel/server/internal/model/entity/task"
	"gorm.io/gorm"
)

// TaskRepo task 数据访问接口
type TaskRepo interface {
	Insert(ctx context.Context, data *task.Task) error
	FindOne(ctx context.Context, id int64) (*task.Task, error)
	FindOneByType(ctx context.Context, id int64, typ task.Type) (*task.Task, error)
	QueryTaskList(ctx context.Context, filter *task.Filter) (int64, []*task.Task, error)
	Update(ctx context.Context, data *task.Task) error
	UpdateStatus(ctx context.Context, id int64, status int8) error
}

var _ TaskRepo = (*taskRepo)(nil)

type taskRepo struct {
	db *gorm.DB
}

func newTaskRepo(db *gorm.DB) TaskRepo {
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
	var total int64
	var data []*task.Task
	if filter == nil {
		filter = &task.Filter{
			Type: task.Undefined,
			Page: 1,
			Size: defaultPageSize,
		}
	}
	filter.Page, filter.Size = normalizePage(filter.Page, filter.Size)

	query := m.db.WithContext(ctx).Model(&task.Task{})
	if filter.Type != task.Undefined {
		query = query.Where("type = ?", filter.Type)
	}
	if filter.Status != nil {
		query = query.Where("status = ?", *filter.Status)
	}
	if filter.Scope != nil {
		var all []*task.Task
		if err := query.Order("created_at DESC").Find(&all).Error; err != nil {
			return 0, nil, err
		}

		// Scope is stored as JSON text; filter here to keep the query dialect-neutral.
		filtered := make([]*task.Task, 0, len(all))
		for _, item := range all {
			var scope task.EmailScope
			if err := scope.Unmarshal([]byte(item.Scope)); err != nil {
				continue
			}
			if scope.Type == *filter.Scope {
				filtered = append(filtered, item)
			}
		}

		total = int64(len(filtered))
		start := (filter.Page - 1) * filter.Size
		if start >= len(filtered) {
			return total, []*task.Task{}, nil
		}
		end := start + filter.Size
		if end > len(filtered) {
			end = len(filtered)
		}
		return total, filtered[start:end], nil
	}

	err := query.Count(&total).
		Offset((filter.Page - 1) * filter.Size).
		Limit(filter.Size).
		Order("created_at DESC").
		Find(&data).Error
	return total, data, err
}

func (m *taskRepo) Update(ctx context.Context, data *task.Task) error {
	return m.db.WithContext(ctx).Where("id = ?", data.Id).Save(data).Error
}

func (m *taskRepo) UpdateStatus(ctx context.Context, id int64, status int8) error {
	return m.db.WithContext(ctx).Model(&task.Task{}).Where("id = ?", id).Update("status", status).Error
}
