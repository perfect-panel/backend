package console

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

const consoleRevenueStatisticsCacheKey = "console:revenue_statistics"
const consoleRevenueStatisticsCacheTTL = 60 * time.Second

type QueryRevenueStatisticsLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryRevenueStatisticsLogic Query revenue statistics
func NewQueryRevenueStatisticsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryRevenueStatisticsLogic {
	return &QueryRevenueStatisticsLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryRevenueStatisticsLogic) QueryRevenueStatistics() (resp *dto.RevenueStatisticsResponse, err error) {
	if strings.ToLower(os.Getenv("PPANEL_MODE")) == "demo" {
		return l.mockRevenueStatistics(), nil
	}

	// Try cache first
	cached, cacheErr := l.svcCtx.Redis.Get(l.ctx, consoleRevenueStatisticsCacheKey).Result()
	if cacheErr == nil && cached != "" {
		var result dto.RevenueStatisticsResponse
		if json.Unmarshal([]byte(cached), &result) == nil {
			return &result, nil
		}
	}

	var today, monthly, all dto.OrdersStatistics
	now := timeutil.Now()
	// Get today's revenue statistics
	todayData, err := l.svcCtx.Store.Order().QueryDateOrders(l.ctx, now)
	if err != nil {
		l.Errorw("[QueryRevenueStatisticsLogic] QueryDateOrders error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "QueryDateOrders error: %v", err)
	} else {
		today = dto.OrdersStatistics{
			AmountTotal:        todayData.AmountTotal,
			NewOrderAmount:     todayData.NewOrderAmount,
			RenewalOrderAmount: todayData.RenewalOrderAmount,
		}
	}
	// Get monthly's revenue statistics
	monthlyData, err := l.svcCtx.Store.Order().QueryMonthlyOrders(l.ctx, now)
	if err != nil {
		l.Errorw("[QueryRevenueStatisticsLogic] QueryMonthlyOrders error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "QueryMonthlyOrders error: %v", err)
	} else {
		monthly = dto.OrdersStatistics{
			AmountTotal:        monthlyData.AmountTotal,
			NewOrderAmount:     monthlyData.NewOrderAmount,
			RenewalOrderAmount: monthlyData.RenewalOrderAmount,
			List:               make([]dto.OrdersStatistics, 0),
		}
	}

	// Get monthly daily list for the current month (from 1st to current date)
	monthlyListData, err := l.svcCtx.Store.Order().QueryDailyOrdersList(l.ctx, now)
	if err != nil {
		l.Errorw("[QueryRevenueStatisticsLogic] QueryDailyOrdersList error", logger.Field("error", err.Error()))
		// Don't return error, just log it and continue with empty list
	} else {
		monthlyList := make([]dto.OrdersStatistics, len(monthlyListData))
		for i, data := range monthlyListData {
			monthlyList[i] = dto.OrdersStatistics{
				Date:               data.Date,
				AmountTotal:        data.AmountTotal,
				NewOrderAmount:     data.NewOrderAmount,
				RenewalOrderAmount: data.RenewalOrderAmount,
			}
		}
		monthly.List = monthlyList
	}

	// Get all revenue statistics
	allData, err := l.svcCtx.Store.Order().QueryTotalOrders(l.ctx)
	if err != nil {
		l.Errorw("[QueryRevenueStatisticsLogic] QueryTotalOrders error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "QueryTotalOrders error: %v", err)
	} else {
		all = dto.OrdersStatistics{
			AmountTotal:        allData.AmountTotal,
			NewOrderAmount:     allData.NewOrderAmount,
			RenewalOrderAmount: allData.RenewalOrderAmount,
			List:               make([]dto.OrdersStatistics, 0),
		}
	}

	// Get all monthly list for the past 6 months
	allListData, err := l.svcCtx.Store.Order().QueryMonthlyOrdersList(l.ctx, now)
	if err != nil {
		l.Errorw("[QueryRevenueStatisticsLogic] QueryMonthlyOrdersList error", logger.Field("error", err.Error()))
		// Don't return error, just log it and continue with empty list
	} else {
		allList := make([]dto.OrdersStatistics, len(allListData))
		for i, data := range allListData {
			allList[i] = dto.OrdersStatistics{
				Date:               data.Date,
				AmountTotal:        data.AmountTotal,
				NewOrderAmount:     data.NewOrderAmount,
				RenewalOrderAmount: data.RenewalOrderAmount,
			}
		}
		all.List = allList
	}

	resp = &dto.RevenueStatisticsResponse{
		Today:   today,
		Monthly: monthly,
		All:     all,
	}

	// Cache the result
	if data, marshalErr := json.Marshal(resp); marshalErr == nil {
		l.svcCtx.Redis.Set(l.ctx, consoleRevenueStatisticsCacheKey, data, consoleRevenueStatisticsCacheTTL)
	}

	return resp, nil
}

// mockRevenueStatistics is a mock function to simulate revenue statistics data.
func (l *QueryRevenueStatisticsLogic) mockRevenueStatistics() *dto.RevenueStatisticsResponse {
	now := timeutil.Now()

	// Generate daily data for the current month (from 1st to current date)
	monthlyList := make([]dto.OrdersStatistics, 7)
	for i := 0; i < 7; i++ {
		dayDate := now.AddDate(0, 0, -(6 - i))
		baseAmount := int64(25000 + ((6 - i) * 3000) + ((6-i)%3)*8000)
		monthlyList[i] = dto.OrdersStatistics{
			Date:               dayDate.Format("2006-01-02"),
			AmountTotal:        baseAmount,
			NewOrderAmount:     int64(float64(baseAmount) * 0.68),
			RenewalOrderAmount: int64(float64(baseAmount) * 0.32),
		}
	}

	// Generate monthly data for the past 6 months (oldest first)
	allList := make([]dto.OrdersStatistics, 6)
	for i := 0; i < 6; i++ {
		monthDate := now.AddDate(0, -(5 - i), 0)
		baseAmount := int64(1800000 + ((5 - i) * 200000) + ((5-i)%2)*500000)
		allList[i] = dto.OrdersStatistics{
			Date:               monthDate.Format("2006-01"),
			AmountTotal:        baseAmount,
			NewOrderAmount:     int64(float64(baseAmount) * 0.68),
			RenewalOrderAmount: int64(float64(baseAmount) * 0.32),
		}
	}

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
