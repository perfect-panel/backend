package order

import (
	"testing"

	"github.com/perfect-panel/backend/internal/module/billing"
)

func TestDailyReportFormatsMinorUnits(t *testing.T) {
	for amount, want := range map[int64]string{0: "0.00", 1: "0.01", 58: "0.58", 1990: "19.90", 100000: "1000.00"} {
		if got := formatReportAmount(amount); got != want {
			t.Fatalf("formatReportAmount(%d) = %q, want %q", amount, got, want)
		}
	}
	lines := formatReportLines([]billing.DailyOrderReportLine{{Name: "Pro", Orders: 2, Amount: 3980}})
	if lines != "· Pro：2 单，39.80" {
		t.Fatalf("lines = %q", lines)
	}
	if empty := formatReportLines(nil); empty != "· 无" {
		t.Fatalf("empty breakdown = %q", empty)
	}
}
