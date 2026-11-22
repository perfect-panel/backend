package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/perfect-panel/backend/internal/repository"

	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/pkg/orm"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ repository.TrafficRepo = (*trafficRepo)(nil)

const trafficLogNewestFirst = "timestamp DESC, id DESC"

type trafficRepo struct {
	Conn  *gorm.DB
	table string
}

// NewTrafficRepo builds the module-owned implementation.
func NewTrafficRepo(db *gorm.DB) repository.TrafficRepo {
	return &trafficRepo{
		Conn:  db,
		table: "traffic",
	}
}

func (m *trafficRepo) InsertBatch(ctx context.Context, data []*traffic.TrafficLog, batchSize int) error {
	if len(data) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 1000
	}
	return m.Conn.WithContext(ctx).CreateInBatches(data, batchSize).Error
}

func (m *trafficRepo) QueryTrafficSummary(ctx context.Context, start, end time.Time) (*traffic.TotalTraffic, error) {
	var data traffic.TotalTraffic
	err := m.Conn.WithContext(ctx).Model(&traffic.TrafficLog{}).
		Select(totalTrafficSelect(m.Conn)).
		Where(trafficTimeRangeCondition(m.Conn), start, end).
		Scan(&data).Error
	return &data, err
}

func (m *trafficRepo) TopServersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]traffic.ServerTrafficRanking, error) {
	var summaries []traffic.ServerTrafficRanking
	start, end := trafficDayRange(date)
	err := m.Conn.WithContext(ctx).Model(&traffic.TrafficLog{}).
		Select(serverTrafficRankingSelect(m.Conn)).
		Where(trafficTimeRangeCondition(m.Conn), start, end).
		Group(trafficColumn(m.Conn, "server_id")).
		Order("total DESC").
		Limit(limit).
		Scan(&summaries).Error
	return summaries, err
}

func (m *trafficRepo) TopUsersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]traffic.UserTrafficRanking, error) {
	var summaries []traffic.UserTrafficRanking
	start, end := trafficDayRange(date)
	err := m.Conn.WithContext(ctx).Model(&traffic.TrafficLog{}).
		Select(userTrafficRankingSelect(m.Conn)).
		Where(trafficTimeRangeCondition(m.Conn), start, end).
		Group(trafficColumn(m.Conn, "user_id") + ", " + trafficColumn(m.Conn, "subscribe_id")).
		Order("total DESC").
		Limit(limit).
		Scan(&summaries).Error
	return summaries, err
}

func (m *trafficRepo) QueryServerTrafficRanking(ctx context.Context, start, end time.Time) ([]traffic.ServerTrafficRanking, error) {
	var summaries []traffic.ServerTrafficRanking
	err := m.Conn.WithContext(ctx).Model(&traffic.TrafficLog{}).
		Select(serverTrafficRankingSelect(m.Conn)).
		Where(trafficTimeRangeCondition(m.Conn), start, end).
		Group(trafficColumn(m.Conn, "server_id")).
		Order("total DESC").
		Scan(&summaries).Error
	return summaries, err
}

func (m *trafficRepo) QueryUserTrafficRanking(ctx context.Context, start, end time.Time) ([]traffic.UserTrafficRanking, error) {
	var summaries []traffic.UserTrafficRanking
	err := m.Conn.WithContext(ctx).Model(&traffic.TrafficLog{}).
		Select(userTrafficRankingSelect(m.Conn)).
		Where(trafficTimeRangeCondition(m.Conn), start, end).
		Group(trafficColumn(m.Conn, "user_id") + ", " + trafficColumn(m.Conn, "subscribe_id")).
		Order("total DESC").
		Scan(&summaries).Error
	return summaries, err
}

// QueryTrafficLogPageList returns a list of records that meet the conditions.
func (m *trafficRepo) QueryTrafficLogPageList(ctx context.Context, userId, subscribeId int64, page, size int) ([]*traffic.TrafficLog, int64, error) {
	var list []*traffic.TrafficLog
	var total int64
	page, size = repository.NormalizePage(page, size)
	query := m.Conn.WithContext(ctx).Model(&traffic.TrafficLog{}).
		Where("user_id = ? AND subscribe_id = ?", userId, subscribeId)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order(trafficLogNewestFirst).
		Limit(size).
		Offset((page - 1) * size).
		Find(&list).Error
	return list, total, err
}

func (m *trafficRepo) QueryTrafficLogDetails(ctx context.Context, filter *traffic.TrafficLogDetailsFilter) ([]*traffic.TrafficLog, int64, error) {
	if filter == nil {
		filter = &traffic.TrafficLogDetailsFilter{Page: 1, Size: 10}
	}
	filter.Page, filter.Size = repository.NormalizePage(filter.Page, filter.Size)

	query := m.Conn.WithContext(ctx).Model(&traffic.TrafficLog{})
	if filter.ServerId != 0 {
		query = query.Where("server_id = ?", filter.ServerId)
	}
	if !filter.Start.IsZero() {
		query = query.Where(trafficColumn(m.Conn, "timestamp")+" >= ?", filter.Start)
	}
	if !filter.End.IsZero() {
		query = query.Where(trafficColumn(m.Conn, "timestamp")+" < ?", filter.End)
	}
	if filter.UserId != 0 {
		query = query.Where("user_id = ?", filter.UserId)
	}
	if filter.SubscribeId != 0 {
		query = query.Where("subscribe_id = ?", filter.SubscribeId)
	}

	var list []*traffic.TrafficLog
	var total int64
	err := query.Count(&total).
		Order(trafficLogNewestFirst).
		Limit(filter.Size).
		Offset((filter.Page - 1) * filter.Size).
		Find(&list).Error
	return list, total, err
}

func (m *trafficRepo) DeleteBeforeBatch(ctx context.Context, end time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}
	var ids []int64
	if err := m.Conn.WithContext(ctx).Model(&traffic.TrafficLog{}).
		Select("id").Where(trafficColumn(m.Conn, "timestamp")+" <= ?", end).
		Order("id ASC").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := m.Conn.WithContext(ctx).Where("id IN ?", ids).Delete(&traffic.TrafficLog{})
	return result.RowsAffected, result.Error
}

func trafficDayRange(date time.Time) (time.Time, time.Time) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	return start, start.Add(24 * time.Hour)
}

func trafficTimeRangeCondition(db *gorm.DB) string {
	column := trafficColumn(db, "timestamp")
	return column + " >= ? AND " + column + " < ?"
}

func totalTrafficSelect(db *gorm.DB) string {
	return trafficSumIntExpr(db, trafficColumn(db, "download"), "download") + ", " +
		trafficSumIntExpr(db, trafficColumn(db, "upload"), "upload")
}

func serverTrafficRankingSelect(db *gorm.DB) string {
	download := trafficColumn(db, "download")
	upload := trafficColumn(db, "upload")
	return fmt.Sprintf(
		"%s AS server_id, %s, %s, %s",
		trafficColumn(db, "server_id"),
		trafficSumIntExpr(db, download+" + "+upload, "total"),
		trafficSumIntExpr(db, download, "download"),
		trafficSumIntExpr(db, upload, "upload"),
	)
}

func userTrafficRankingSelect(db *gorm.DB) string {
	download := trafficColumn(db, "download")
	upload := trafficColumn(db, "upload")
	return fmt.Sprintf(
		"%s AS user_id, %s AS subscribe_id, %s, %s, %s",
		trafficColumn(db, "user_id"),
		trafficColumn(db, "subscribe_id"),
		trafficSumIntExpr(db, download+" + "+upload, "total"),
		trafficSumIntExpr(db, download, "download"),
		trafficSumIntExpr(db, upload, "upload"),
	)
}

func trafficSumIntExpr(db *gorm.DB, expr, alias string) string {
	if db != nil && db.Dialector.Name() == orm.DriverPostgres {
		return fmt.Sprintf("COALESCE(SUM(%s), 0)::bigint AS %s", expr, alias)
	}
	return fmt.Sprintf("COALESCE(SUM(%s), 0) AS %s", expr, alias)
}

func trafficColumn(db *gorm.DB, column string) string {
	if db != nil && db.Statement != nil {
		return db.Statement.Quote(clause.Column{Table: traffic.TrafficLog{}.TableName(), Name: column})
	}
	return traffic.TrafficLog{}.TableName() + "." + column
}
