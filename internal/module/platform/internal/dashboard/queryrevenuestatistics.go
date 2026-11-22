package dashboard

import (
	"context"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/internal/readmodel"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

const consoleRevenueStatisticsCacheKey = "console:revenue_statistics"
const consoleRevenueStatisticsCacheTTL = 60 * time.Second

// QueryRevenueStatistics summarizes order revenue for today, this month (with
// its daily breakdown) and all time (with the last months' breakdown). The
// totals are required; a breakdown that cannot be read is left out. The
// summary is cached for a minute.
func (s *Service) QueryRevenueStatistics(ctx context.Context) (*dto.RevenueStatisticsResponse, error) {
	if demoMode() {
		return mockRevenueStatistics(), nil
	}
	if cached, ok := readSnapshot[dto.RevenueStatisticsResponse](ctx, s.deps.Cache, consoleRevenueStatisticsCacheKey); ok {
		return cached, nil
	}

	now := timeutil.Now()
	today, err := s.deps.Orders.QueryDateOrders(ctx, now)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "sum today's orders: %v", err)
	}
	month, err := s.deps.Orders.QueryMonthlyOrders(ctx, now)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "sum this month's orders: %v", err)
	}
	monthly := ordersStatistics("", month)
	monthly.List = make([]dto.OrdersStatistics, 0)
	if days, err := s.deps.Orders.QueryDailyOrdersList(ctx, now); err != nil {
		logger.WithContext(ctx).Errorw("[QueryRevenueStatistics] daily order breakdown", logger.Field("error", err.Error()))
	} else {
		monthly.List = ordersBreakdown(days)
	}

	total, err := s.deps.Orders.QueryTotalOrders(ctx)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "sum all orders: %v", err)
	}
	all := ordersStatistics("", total)
	all.List = make([]dto.OrdersStatistics, 0)
	if months, err := s.deps.Orders.QueryMonthlyOrdersList(ctx, now); err != nil {
		logger.WithContext(ctx).Errorw("[QueryRevenueStatistics] monthly order breakdown", logger.Field("error", err.Error()))
	} else {
		all.List = ordersBreakdown(months)
	}

	resp := &dto.RevenueStatisticsResponse{
		Today:   ordersStatistics("", today),
		Monthly: monthly,
		All:     all,
	}
	storeSnapshot(ctx, s.deps.Cache, consoleRevenueStatisticsCacheKey, resp, consoleRevenueStatisticsCacheTTL)
	return resp, nil
}

func ordersStatistics(date string, total readmodel.OrdersTotal) dto.OrdersStatistics {
	return dto.OrdersStatistics{
		Date:               date,
		AmountTotal:        total.AmountTotal,
		NewOrderAmount:     total.NewOrderAmount,
		RenewalOrderAmount: total.RenewalOrderAmount,
	}
}

func ordersBreakdown(periods []readmodel.OrdersTotalWithDate) []dto.OrdersStatistics {
	list := make([]dto.OrdersStatistics, len(periods))
	for i, period := range periods {
		list[i] = ordersStatistics(period.Date, readmodel.OrdersTotal{
			AmountTotal:        period.AmountTotal,
			NewOrderAmount:     period.NewOrderAmount,
			RenewalOrderAmount: period.RenewalOrderAmount,
		})
	}
	return list
}

// mockRevenueStatistics is the demo deployment's canned revenue statistics.
func mockRevenueStatistics() *dto.RevenueStatisticsResponse {
	now := timeutil.Now()

	monthlyList, allList := demoSeries(now,
		func(ago int) int64 { return int64(25000 + ago*3000 + (ago%3)*8000) },
		func(ago int) int64 { return int64(1800000 + ago*200000 + (ago%2)*500000) },
		func(date string, amount int64) dto.OrdersStatistics {
			return dto.OrdersStatistics{
				Date:               date,
				AmountTotal:        amount,
				NewOrderAmount:     int64(float64(amount) * 0.68),
				RenewalOrderAmount: int64(float64(amount) * 0.32),
			}
		})

	return &dto.RevenueStatisticsResponse{
		Today: dto.OrdersStatistics{
			AmountTotal:        35888,
			NewOrderAmount:     22888,
			RenewalOrderAmount: 13000,
		},
		Monthly: dto.OrdersStatistics{
			AmountTotal:        888888,
			NewOrderAmount:     588888,
			RenewalOrderAmount: 300000,
			List:               monthlyList,
		},
		All: dto.OrdersStatistics{
			AmountTotal:        12888888,
			NewOrderAmount:     8588888,
			RenewalOrderAmount: 4300000,
			List:               allList,
		},
	}
}
