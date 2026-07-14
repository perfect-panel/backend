package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/model/entity/traffic"
	"github.com/perfect-panel/server/pkg/orm"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TrafficRepo traffic 数据访问接口
type TrafficRepo interface {
	Insert(ctx context.Context, data *traffic.TrafficLog) error
	FindOne(ctx context.Context, id int64) (*traffic.TrafficLog, error)
	Update(ctx context.Context, data *traffic.TrafficLog) error
	Delete(ctx context.Context, id int64) error
	Transaction(ctx context.Context, fn func(db *gorm.DB) error) error
	QueryServerTrafficByDay(ctx context.Context, serverId int64, date time.Time) (*traffic.TotalTraffic, error)
	QueryTrafficByDay(ctx context.Context, date time.Time) (*traffic.TotalTraffic, error)
	QueryTrafficByMonthly(ctx context.Context, date time.Time) (*traffic.TotalTraffic, error)
	QueryTrafficSummary(ctx context.Context, start, end time.Time) (*traffic.TotalTraffic, error)
	TopServersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]traffic.ServerTrafficRanking, error)
	TopServersTrafficByMonthly(ctx context.Context, date time.Time, limit int) ([]traffic.ServerTrafficRanking, error)
	TopUsersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]traffic.UserTrafficRanking, error)
	TopUsersTrafficByMonthly(ctx context.Context, date time.Time, limit int) ([]traffic.UserTrafficRanking, error)
	QueryServerTrafficRanking(ctx context.Context, start, end time.Time) ([]traffic.ServerTrafficRanking, error)
	QueryUserTrafficRanking(ctx context.Context, start, end time.Time) ([]traffic.UserTrafficRanking, error)
	QueryTrafficLogPageList(ctx context.Context, userId, subscribeId int64, page, size int) ([]*traffic.TrafficLog, int64, error)
	QueryTrafficLogDetails(ctx context.Context, filter *traffic.TrafficLogDetailsFilter) ([]*traffic.TrafficLog, int64, error)
	DeleteBefore(ctx context.Context, end time.Time) error
}

var _ TrafficRepo = (*trafficRepo)(nil)

type trafficRepo struct {
	Conn  *gorm.DB
	table string
}

func newTrafficRepo(db *gorm.DB) TrafficRepo {
	return &trafficRepo{
		Conn:  db,
		table: "traffic",
	}
}

func (m *trafficRepo) Insert(ctx context.Context, data *traffic.TrafficLog) error {
	return m.Conn.WithContext(ctx).Create(&data).Error
}

func (m *trafficRepo) FindOne(ctx context.Context, id int64) (*traffic.TrafficLog, error) {
	var data traffic.TrafficLog
	err := m.Conn.WithContext(ctx).Model(&traffic.TrafficLog{}).Where("id = ?", id).First(&data).Error
	return &data, err
}

func (m *trafficRepo) Update(ctx context.Context, data *traffic.TrafficLog) error {
	return m.Conn.WithContext(ctx).Save(data).Error
}

func (m *trafficRepo) Delete(ctx context.Context, id int64) error {
	_, err := m.FindOne(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	return m.Conn.WithContext(ctx).Delete(&traffic.TrafficLog{}, id).Error
}

func (m *trafficRepo) Transaction(ctx context.Context, fn func(db *gorm.DB) error) error {
	return m.Conn.WithContext(ctx).Transaction(fn)
}

func (m *trafficRepo) QueryServerTrafficByDay(ctx context.Context, serverId int64, date time.Time) (*traffic.TotalTraffic, error) {
	var data traffic.TotalTraffic
	start, end := trafficDayRange(date)
	err := m.Conn.WithContext(ctx).Model(&traffic.TrafficLog{}).
		Select(totalTrafficSelect(m.Conn)).
		Where(fmt.Sprintf("%s = ? AND %s >= ? AND %s < ?", trafficColumn(m.Conn, "server_id"), trafficColumn(m.Conn, "timestamp"), trafficColumn(m.Conn, "timestamp")), serverId, start, end).
		Scan(&data).Error
	return &data, err
}

func (m *trafficRepo) QueryTrafficByDay(ctx context.Context, date time.Time) (*traffic.TotalTraffic, error) {
	var data traffic.TotalTraffic
	start, end := trafficDayRange(date)
	err := m.Conn.WithContext(ctx).Model(&traffic.TrafficLog{}).
		Select(totalTrafficSelect(m.Conn)).
		Where(trafficTimeRangeCondition(m.Conn), start, end).
		Scan(&data).Error
	return &data, err
}

func (m *trafficRepo) QueryTrafficByMonthly(ctx context.Context, date time.Time) (*traffic.TotalTraffic, error) {
	var data traffic.TotalTraffic
	start, end := trafficMonthRange(date)
	err := m.Conn.WithContext(ctx).Model(&traffic.TrafficLog{}).
		Select(totalTrafficSelect(m.Conn)).
		Where(trafficTimeRangeCondition(m.Conn), start, end).
		Scan(&data).Error
	return &data, err
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

func (m *trafficRepo) TopServersTrafficByMonthly(ctx context.Context, date time.Time, limit int) ([]traffic.ServerTrafficRanking, error) {
	var summaries []traffic.ServerTrafficRanking
	start, end := trafficMonthRange(date)
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

func (m *trafficRepo) TopUsersTrafficByMonthly(ctx context.Context, date time.Time, limit int) ([]traffic.UserTrafficRanking, error) {
	var summaries []traffic.UserTrafficRanking
	start, end := trafficMonthRange(date)
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
	page, size = normalizePage(page, size)
	err := m.Conn.WithContext(ctx).Model(&traffic.TrafficLog{}).Where("user_id = ? and subscribe_id= ?", userId, subscribeId).Count(&total).Limit(size).Offset((page - 1) * size).Find(&list).Error
	return list, total, err
}

func (m *trafficRepo) QueryTrafficLogDetails(ctx context.Context, filter *traffic.TrafficLogDetailsFilter) ([]*traffic.TrafficLog, int64, error) {
	if filter == nil {
		filter = &traffic.TrafficLogDetailsFilter{Page: 1, Size: 10}
	}
	filter.Page, filter.Size = normalizePage(filter.Page, filter.Size)

	query := m.Conn.WithContext(ctx).Model(&traffic.TrafficLog{})
	if filter.ServerId != 0 {
		query = query.Where("server_id = ?", filter.ServerId)
	}
	if !filter.Start.IsZero() && !filter.End.IsZero() {
		query = query.Where(trafficTimeRangeCondition(m.Conn), filter.Start, filter.End)
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
		Order("timestamp DESC").
		Limit(filter.Size).
		Offset((filter.Page - 1) * filter.Size).
		Find(&list).Error
	return list, total, err
}

func (m *trafficRepo) DeleteBefore(ctx context.Context, end time.Time) error {
	return m.Conn.WithContext(ctx).Model(&traffic.TrafficLog{}).Where(trafficColumn(m.Conn, "timestamp")+" <= ?", end).Delete(&traffic.TrafficLog{}).Error
}

func trafficDayRange(date time.Time) (time.Time, time.Time) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	return start, start.Add(24 * time.Hour)
}

func trafficMonthRange(date time.Time) (time.Time, time.Time) {
	start := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	return start, start.AddDate(0, 1, 0)
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
