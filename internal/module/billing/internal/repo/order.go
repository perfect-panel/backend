package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/perfect-panel/backend/internal/repository"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/pkg/cache"
	"github.com/perfect-panel/backend/pkg/orm"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const orderUserAuthMethodsTable = "user_auth_methods"

var (
	cacheOrderIdPrefix = "cache:order:id:"
	cacheOrderNoPrefix = "cache:order:no:"
)

var _ repository.OrderRepo = (*orderRepo)(nil)

type orderRepo struct {
	cache.CachedConn
	table string
}

// NewOrderRepo builds the module-owned implementation over the shared
// cached connection.
func NewOrderRepo(conn cache.CachedConn) repository.OrderRepo {
	return &orderRepo{
		CachedConn: conn,
		table:      "order",
	}
}

// OrderUserCountsByBucket counts distinct ordering users per date bucket
// (repository.OrderStatsBridge); the identity bundle merges the counts with
// registration statistics in Go so no SQL joins the two domains.
func (m *orderRepo) OrderUserCountsByBucket(ctx context.Context, isNew bool, since time.Time, until *time.Time, bucket string) (map[string]int64, error) {
	type row struct {
		Date  string
		Users int64
	}
	var rows []row
	err := m.QueryNoCacheCtx(ctx, &rows, func(conn *gorm.DB, v any) error {
		dateExpr := orm.DateBucketExpr(conn, "created_at", bucket)
		q := conn.Model(&order.Order{}).
			Select(fmt.Sprintf("%s AS date, COUNT(DISTINCT user_id) AS users", dateExpr)).
			Where("is_new = ? AND status IN ?", isNew, settledStatuses())
		if until != nil {
			q = q.Where("created_at BETWEEN ? AND ?", since, *until)
		} else {
			q = q.Where("created_at >= ?", since)
		}
		return q.Group(dateExpr).Scan(v).Error
	})
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int64, len(rows))
	for _, r := range rows {
		counts[r.Date] = r.Users
	}
	return counts, nil
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

func (m *orderRepo) Insert(ctx context.Context, data *order.Order) error {
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return withOrderEventTransaction(conn, func(conn *gorm.DB) error {
			if data.StateVersion == 0 {
				data.StateVersion = 1
			}
			if err := conn.Create(&data).Error; err != nil {
				return err
			}
			return insertOrderEvent(conn, data, orderEventCreated)
		})
	}, m.getCacheKeys(data)...)
}

func (m *orderRepo) FindOne(ctx context.Context, id int64) (*order.Order, error) {
	var resp order.Order
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v any) error {
		return conn.Model(&order.Order{}).Where("id = ?", id).First(&resp).Error
	})
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (m *orderRepo) FindOneByOrderNo(ctx context.Context, orderNo string) (*order.Order, error) {
	var resp order.Order
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v any) error {
		return conn.Model(&order.Order{}).Where("order_no = ?", orderNo).First(&resp).Error
	})
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (m *orderRepo) FindOneByIdempotencyKey(ctx context.Context, key string) (*order.Order, error) {
	var resp order.Order
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v any) error {
		return conn.Model(&order.Order{}).Where("idempotency_key = ?", key).First(&resp).Error
	})
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (m *orderRepo) FindOneByOrderNoForUpdate(ctx context.Context, orderNo string) (*order.Order, error) {
	var resp order.Order
	err := m.QueryNoCacheCtx(ctx, &resp, func(conn *gorm.DB, v any) error {
		return conn.Clauses(clause.Locking{Strength: "UPDATE"}).
			Model(&order.Order{}).
			Where("order_no = ?", orderNo).
			First(&resp).Error
	})
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (m *orderRepo) Update(ctx context.Context, data *order.Order) error {
	old, err := m.FindOne(ctx, data.Id)
	if err != nil {
		return err
	}
	if old.Status != data.Status || old.StateVersion != data.StateVersion {
		return fmt.Errorf("order status and state version may only be changed through a state transition")
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		result := conn.Model(&order.Order{}).
			Where("id = ? AND status = ? AND state_version = ?", data.Id, old.Status, old.StateVersion).
			Select("*").
			// These values are assigned only when an order is created.  Generic
			// updates intentionally keep their database values: a Go string zero
			// value cannot represent the SQL NULL used by legacy/V1 orders, and
			// Select("*") would otherwise rewrite that NULL as an empty string.
			Omit("IdempotencyKey", "IdempotencyHash", "GuestCheckoutTokenHash").
			Updates(data)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("order changed concurrently")
		}
		return nil
	}, m.getCacheKeys(old)...)
}

func (m *orderRepo) Delete(ctx context.Context, id int64) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Delete(&order.Order{}, id).Error
	}, m.getCacheKeys(data)...)
}

func (m *orderRepo) CountUserCouponUsage(ctx context.Context, userID int64, coupon string) (int64, error) {
	var count int64
	err := m.QueryNoCacheCtx(ctx, &count, func(conn *gorm.DB, v any) error {
		return conn.Model(&order.Order{}).
			Where("user_id = ? AND coupon = ? AND status IN ?", userID, coupon, statusList(order.CouponUseStatuses())).
			Count(&count).Error
	})
	return count, err
}

// CountGuestCouponUsage counts the orders a guest identity created with the
// coupon that hold a reservation or consumed a use. Guest orders carry no
// user until activation and keep the identity they were created under
// afterwards, so the identity, not the user id, counts them.
func (m *orderRepo) CountGuestCouponUsage(ctx context.Context, authType, identifier, coupon string) (int64, error) {
	var count int64
	err := m.QueryNoCacheCtx(ctx, &count, func(conn *gorm.DB, v any) error {
		return conn.Model(&order.Order{}).
			Where("guest_auth_type = ? AND guest_identifier = ? AND coupon = ? AND status IN ?",
				authType, identifier, coupon, statusList(order.CouponUseStatuses())).
			Count(&count).Error
	})
	return count, err
}

// CountPendingGuestOrders counts the unpaid orders a guest identity created
// at or after since and still holds before its account exists; guest orders
// carry no user until activation.
func (m *orderRepo) CountPendingGuestOrders(ctx context.Context, authType, identifier string, since time.Time) (int64, error) {
	var count int64
	err := m.QueryNoCacheCtx(ctx, &count, func(conn *gorm.DB, v any) error {
		return conn.Model(&order.Order{}).
			Where("user_id = ? AND status = ? AND guest_auth_type = ? AND guest_identifier = ? AND created_at >= ?",
				0, order.StatusPending, authType, identifier, since).
			Count(&count).Error
	})
	return count, err
}

// QueryOrderListByPage pages the orders matching the filters, newest first,
// with their payment methods.
func (m *orderRepo) QueryOrderListByPage(ctx context.Context, page, size int, status uint8, user, subscribe int64, search string) (int64, []*order.Details, error) {
	var list []*order.Details
	var total int64
	page, size = repository.NormalizePage(page, size)
	err := m.QueryNoCacheCtx(ctx, &list, func(conn *gorm.DB, v any) error {
		conn = conn.Model(&order.Order{})
		conn = applyOrderListFilters(conn, status, user, subscribe, search)
		if err := conn.Count(&total).Error; err != nil {
			return err
		}
		return conn.Order(orderColumn(conn, "id") + " desc").Preload("Payment").Offset((page - 1) * size).Limit(size).Find(v).Error
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

func (m *orderRepo) UpdateOrderStatusFrom(ctx context.Context, orderNo string, from, status uint8) (bool, error) {
	orderInfo, err := m.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return false, err
	}
	var updated bool
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return withOrderEventTransaction(conn, func(conn *gorm.DB) error {
			result := conn.Model(&order.Order{}).
				Where("order_no = ? AND status = ?", orderNo, from).
				Updates(map[string]any{
					"status":        status,
					"state_version": gorm.Expr("state_version + ?", 1),
				})
			updated = result.RowsAffected == 1
			if result.Error != nil || !updated {
				return result.Error
			}
			var latest order.Order
			if err := conn.Where("order_no = ?", orderNo).First(&latest).Error; err != nil {
				return err
			}
			return insertOrderEvent(conn, &latest, orderEventTypeForStatus(status))
		})
	}, m.getCacheKeys(orderInfo)...)
	if err != nil {
		updated = false
	}
	return updated, err
}

// UpdatePaymentExpectation persists the exact amount and currency sent to a
// payment gateway. A snapshot is immutable: only a pending order that has not
// yet been sent to any gateway may set it. Repeated checkout requests must use
// the exact same amount and currency that were originally bound to the order.
func (m *orderRepo) UpdatePaymentExpectation(ctx context.Context, orderNo string, amount int64, currency string) (bool, error) {
	orderInfo, err := m.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return false, err
	}
	var updated bool
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		result := conn.Model(&order.Order{}).
			Where("order_no = ? AND status = ? AND payment_currency = ?", orderNo, order.StatusPending, "").
			Updates(map[string]any{
				"payment_amount":   amount,
				"payment_currency": currency,
			})
		updated = result.RowsAffected == 1
		return result.Error
	}, m.getCacheKeys(orderInfo)...)
	if err != nil || updated {
		return updated, err
	}
	// MySQL reports zero affected rows when a repeated checkout writes the
	// same values. Treat that as success only after re-reading and confirming
	// that the order is still pending with the identical snapshot.
	latest, err := m.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return false, err
	}
	return latest.Status == order.StatusPending && latest.PaymentAmount == amount && latest.PaymentCurrency == currency, nil
}

// SetPaymentTradeNoIfEmpty atomically claims the sole provider-side payment
// intent for a pending order. It prevents concurrent first Stripe checkouts
// from exposing two independently chargeable client secrets.
func (m *orderRepo) SetPaymentTradeNoIfEmpty(ctx context.Context, orderNo, tradeNo string) (bool, error) {
	orderInfo, err := m.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return false, err
	}
	var updated bool
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		result := conn.Model(&order.Order{}).
			Where("order_no = ? AND status = ? AND (trade_no IS NULL OR trade_no = '')", orderNo, order.StatusPending).
			Update("trade_no", tradeNo)
		updated = result.RowsAffected == 1
		return result.Error
	}, m.getCacheKeys(orderInfo)...)
	if err != nil {
		updated = false
	}
	return updated, err
}

func (m *orderRepo) SetCommission(ctx context.Context, orderNo string, amount, refererID int64) error {
	orderInfo, err := m.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return err
	}
	return m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return conn.Model(&order.Order{}).Where("order_no = ?", orderNo).
			Updates(map[string]any{"commission": amount, "commission_referer_id": refererID}).Error
	}, m.getCacheKeys(orderInfo)...)
}

func (m *orderRepo) HasCommissionedOrder(ctx context.Context, userID int64, exceptOrderNo string) (bool, error) {
	var count int64
	err := m.QueryNoCacheCtx(ctx, &count, func(conn *gorm.DB, v any) error {
		return conn.Model(&order.Order{}).
			Where("user_id = ? AND commission > 0 AND order_no <> ?", userID, exceptOrderNo).
			Count(&count).Error
	})
	return count > 0, err
}

func (m *orderRepo) CountPendingByPaymentID(ctx context.Context, paymentID int64) (int64, error) {
	var count int64
	err := m.QueryNoCacheCtx(ctx, &count, func(conn *gorm.DB, value any) error {
		return conn.Model(&order.Order{}).
			Where("payment_id = ? AND status = ?", paymentID, order.StatusPending).
			Count(&count).Error
	})
	return count, err
}

// MarkOrderPaid performs the only valid callback-driven state transition. The
// affected-row result is part of the contract so callers cannot enqueue an
// activation task after a stale or conflicting transition. The transition
// also binds the trade number: an order that already claimed another
// gateway payment (a Stripe intent, a Cryptomus invoice) is left alone, so a
// settlement racing that claim cannot overwrite it.
func (m *orderRepo) MarkOrderPaid(ctx context.Context, orderNo, tradeNo string) (bool, error) {
	orderInfo, err := m.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return false, err
	}
	var updated bool
	err = m.ExecCtx(ctx, func(conn *gorm.DB) error {
		return withOrderEventTransaction(conn, func(conn *gorm.DB) error {
			result := conn.Model(&order.Order{}).
				Where("order_no = ? AND status = ? AND (trade_no IS NULL OR trade_no = '' OR trade_no = ?)", orderNo, order.StatusPending, tradeNo).
				Updates(map[string]any{
					"status":        order.StatusPaid,
					"trade_no":      tradeNo,
					"state_version": gorm.Expr("state_version + ?", 1),
				})
			updated = result.RowsAffected == 1
			if result.Error != nil || !updated {
				return result.Error
			}
			var latest order.Order
			if err := conn.Where("order_no = ?", orderNo).First(&latest).Error; err != nil {
				return err
			}
			return insertOrderEvent(conn, &latest, orderEventPaymentPaid)
		})
	}, m.getCacheKeys(orderInfo)...)
	if err != nil {
		updated = false
	}
	return updated, err
}

func (m *orderRepo) QueryOrdersByStatusAfterID(ctx context.Context, status uint8, afterID int64, limit int) ([]*order.Order, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	var orders []*order.Order
	err := m.QueryNoCacheCtx(ctx, &orders, func(conn *gorm.DB, value any) error {
		return conn.Model(&order.Order{}).
			Where("status = ? AND id > ?", status, afterID).
			Order("id ASC").
			Limit(limit).
			Find(value).Error
	})
	return orders, err
}

// FindOneDetailsByOrderNo reads an order with its payment method.
func (m *orderRepo) FindOneDetailsByOrderNo(ctx context.Context, orderNo string) (*order.Details, error) {
	var orderInfo order.Details
	err := m.QueryNoCacheCtx(ctx, &orderInfo, func(conn *gorm.DB, v any) error {
		return conn.Model(&order.Order{}).Where("order_no = ?", orderNo).Preload("Payment").First(v).Error
	})
	return &orderInfo, err
}

func (m *orderRepo) FindOneDetails(ctx context.Context, id int64) (*order.Details, error) {
	var orderInfo order.Details
	err := m.QueryNoCacheCtx(ctx, &orderInfo, func(conn *gorm.DB, v any) error {
		return conn.Model(&order.Order{}).
			Where("id = ?", id).
			Preload("SubOrders").
			First(v).Error
	})
	return &orderInfo, err
}

func (m *orderRepo) QueryMonthlyOrders(ctx context.Context, date time.Time) (order.OrdersTotal, error) {
	firstDay := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	lastDay := firstDay.AddDate(0, 1, 0).Add(-time.Nanosecond)
	var result order.OrdersTotal
	err := m.QueryNoCacheCtx(ctx, &result, func(conn *gorm.DB, v any) error {
		return conn.Model(&order.Order{}).
			Where("status IN ? AND created_at BETWEEN ? AND ? AND method != ?", settledStatuses(), firstDay, lastDay, "balance").
			Select(
				"SUM(amount) as amount_total, " +
					"SUM(CASE WHEN is_new THEN amount ELSE 0 END) as new_order_amount, " +
					"SUM(CASE WHEN NOT is_new THEN amount ELSE 0 END) as renewal_order_amount",
			).
			Scan(v).Error
	})
	return result, err
}

// appDayStart returns midnight, in the application timezone, of the calendar
// day containing date. Truncate(24*time.Hour) rounds to UTC midnight instead,
// which shifts "today" by the zone offset.
func appDayStart(date time.Time) time.Time {
	loc := timeutil.Location()
	local := date.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}

// QueryDateOrders totals the settled gateway orders of date's day in the
// application timezone.
func (m *orderRepo) QueryDateOrders(ctx context.Context, date time.Time) (order.OrdersTotal, error) {
	start := appDayStart(date)
	end := start.AddDate(0, 0, 1)
	var result order.OrdersTotal
	err := m.QueryNoCacheCtx(ctx, &result, func(conn *gorm.DB, v any) error {
		return conn.Model(&order.Order{}).
			Where("status IN ? AND created_at >= ? AND created_at < ? AND method != ?", settledStatuses(), start, end, "balance").
			Select(
				"SUM(amount) as amount_total, " +
					"SUM(CASE WHEN is_new THEN amount ELSE 0 END) as new_order_amount, " +
					"SUM(CASE WHEN NOT is_new THEN amount ELSE 0 END) as renewal_order_amount",
			).
			Scan(v).Error
	})
	return result, err
}

// QueryDailyReport totals the orders settled on one calendar day and breaks
// them down by plan and by payment method. Unlike the dashboard totals it
// counts balance-funded orders too: the daily report is an operational
// summary of everything that was settled, not of gateway revenue only.
func (m *orderRepo) QueryDailyReport(ctx context.Context, date time.Time) (*order.DailyReport, error) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	end := start.AddDate(0, 0, 1)
	settled := settledStatuses()

	report := &order.DailyReport{Date: start}
	err := m.QueryNoCacheCtx(ctx, report, func(conn *gorm.DB, _ any) error {
		var totals struct {
			Orders int64
			Amount int64
		}
		if err := conn.Model(&order.Order{}).
			Select("COUNT(*) AS orders, COALESCE(SUM(amount), 0) AS amount").
			Where("status IN ? AND created_at >= ? AND created_at < ?", settled, start, end).
			Scan(&totals).Error; err != nil {
			return err
		}
		report.Orders = totals.Orders
		report.Amount = totals.Amount

		if err := conn.Model(&order.Order{}).
			Select("subscribe_id AS id, COUNT(*) AS orders, COALESCE(SUM(amount), 0) AS amount").
			Where("status IN ? AND created_at >= ? AND created_at < ?", settled, start, end).
			Group("subscribe_id").
			Order("amount DESC").
			Scan(&report.ByPlan).Error; err != nil {
			return err
		}

		return conn.Model(&order.Order{}).
			Select("method AS name, COUNT(*) AS orders, COALESCE(SUM(amount), 0) AS amount").
			Where("status IN ? AND created_at >= ? AND created_at < ?", settled, start, end).
			Group("method").
			Order("amount DESC").
			Scan(&report.ByMethod).Error
	})
	if err != nil {
		return nil, err
	}
	return report, nil
}

func (m *orderRepo) QueryTotalOrders(ctx context.Context) (order.OrdersTotal, error) {
	var result order.OrdersTotal

	err := m.QueryNoCacheCtx(ctx, &result, func(conn *gorm.DB, _ any) error {
		return conn.Model(&order.Order{}).
			Select(`
				SUM(amount) AS amount_total,
				SUM(CASE WHEN is_new THEN amount ELSE 0 END) AS new_order_amount,
				SUM(CASE WHEN NOT is_new THEN amount ELSE 0 END) AS renewal_order_amount
			`).
			Where("status IN ? AND method != ?", settledStatuses(), "balance").
			Scan(&result).Error
	})

	return result, err
}

func (m *orderRepo) QueryMonthlyUserCounts(ctx context.Context, date time.Time) (int64, int64, error) {
	firstDay := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	nextMonth := firstDay.AddDate(0, 1, 0)

	var counts order.UserCounts

	err := m.QueryNoCacheCtx(ctx, nil, func(conn *gorm.DB, _ any) error {
		return conn.Model(&order.Order{}).
			Select(`
				COUNT(DISTINCT CASE WHEN is_new THEN user_id END) AS new_users,
				COUNT(DISTINCT CASE WHEN NOT is_new THEN user_id END) AS renewal_users
			`).
			Where("status IN ? AND created_at >= ? AND created_at < ? AND method != ?",
				settledStatuses(), firstDay, nextMonth, "balance").
			Scan(&counts).Error
	})

	return counts.NewUsers, counts.RenewalUsers, err
}

func (m *orderRepo) QueryDateUserCounts(ctx context.Context, date time.Time) (int64, int64, error) {
	start := appDayStart(date)
	nextDay := start.AddDate(0, 0, 1)

	var counts order.UserCounts

	err := m.QueryNoCacheCtx(ctx, nil, func(conn *gorm.DB, _ any) error {
		return conn.Model(&order.Order{}).
			Select(`
				COUNT(DISTINCT CASE WHEN is_new THEN user_id END) AS new_users,
				COUNT(DISTINCT CASE WHEN NOT is_new THEN user_id END) AS renewal_users
			`).
			Where("status IN ? AND created_at >= ? AND created_at < ? AND method != ?",
				settledStatuses(), start, nextDay, "balance").
			Scan(&counts).Error
	})

	return counts.NewUsers, counts.RenewalUsers, err
}

func (m *orderRepo) QueryTotalUserCounts(ctx context.Context) (int64, int64, error) {
	var counts order.UserCounts

	err := m.QueryNoCacheCtx(ctx, nil, func(conn *gorm.DB, _ any) error {
		return conn.Model(&order.Order{}).
			Where("status IN ? AND method != ?", settledStatuses(), "balance").
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
	err := m.QueryNoCacheCtx(ctx, nil, func(conn *gorm.DB, _ any) error {
		return conn.Model(&order.Order{}).
			Where("user_id = ? AND status IN ?", userID, settledStatuses()).
			Count(&count).Error
	})
	return count == 0, err
}

// QueryDailyOrdersList totals the settled gateway orders of each day of
// date's month up to and including date's day, in the application timezone.
func (m *orderRepo) QueryDailyOrdersList(ctx context.Context, date time.Time) ([]order.OrdersTotalWithDate, error) {
	var results []order.OrdersTotalWithDate

	err := m.QueryNoCacheCtx(ctx, &results, func(conn *gorm.DB, v any) error {
		today := appDayStart(date)
		firstDay := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
		nextDay := today.AddDate(0, 0, 1)
		dateExpr := orm.DateBucketExpr(conn, "created_at", "day")

		return conn.Model(&order.Order{}).
			Select(fmt.Sprintf(`
				%s AS date,
				SUM(amount) AS amount_total,
				SUM(CASE WHEN is_new THEN amount ELSE 0 END) AS new_order_amount,
				SUM(CASE WHEN NOT is_new THEN amount ELSE 0 END) AS renewal_order_amount
			`, dateExpr)).
			Where("status IN ? AND created_at >= ? AND created_at < ? AND method != ?",
				settledStatuses(), firstDay, nextDay, "balance").
			Group(dateExpr).
			Order("date ASC").
			Scan(v).Error
	})
	return results, err
}

// QueryMonthlyOrdersList totals the settled gateway orders of each of the six
// months up to and including date's month.
func (m *orderRepo) QueryMonthlyOrdersList(ctx context.Context, date time.Time) ([]order.OrdersTotalWithDate, error) {
	var results []order.OrdersTotalWithDate

	err := m.QueryNoCacheCtx(ctx, &results, func(conn *gorm.DB, v any) error {
		start := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location()).AddDate(0, -5, 0)
		end := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location()).AddDate(0, 1, 0)
		dateExpr := orm.DateBucketExpr(conn, "created_at", "month")

		return conn.Model(&order.Order{}).
			Select(fmt.Sprintf(`
				%s AS date,
				SUM(amount) AS amount_total,
				SUM(CASE WHEN is_new THEN amount ELSE 0 END) AS new_order_amount,
				SUM(CASE WHEN NOT is_new THEN amount ELSE 0 END) AS renewal_order_amount
			`, dateExpr)).
			Where("status IN ? AND created_at >= ? AND created_at < ? AND method != ?",
				settledStatuses(), start, end, "balance").
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

// statusList converts statuses for an IN clause. They must be bound as
// integers: database/sql sends a []uint8 as one byte string, not a list.
func statusList(statuses []uint8) []int64 {
	list := make([]int64, len(statuses))
	for i, status := range statuses {
		list[i] = int64(status)
	}
	return list
}

// settledStatuses selects orders whose payment was collected.
func settledStatuses() []int64 { return statusList(order.SettledStatuses()) }
