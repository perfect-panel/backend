package adminorder

import (
	"context"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"gorm.io/gorm"
)

type dailyReportOrders struct {
	report *order.DailyReport
	date   time.Time
}

var _ Orders = (*dailyReportOrders)(nil)

func (r *dailyReportOrders) FindOne(context.Context, int64) (*order.Order, error) {
	return nil, gorm.ErrRecordNotFound
}

func (r *dailyReportOrders) QueryOrderListByPage(context.Context, int, int, uint8, int64, int64, string) (int64, []*order.Details, error) {
	return 0, nil, nil
}

func (r *dailyReportOrders) QueryDailyReport(_ context.Context, date time.Time) (*order.DailyReport, error) {
	r.date = date
	return r.report, nil
}

type dailyReportPlans struct {
	plans map[int64]*subscribe.Subscribe
}

func (r *dailyReportPlans) FindOne(_ context.Context, id int64) (*subscribe.Subscribe, error) {
	plan, ok := r.plans[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return plan, nil
}

// Plan ids are meaningless in a report an operator reads, so the breakdown
// carries names; a plan-less group (balance top-ups) and an unreadable plan
// both stay in the report rather than dropping revenue from the totals.
func TestDailyReportLabelsThePlanBreakdown(t *testing.T) {
	orders := &dailyReportOrders{report: &order.DailyReport{
		Date:   time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC),
		Orders: 5,
		Amount: 12345,
		ByPlan: []order.DailyBreakdown{
			{Id: 9, Orders: 3, Amount: 9000},
			{Id: 0, Orders: 1, Amount: 2000},
			{Id: 404, Orders: 1, Amount: 1345},
		},
		ByMethod: []order.DailyBreakdown{
			{Name: "EPay", Orders: 4, Amount: 10345},
			{Name: "", Orders: 1, Amount: 2000},
		},
	}}
	svc := NewService(Deps{Orders: orders, Plans: &dailyReportPlans{
		plans: map[int64]*subscribe.Subscribe{9: {Id: 9, Name: "Pro 月付"}},
	}})

	report, err := svc.DailyReport(context.Background(), orders.report.Date)
	if err != nil {
		t.Fatalf("DailyReport error = %v", err)
	}
	if report.Orders != 5 || report.Amount != 12345 {
		t.Fatalf("totals = %d orders / %d amount", report.Orders, report.Amount)
	}

	wantPlans := []string{"Pro 月付", unnamedPlanLabel, "#404"}
	if len(report.ByPlan) != len(wantPlans) {
		t.Fatalf("plan rows = %d, want %d", len(report.ByPlan), len(wantPlans))
	}
	for i, want := range wantPlans {
		if report.ByPlan[i].Name != want {
			t.Fatalf("plan[%d] = %q, want %q", i, report.ByPlan[i].Name, want)
		}
	}

	if report.ByMethod[0].Name != "EPay" || report.ByMethod[1].Name != unnamedPlanLabel {
		t.Fatalf("method rows = %+v", report.ByMethod)
	}
}

// A deployment with no plan reader still reports totals; only the labels
// degrade.
func TestDailyReportWithoutPlanReader(t *testing.T) {
	orders := &dailyReportOrders{report: &order.DailyReport{
		Orders: 1,
		Amount: 500,
		ByPlan: []order.DailyBreakdown{{Id: 9, Orders: 1, Amount: 500}},
	}}
	svc := NewService(Deps{Orders: orders})

	report, err := svc.DailyReport(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("DailyReport error = %v", err)
	}
	if len(report.ByPlan) != 1 || report.ByPlan[0].Name != unnamedPlanLabel {
		t.Fatalf("plan rows = %+v", report.ByPlan)
	}
}
