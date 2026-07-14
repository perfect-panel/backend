package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/pkg/cache"
	"github.com/perfect-panel/server/pkg/orm"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const orderUserAuthMethodsTable = "user_auth_methods"

var (
	cacheOrderIdPrefix = "cache:order:id:"
	cacheOrderNoPrefix = "cache:order:no:"
)

// OrderRepo order 数据访问接口
type OrderRepo interface {
	Insert(ctx context.Context, data *order.Order, tx ...*gorm.DB) error
	FindOne(ctx context.Context, id int64) (*order.Order, error)
	FindOneByOrderNo(ctx context.Context, orderNo string) (*order.Order, error)
	Update(ctx context.Context, data *order.Order, tx ...*gorm.DB) error
	Delete(ctx context.Context, id int64, tx ...*gorm.DB) error
	Transaction(ctx context.Context, fn func(db *gorm.DB) error) error
	UpdateOrderStatus(ctx context.Context, orderNo string, status uint8, tx ...*gorm.DB) error
	UpdateOrderStatusAndTradeNo(ctx context.Context, orderNo string, status uint8, tradeNo string, tx ...*gorm.DB) error
	CountUserCouponUsage(ctx context.Context, userID int64, coupon string) (int64, error)
	QueryOrderListByPage(ctx context.Context, page, size int, status uint8, user, subscribe int64, search string) (int64, []*order.Details, error)
	FindOneDetails(ctx context.Context, id int64) (*order.Details, error)
	FindOneDetailsByOrderNo(ctx context.Context, orderNo string) (*order.Details, error)
	QueryMonthlyOrders(ctx context.Context, date time.Time) (order.OrdersTotal, error)
	QueryDateOrders(ctx context.Context, date time.Time) (order.OrdersTotal, error)
	QueryTotalOrders(ctx context.Context) (order.OrdersTotal, error)
	QueryMonthlyUserCounts(ctx context.Context, date time.Time) (int64, int64, error)
	QueryDateUserCounts(ctx context.Context, date time.Time) (int64, int64, error)
	QueryTotalUserCounts(ctx context.Context) (int64, int64, error)
	IsUserEligibleForNewOrder(ctx context.Context, userID int64) (bool, error)
	QueryDailyOrdersList(ctx context.Context, date time.Time) ([]order.OrdersTotalWithDate, error)
	QueryMonthlyOrdersList(ctx context.Context, date time.Time) ([]order.OrdersTotalWithDate, error)
}

var _ OrderRepo = (*orderRepo)(nil)

type orderRepo struct {
	cache.CachedConn
	table string
}

func newOrderRepo(db *gorm.DB, c *redis.Client) OrderRepo {
	return &orderRepo{
		CachedConn: cache.NewConn(db, c),
		table:      "order",
	}
}

func (m *orderRepo) getCacheKeys(data *order.Order) []string {
	if data == nil {
		return []string{}
	}
	orderIdKey := fmt.Sprintf("%s%v", cacheOrderIdPrefix, data.Id)
	orderNoKey := fmt.Sprintf("%s%v", cacheOrderNoPrefix, data.OrderNo)
	return []string{
		orderIdKey,
		orderNoKey,
	}
}

func (m *orderRepo) Insert(ctx context.Context, data *order.Order, tx ...*gorm.DB) error {
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Create(&data).Error
	}, m.getCacheKeys(data)...)
}

func (m *orderRepo) FindOne(ctx context.Context, id int64) (*order.Order, error) {
	var resp order.Order
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&order.Order{}).Where("id = ?", id).First(&resp).Error
	})
	switch {
	case err == nil:
		return &resp, nil
	default:
		return nil, err
	}
}

func (m *orderRepo) FindOneByOrderNo(ctx context.Context, orderNo string) (*order.Order, error) {
	var resp order.Order
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&order.Order{}).Where("order_no = ?", orderNo).First(&resp).Error
	})
	switch {
	case err == nil:
		return &resp, nil
	default:
		return nil, err
	}
}

func (m *orderRepo) Update(ctx context.Context, data *order.Order, tx ...*gorm.DB) error {
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Save(data).Error
	}, m.getCacheKeys(old)...)
}

func (m *orderRepo) Delete(ctx context.Context, id int64, tx ...*gorm.DB) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Delete(&order.Order{}, id).Error
	}, m.getCacheKeys(data)...)
}

func (m *orderRepo) Transaction(ctx context.Context, fn func(db *gorm.DB) error) error {
	return m.TransactCtx(ctx, fn)
}

func (m *orderRepo) CountUserCouponUsage(ctx context.Context, userID int64, coupon string) (int64, error) {
	var count int64
	err := m.QueryNoCacheCtx(ctx, &count, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&order.Order{}).Where("user_id = ? AND coupon = ?", userID, coupon).Count(&count).Error
	})
	return count, err
}

// QueryOrderListByPage Query order list by page
func (m *orderRepo) QueryOrderListByPage(ctx context.Context, page, size int, status uint8, user, subscribe int64, search string) (int64, []*order.Details, error) {
	var list []*order.Details
	var total int64
	page, size = normalizePage(page, size)
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v interface{}) error {
		conn = conn.Model(&order.Order{})
		conn = applyOrderListFilters(conn, status, user, subscribe, search)
		if err := conn.Count(&total).Error; err != nil {
			return err
		}
		return conn.Order(orderColumn(conn, "id") + " desc").Preload("Subscribe").Preload("Payment").Offset((page - 1) * size).Limit(size).Find(v).Error
	})
	return total, list, err
}

func applyOrderListFilters(conn *gorm.DB, status uint8, user, subscribe int64, search string) *gorm.DB {
	if status > 0 {
		conn = conn.Where(orderColumn(conn, "status")+" = ?", status)
	}
	if user > 0 {
		conn = conn.Where(orderColumn(conn, "user_id")+" = ?", user)
	}
	if subscribe > 0 {
		conn = conn.Where(orderColumn(conn, "subscribe_id")+" = ?", subscribe)
	}
	if search != "" {
		pattern := orm.LikePrefixPattern(search)
		if pattern != "" {
			conn = conn.Where(orderListSearchCondition(conn), pattern, pattern, pattern, "email", pattern)
		}
	}
	return conn
}

func orderListSearchCondition(conn *gorm.DB) string {
	authUserID := orderQuoteColumn(conn, orderUserAuthMethodsTable, "user_id")
	authType := orderQuoteColumn(conn, orderUserAuthMethodsTable, "auth_type")
	authIdentifier := orderQuoteColumn(conn, orderUserAuthMethodsTable, "auth_identifier")
	return fmt.Sprintf(
		"(%s LIKE ?%s OR %s LIKE ?%s OR %s LIKE ?%s OR EXISTS (SELECT 1 FROM %s WHERE %s = %s AND %s = ? AND %s LIKE ?%s))",
		orderColumn(conn, "order_no"),
		orm.LikeEscapeClause(),
		orderColumn(conn, "trade_no"),
		orm.LikeEscapeClause(),
		orderColumn(conn, "coupon"),
		orm.LikeEscapeClause(),
		orderQuoteTable(conn, orderUserAuthMethodsTable),
		authUserID,
		orderColumn(conn, "user_id"),
		authType,
		authIdentifier,
		orm.LikeEscapeClause(),
	)
}

// UpdateOrderStatus Update order status
func (m *orderRepo) UpdateOrderStatus(ctx context.Context, orderNo string, status uint8, tx ...*gorm.DB) error {
	orderInfo, err := m.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Model(&order.Order{}).Where("order_no = ?", orderNo).Update("status", status).Error
	}, m.getCacheKeys(orderInfo)...)
}

// UpdateOrderStatusAndTradeNo Update order status and trade number
func (m *orderRepo) UpdateOrderStatusAndTradeNo(ctx context.Context, orderNo string, status uint8, tradeNo string, tx ...*gorm.DB) error {
	orderInfo, err := m.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		if len(tx) > 0 {
			conn = tx[0]
		}
		return conn.Model(&order.Order{}).Where("order_no = ?", orderNo).Updates(map[string]interface{}{
			"status":   status,
			"trade_no": tradeNo,
		}).Error
	}, m.getCacheKeys(orderInfo)...)
}

// FindOneDetailsByOrderNo Find order details by order number
func (m *orderRepo) FindOneDetailsByOrderNo(ctx context.Context, orderNo string) (*order.Details, error) {
	var orderInfo order.Details
	err := m.QueryNoCacheCtx(ctx, &orderInfo, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&order.Order{}).Where("order_no = ?", orderNo).Preload("Subscribe").Preload("Payment").First(v).Error
	})
	return &orderInfo, err
}

func (m *orderRepo) FindOneDetails(ctx context.Context, id int64) (*order.Details, error) {
	var orderInfo order.Details
	err := m.QueryNoCacheCtx(ctx, &orderInfo, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&order.Order{}).
			Where("id = ?", id).
			Preload("Subscribe").
			Preload("SubOrders").
			First(v).Error
	})
	return &orderInfo, err
}

func (m *orderRepo) QueryMonthlyOrders(ctx context.Context, date time.Time) (order.OrdersTotal, error) {
	firstDay := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	lastDay := firstDay.AddDate(0, 1, 0).Add(-time.Nanosecond)
	var result order.OrdersTotal
	err := m.QueryNoCacheCtx(ctx, &result, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&order.Order{}).
			Where("status IN ? AND created_at BETWEEN ? AND ? AND method != ?", []int64{2, 5}, firstDay, lastDay, "balance").
			Select(
				"SUM(amount) as amount_total, " +
					"SUM(CASE WHEN is_new THEN amount ELSE 0 END) as new_order_amount, " +
					"SUM(CASE WHEN NOT is_new THEN amount ELSE 0 END) as renewal_order_amount",
			).
			Scan(v).Error
	})
	return result, err
}

// QueryDateOrders Query orders by date
func (m *orderRepo) QueryDateOrders(ctx context.Context, date time.Time) (order.OrdersTotal, error) {
	start := date.Truncate(24 * time.Hour)
	end := start.Add(24 * time.Hour).Add(-time.Nanosecond)
	var result order.OrdersTotal
	err := m.QueryNoCacheCtx(ctx, &result, func(conn *gorm.DB, v interface{}) error {
		return conn.Model(&order.Order{}).
			Where("status IN ? AND created_at BETWEEN ? AND ? AND method != ?", []int64{2, 5}, start, end, "balance").
			Select(
				"SUM(amount) as amount_total, " +
					"SUM(CASE WHEN is_new THEN amount ELSE 0 END) as new_order_amount, " +
					"SUM(CASE WHEN NOT is_new THEN amount ELSE 0 END) as renewal_order_amount",
			).
			Scan(v).Error
	})
	return result, err
}

func (m *orderRepo) QueryTotalOrders(ctx context.Context) (order.OrdersTotal, error) {
	var result order.OrdersTotal

	err := m.QueryNoCacheCtx(ctx, &result, func(conn *gorm.DB, _ interface{}) error {
		return conn.Model(&order.Order{}).
			Select(`
				SUM(amount) AS amount_total,
				SUM(CASE WHEN is_new THEN amount ELSE 0 END) AS new_order_amount,
				SUM(CASE WHEN NOT is_new THEN amount ELSE 0 END) AS renewal_order_amount
			`).
			Where("status IN ? AND method != ?", []int64{2, 5}, "balance").
			Scan(&result).Error
	})

	return result, err
}

func (m *orderRepo) QueryMonthlyUserCounts(ctx context.Context, date time.Time) (int64, int64, error) {
	firstDay := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	nextMonth := firstDay.AddDate(0, 1, 0)

	var counts order.UserCounts

	err := m.QueryNoCacheCtx(ctx, nil, func(conn *gorm.DB, _ interface{}) error {
		return conn.Model(&order.Order{}).
			Select(`
				COUNT(DISTINCT CASE WHEN is_new THEN user_id END) AS new_users,
				COUNT(DISTINCT CASE WHEN NOT is_new THEN user_id END) AS renewal_users
			`).
			Where("status IN ? AND created_at >= ? AND created_at < ? AND method != ?",
				[]int64{2, 5}, firstDay, nextMonth, "balance").
			Scan(&counts).Error
	})

	return counts.NewUsers, counts.RenewalUsers, err
}

func (m *orderRepo) QueryDateUserCounts(ctx context.Context, date time.Time) (int64, int64, error) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	nextDay := start.Add(24 * time.Hour)

	var counts order.UserCounts

	err := m.QueryNoCacheCtx(ctx, nil, func(conn *gorm.DB, _ interface{}) error {
		return conn.Model(&order.Order{}).
			Select(`
				COUNT(DISTINCT CASE WHEN is_new THEN user_id END) AS new_users,
				COUNT(DISTINCT CASE WHEN NOT is_new THEN user_id END) AS renewal_users
			`).
			Where("status IN ? AND created_at >= ? AND created_at < ? AND method != ?",
				[]int64{2, 5}, start, nextDay, "balance").
			Scan(&counts).Error
	})

	return counts.NewUsers, counts.RenewalUsers, err
}

func (m *orderRepo) QueryTotalUserCounts(ctx context.Context) (int64, int64, error) {
	var counts order.UserCounts

	err := m.QueryNoCacheCtx(ctx, nil, func(conn *gorm.DB, _ interface{}) error {
		return conn.Model(&order.Order{}).
			Where("status IN ? AND method != ?", []int64{2, 5}, "balance").
			Select(`
				COUNT(DISTINCT CASE WHEN is_new THEN user_id END) AS new_users,
				COUNT(DISTINCT CASE WHEN NOT is_new THEN user_id END) AS renewal_users
			`).
			Scan(&counts).Error
	})

	return counts.NewUsers, counts.RenewalUsers, err
}

func (m *orderRepo) IsUserEligibleForNewOrder(ctx context.Context, userID int64) (bool, error) {
	var count int64
	err := m.QueryNoCacheCtx(ctx, nil, func(conn *gorm.DB, _ interface{}) error {
		return conn.Model(&order.Order{}).
			Where("user_id = ? AND status IN ?", userID, []int64{2, 5}).
			Count(&count).Error
	})
	return count == 0, err
}

// QueryDailyOrdersList 查询当月每日订单统计
func (m *orderRepo) QueryDailyOrdersList(ctx context.Context, date time.Time) ([]order.OrdersTotalWithDate, error) {
	var results []order.OrdersTotalWithDate

	err := m.QueryNoCacheCtx(ctx, &results, func(conn *gorm.DB, v interface{}) error {
		firstDay := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
		nextDay := date.AddDate(0, 0, 1).Truncate(24 * time.Hour)
		dateExpr := orderDateBucketExpr(conn, "created_at", "day")

		return conn.Model(&order.Order{}).
			Select(fmt.Sprintf(`
				%s AS date,
				SUM(amount) AS amount_total,
				SUM(CASE WHEN is_new THEN amount ELSE 0 END) AS new_order_amount,
				SUM(CASE WHEN NOT is_new THEN amount ELSE 0 END) AS renewal_order_amount
			`, dateExpr)).
			Where("status IN ? AND created_at >= ? AND created_at < ? AND method != ?",
				[]int64{2, 5}, firstDay, nextDay, "balance").
			Group(dateExpr).
			Order("date ASC").
			Scan(v).Error
	})
	return results, err
}

// QueryMonthlyOrdersList 查询过去 6 个月订单统计（包含当前月）
func (m *orderRepo) QueryMonthlyOrdersList(ctx context.Context, date time.Time) ([]order.OrdersTotalWithDate, error) {
	var results []order.OrdersTotalWithDate

	err := m.QueryNoCacheCtx(ctx, &results, func(conn *gorm.DB, v interface{}) error {
		start := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location()).AddDate(0, -5, 0)
		end := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location()).AddDate(0, 1, 0)
		dateExpr := orderDateBucketExpr(conn, "created_at", "month")

		return conn.Model(&order.Order{}).
			Select(fmt.Sprintf(`
				%s AS date,
				SUM(amount) AS amount_total,
				SUM(CASE WHEN is_new THEN amount ELSE 0 END) AS new_order_amount,
				SUM(CASE WHEN NOT is_new THEN amount ELSE 0 END) AS renewal_order_amount
			`, dateExpr)).
			Where("status IN ? AND created_at >= ? AND created_at < ? AND method != ?",
				[]int64{2, 5}, start, end, "balance").
			Group(dateExpr).
			Order("date ASC").
			Scan(v).Error
	})
	return results, err
}

func orderTableName(db *gorm.DB) string {
	return orderQuoteTable(db, order.Order{}.TableName())
}

func orderColumn(db *gorm.DB, column string) string {
	return orderQuoteColumn(db, order.Order{}.TableName(), column)
}

func orderQuoteTable(db *gorm.DB, table string) string {
	if db != nil && db.Statement != nil {
		return db.Statement.Quote(clause.Table{Name: table})
	}
	return table
}

func orderQuoteColumn(db *gorm.DB, table, column string) string {
	if db != nil && db.Statement != nil {
		return db.Statement.Quote(clause.Column{Table: table, Name: column})
	}
	return table + "." + column
}

func orderDateBucketExpr(db *gorm.DB, column, bucket string) string {
	if db.Dialector.Name() == "postgres" {
		if bucket == "month" {
			return fmt.Sprintf("TO_CHAR(%s, 'YYYY-MM')", column)
		}
		return fmt.Sprintf("TO_CHAR(%s, 'YYYY-MM-DD')", column)
	}
	if bucket == "month" {
		return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m')", column)
	}
	return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m-%%d')", column)
}
