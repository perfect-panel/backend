package log_test

import (
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
)

func TestLogDateRangeValidation(t *testing.T) {
	for _, tc := range []struct {
		date  string
		valid bool
	}{
		{"", true}, {"2024-02-29", true}, {"2026-02-29", false}, {"2026-13-01", false}, {"2026-1-01", false}, {"not-a-date", false},
	} {
		for _, start := range []bool{true, false} {
			req := dto.FilterLoginLogRequest{FilterLogParams: dto.FilterLogParams{Page: 1, Size: 10}}
			if start {
				req.StartDate = tc.date
			} else {
				req.EndDate = tc.date
			}
			err := validation.Validate(&req)
			if (err == nil) != tc.valid {
				t.Fatalf("date=%q start=%v err=%v", tc.date, start, err)
			}
		}
	}
}
