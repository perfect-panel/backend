package dashboard

import (
	"context"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/internal/readmodel"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

const consoleUserStatisticsCacheKey = "console:user_statistics"
const consoleUserStatisticsCacheTTL = 60 * time.Second

// QueryUserStatistics counts registrations and paying users for today, this
// month (with its daily breakdown) and all time (with the last months'
// breakdown). Every figure is optional: one whose source fails is logged and
// left at zero. The summary is cached for a minute.
func (s *Service) QueryUserStatistics(ctx context.Context) (*dto.UserStatisticsResponse, error) {
	if demoMode() {
		return mockUserStatistics(), nil
	}
	if cached, ok := readSnapshot[dto.UserStatisticsResponse](ctx, s.deps.Cache, consoleUserStatisticsCacheKey); ok {
		return cached, nil
	}

	resp := &dto.UserStatisticsResponse{}
	now := timeutil.Now()
	failed := func(source string, err error) bool {
		if err == nil {
			return false
		}
		logger.WithContext(ctx).Errorw("[QueryUserStatistics] "+source, logger.Field("error", err.Error()))
		return true
	}

	if count, err := s.deps.Users.QueryRegisterUserTotalByDate(ctx, now); !failed("registrations today", err) {
		resp.Today.Register = count
	}
	if newUsers, renewals, err := s.deps.Orders.QueryDateUserCounts(ctx, now); !failed("paying users today", err) {
		resp.Today.NewOrderUsers, resp.Today.RenewalOrderUsers = newUsers, renewals
	}
	if count, err := s.deps.Users.QueryRegisterUserTotalByMonthly(ctx, now); !failed("registrations this month", err) {
		resp.Monthly.Register = count
	}
	if newUsers, renewals, err := s.deps.Orders.QueryMonthlyUserCounts(ctx, now); !failed("paying users this month", err) {
		resp.Monthly.NewOrderUsers, resp.Monthly.RenewalOrderUsers = newUsers, renewals
	}
	if days, err := s.deps.Users.QueryDailyUserStatisticsList(ctx, now); !failed("daily user breakdown", err) {
		resp.Monthly.List = userBreakdown(days)
	}
	if count, err := s.deps.Users.QueryRegisterUserTotal(ctx); !failed("registrations", err) {
		resp.All.Register = count
	}
	if newUsers, renewals, err := s.deps.Orders.QueryTotalUserCounts(ctx); !failed("paying users", err) {
		resp.All.NewOrderUsers, resp.All.RenewalOrderUsers = newUsers, renewals
	}
	if months, err := s.deps.Users.QueryMonthlyUserStatisticsList(ctx, now); !failed("monthly user breakdown", err) {
		resp.All.List = userBreakdown(months)
	}

	storeSnapshot(ctx, s.deps.Cache, consoleUserStatisticsCacheKey, resp, consoleUserStatisticsCacheTTL)
	return resp, nil
}

func userBreakdown(periods []readmodel.UserStatisticsWithDate) []dto.UserStatistics {
	list := make([]dto.UserStatistics, len(periods))
	for i, period := range periods {
		list[i] = dto.UserStatistics{
			Date:              period.Date,
			Register:          period.Register,
			NewOrderUsers:     period.NewOrderUsers,
			RenewalOrderUsers: period.RenewalOrderUsers,
		}
	}
	return list
}

// mockUserStatistics is the demo deployment's canned user statistics.
func mockUserStatistics() *dto.UserStatisticsResponse {
	now := timeutil.Now()

	monthlyList, allList := demoSeries(now,
		func(ago int) int64 { return int64(18 + ago*3 + (ago%3)*8) },
		func(ago int) int64 { return int64(1800 + ago*200 + (ago%2)*500) },
		func(date string, registered int64) dto.UserStatistics {
			return dto.UserStatistics{
				Date:              date,
				Register:          registered,
				NewOrderUsers:     int64(float64(registered) * 0.65),
				RenewalOrderUsers: int64(float64(registered) * 0.35),
			}
		})

	return &dto.UserStatisticsResponse{
		Today: dto.UserStatistics{
			Register:          28,
			NewOrderUsers:     18,
			RenewalOrderUsers: 10,
		},
		Monthly: dto.UserStatistics{
			Register:          888,
			NewOrderUsers:     588,
			RenewalOrderUsers: 300,
			List:              monthlyList,
		},
		// The all-time period does not use the paying-user counts.
		All: dto.UserStatistics{
			Register: 18888,
			List:     allList,
		},
	}
}
