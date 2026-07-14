package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/pkg/orm"
	"gorm.io/gorm"
)

// LogRepo log 数据访问接口
type LogRepo interface {
	Insert(ctx context.Context, data *log.SystemLog) error
	FindOne(ctx context.Context, id int64) (*log.SystemLog, error)
	Update(ctx context.Context, data *log.SystemLog) error
	Delete(ctx context.Context, id int64) error
	FilterSystemLog(ctx context.Context, filter *log.FilterParams) ([]*log.SystemLog, int64, error)
	FindFirstByDateType(ctx context.Context, date string, typ uint8) (*log.SystemLog, error)
	FindByDatesType(ctx context.Context, dates []string, typ uint8) ([]*log.SystemLog, error)
	DeleteBefore(ctx context.Context, end time.Time) error
	SumAmountByTypeAndObjectID(ctx context.Context, typ uint8, objectID int64) (int64, error)
}

var _ LogRepo = (*logRepo)(nil)

type logRepo struct {
	*gorm.DB
}

func newLogRepo(db *gorm.DB) LogRepo {
	return &logRepo{
		DB: db,
	}
}

func (m *logRepo) Insert(ctx context.Context, data *log.SystemLog) error {
	return m.WithContext(ctx).Create(data).Error
}

func (m *logRepo) FindOne(ctx context.Context, id int64) (*log.SystemLog, error) {
	var data log.SystemLog
	err := m.WithContext(ctx).Where("id = ?", id).First(&data).Error
	if err != nil {
		return nil, err
	}
	return &data, nil
}

func (m *logRepo) Update(ctx context.Context, data *log.SystemLog) error {
	return m.WithContext(ctx).Where("id = ?", data.Id).Save(data).Error
}

func (m *logRepo) Delete(ctx context.Context, id int64) error {
	return m.WithContext(ctx).Where("id = ?", id).Delete(&log.SystemLog{}).Error
}

// FilterSystemLog filter system logs with pagination
func (m *logRepo) FilterSystemLog(ctx context.Context, filter *log.FilterParams) ([]*log.SystemLog, int64, error) {
	tx := m.WithContext(ctx).Model(&log.SystemLog{}).Order("id DESC")
	if filter == nil {
		filter = &log.FilterParams{
			Page: 1,
			Size: defaultPageSize,
		}
	}

	filter.Page, filter.Size = normalizePage(filter.Page, filter.Size)

	if filter.Type != 0 {
		tx = tx.Where("type = ?", filter.Type)
	}

	if filter.Data != "" {
		tx = tx.Where("date = ?", filter.Data)
	}

	if filter.ObjectID != 0 {
		tx = tx.Where("object_id = ?", filter.ObjectID)
	}
	if filter.Search != "" {
		tx = tx.Scopes(orm.ContainsLike([]string{"content"}, filter.Search))
	}

	var total int64
	var logs []*log.SystemLog
	if !filter.SkipCount {
		if err := tx.Count(&total).Error; err != nil {
			return nil, 0, err
		}
	}
	err := tx.Limit(filter.Size).Offset((filter.Page - 1) * filter.Size).Find(&logs).Error
	return logs, total, err
}

// FindFirstByDateType find first system log by date and type
func (m *logRepo) FindFirstByDateType(ctx context.Context, date string, typ uint8) (*log.SystemLog, error) {
	var data log.SystemLog
	err := m.WithContext(ctx).Model(&log.SystemLog{}).Where("date = ? AND type = ?", date, typ).First(&data).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &data, nil
}

// FindByDatesType find system logs by dates and type
func (m *logRepo) FindByDatesType(ctx context.Context, dates []string, typ uint8) ([]*log.SystemLog, error) {
	var data []*log.SystemLog
	if len(dates) == 0 {
		return data, nil
	}
	err := m.WithContext(ctx).Model(&log.SystemLog{}).Where("date IN ? AND type = ?", dates, typ).Find(&data).Error
	return data, err
}

// DeleteBefore deletes system logs whose date is before the given end date.
func (m *logRepo) DeleteBefore(ctx context.Context, end time.Time) error {
	return m.WithContext(ctx).
		Where("date < ?", end.Format(time.DateOnly)).
		Delete(&log.SystemLog{}).Error
}

// SumAmountByTypeAndObjectID returns the sum of the "amount" field extracted from JSON content
// for all system logs matching the given type and object ID.
func (m *logRepo) SumAmountByTypeAndObjectID(ctx context.Context, typ uint8, objectID int64) (int64, error) {
	jsonExtract := jsonAmountExpr(m.DB)
	var sum int64
	err := m.WithContext(ctx).
		Model(&log.SystemLog{}).
		Select(fmt.Sprintf("COALESCE(SUM(%s), 0)", jsonExtract)).
		Where("type = ? AND object_id = ?", typ, objectID).
		Scan(&sum).Error
	return sum, err
}

func jsonAmountExpr(db *gorm.DB) string {
	if db != nil && db.Dialector.Name() == orm.DriverPostgres {
		return "(content::json->>'amount')::bigint"
	}
	return "CAST(JSON_EXTRACT(content, '$.amount') AS SIGNED)"
}
