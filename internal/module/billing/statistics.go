package billing

import (
	"context"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/repository"
)

// OrderStatistics is the part of the facade the platform's dashboard and the
// Telegram bot read billing's order figures through, instead of billing's
// repositories (ADR-001 rule 4). The reads return the repository's results
// and errors as they are.
type OrderStatistics interface {
	// OrderRevenueOn, OrderRevenueInMonth and OrderRevenueTotal sum the
	// orders of date's day, of date's month and of all time.
	OrderRevenueOn(ctx context.Context, date time.Time) (order.OrdersTotal, error)
	OrderRevenueInMonth(ctx context.Context, date time.Time) (order.OrdersTotal, error)
	OrderRevenueTotal(ctx context.Context) (order.OrdersTotal, error)
	// DailyOrderRevenue breaks date's month down by day and
	// MonthlyOrderRevenue the last months up to date's by month.
	DailyOrderRevenue(ctx context.Context, date time.Time) ([]order.OrdersTotalWithDate, error)
	MonthlyOrderRevenue(ctx context.Context, date time.Time) ([]order.OrdersTotalWithDate, error)
	// PayingUsersOn, PayingUsersInMonth and PayingUsersTotal count the
	// buyers of new orders and of renewals of date's day, of date's month
	// and of all time.
	PayingUsersOn(ctx context.Context, date time.Time) (newUsers, renewalUsers int64, err error)
	PayingUsersInMonth(ctx context.Context, date time.Time) (newUsers, renewalUsers int64, err error)
	PayingUsersTotal(ctx context.Context) (newUsers, renewalUsers int64, err error)
}

// statistics serves OrderStatistics from the order repository.
type statistics struct {
	orders repository.OrderRepo
}

func (s statistics) OrderRevenueOn(ctx context.Context, date time.Time) (order.OrdersTotal, error) {
	return s.orders.QueryDateOrders(ctx, date)
}

func (s statistics) OrderRevenueInMonth(ctx context.Context, date time.Time) (order.OrdersTotal, error) {
	return s.orders.QueryMonthlyOrders(ctx, date)
}

func (s statistics) OrderRevenueTotal(ctx context.Context) (order.OrdersTotal, error) {
	return s.orders.QueryTotalOrders(ctx)
}

func (s statistics) DailyOrderRevenue(ctx context.Context, date time.Time) ([]order.OrdersTotalWithDate, error) {
	return s.orders.QueryDailyOrdersList(ctx, date)
}

func (s statistics) MonthlyOrderRevenue(ctx context.Context, date time.Time) ([]order.OrdersTotalWithDate, error) {
	return s.orders.QueryMonthlyOrdersList(ctx, date)
}

func (s statistics) PayingUsersOn(ctx context.Context, date time.Time) (int64, int64, error) {
	return s.orders.QueryDateUserCounts(ctx, date)
}

func (s statistics) PayingUsersInMonth(ctx context.Context, date time.Time) (int64, int64, error) {
	return s.orders.QueryMonthlyUserCounts(ctx, date)
}

func (s statistics) PayingUsersTotal(ctx context.Context) (int64, int64, error) {
	return s.orders.QueryTotalUserCounts(ctx)
}
