package adminorder

import (
	"context"
	"strconv"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// DailyReportLine is one plan or payment-method row of the daily report.
type DailyReportLine struct {
	Name   string
	Orders int64
	Amount int64
}

// DailyReport summarises the orders settled on one day. Amounts stay in minor
// units; formatting belongs to whoever renders the report.
type DailyReport struct {
	Date     time.Time
	Orders   int64
	Amount   int64
	ByPlan   []DailyReportLine
	ByMethod []DailyReportLine
}

// unnamedPlanLabel labels rows whose orders carry no plan, such as balance
// top-ups, and payment groups with an empty method.
const unnamedPlanLabel = "未关联套餐"

// DailyReport totals a day's settled orders and resolves plan ids to names so
// the caller only has to format the result.
func (s *Service) DailyReport(ctx context.Context, date time.Time) (*DailyReport, error) {
	raw, err := s.deps.Orders.QueryDailyReport(ctx, date)
	if err != nil {
		logger.WithContext(ctx).Errorw("query daily order report failed",
			logger.Field("error", err.Error()),
			logger.Field("date", date.Format(time.DateOnly)),
		)
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "query daily order report")
	}

	report := &DailyReport{
		Date:     raw.Date,
		Orders:   raw.Orders,
		Amount:   raw.Amount,
		ByPlan:   make([]DailyReportLine, 0, len(raw.ByPlan)),
		ByMethod: make([]DailyReportLine, 0, len(raw.ByMethod)),
	}
	for _, row := range raw.ByPlan {
		report.ByPlan = append(report.ByPlan, DailyReportLine{
			Name:   s.planName(ctx, row),
			Orders: row.Orders,
			Amount: row.Amount,
		})
	}
	for _, row := range raw.ByMethod {
		name := row.Name
		if name == "" {
			name = unnamedPlanLabel
		}
		report.ByMethod = append(report.ByMethod, DailyReportLine{
			Name:   name,
			Orders: row.Orders,
			Amount: row.Amount,
		})
	}
	return report, nil
}

// planName resolves a grouped plan id. Recharges and other plan-less orders
// group under a fixed label, and an unreadable plan degrades to its id rather
// than failing the whole report.
func (s *Service) planName(ctx context.Context, row order.DailyBreakdown) string {
	if row.Id == 0 || s.deps.Plans == nil {
		return unnamedPlanLabel
	}
	plan, err := s.deps.Plans.FindOne(ctx, row.Id)
	if err != nil || plan == nil || plan.Name == "" {
		logger.WithContext(ctx).Infow("daily report: plan name unavailable",
			logger.Field("subscribe_id", row.Id),
		)
		return "#" + strconv.FormatInt(row.Id, 10)
	}
	return plan.Name
}
