package order

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/module/billing"
	"github.com/perfect-panel/backend/internal/module/notification"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// DailyOrderReportHandler posts the previous day's settlement summary into
// the admin group's notification topic. It reports yesterday because it
// runs after midnight, when the day it summarises is complete.
type DailyOrderReportHandler struct {
	deps Dependencies
}

// NewDailyOrderReportHandler builds the report over the billing and
// notification facades.
func NewDailyOrderReportHandler(deps Dependencies) *DailyOrderReportHandler {
	return &DailyOrderReportHandler{deps: deps}
}

func (h *DailyOrderReportHandler) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	log := logger.WithContext(ctx)
	if !h.deps.telegramConfig().EnableNotify {
		return nil
	}

	date := timeutil.Now().AddDate(0, 0, -1)
	report, err := h.deps.Billing.DailyOrderReport(ctx, date)
	if err != nil {
		return err
	}

	text, err := notification.RenderTelegramMarkdown(notification.AdminOrderDaily, map[string]string{
		"Date":      report.Date.Format(time.DateOnly),
		"Orders":    fmt.Sprintf("%d", report.Orders),
		"Amount":    formatReportAmount(report.Amount),
		"Subscribe": formatReportLines(report.ByPlan),
		"Payment":   formatReportLines(report.ByMethod),
	})
	if err != nil {
		log.Errorw("[DailyOrderReport] render template failed", logger.Field("error", err.Error()))
		return err
	}

	// The admin group's notification topic is the only administrator
	// channel; without a usable group the report is skipped, not retried.
	if err := h.deps.Notification.NotifyAdminsTelegram(ctx, text); err != nil {
		log.Infow("[DailyOrderReport] report skipped", logger.Field("reason", err.Error()))
		return nil
	}
	log.Infow("[DailyOrderReport] report delivered",
		logger.Field("date", report.Date.Format(time.DateOnly)),
		logger.Field("orders", report.Orders),
	)
	return nil
}

// formatReportLines renders a breakdown as Markdown list items. An empty
// breakdown still needs a line, otherwise the section reads as broken.
func formatReportLines(lines []billing.DailyOrderReportLine) string {
	if len(lines) == 0 {
		return "· 无"
	}
	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		rendered = append(rendered, fmt.Sprintf("· %s：%d 单，%s",
			line.Name, line.Orders, formatReportAmount(line.Amount)))
	}
	return strings.Join(rendered, "\n")
}

// formatReportAmount converts minor units to the display amount.
func formatReportAmount(amount int64) string {
	return billing.FormatAmount(amount)
}
