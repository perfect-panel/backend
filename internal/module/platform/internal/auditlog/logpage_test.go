package auditlog

import (
	"context"
	"errors"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// pageRows answers every filter with rows, or fails with err.
type pageRows struct {
	rows   []*log.SystemLog
	err    error
	filter *log.FilterParams
}

var _ logFilter = (*pageRows)(nil)

func (r *pageRows) FilterSystemLog(_ context.Context, filter *log.FilterParams) ([]*log.SystemLog, int64, error) {
	r.filter = filter
	return r.rows, int64(len(r.rows)), r.err
}

func commissionView(row *log.SystemLog, content *log.Commission) dto.CommissionLog {
	return dto.CommissionLog{UserId: row.ObjectID, Amount: content.Amount}
}

func TestLogPageDecodesEveryRow(t *testing.T) {
	logs := &pageRows{rows: []*log.SystemLog{
		{Id: 1, ObjectID: 5, Content: `{"amount":100}`},
		{Id: 2, ObjectID: 5, Content: `{"amount":-40}`},
	}}
	params := &log.FilterParams{Type: log.TypeCommission.Uint8(), ObjectID: 5}
	total, list, err := logPage(context.Background(), logs, "commission", params, commissionView)
	if err != nil || total != 2 || len(list) != 2 || list[0].Amount != 100 || list[1].Amount != -40 || list[1].UserId != 5 {
		t.Fatalf("page = %d, %+v, %v", total, list, err)
	}
	if logs.filter != params {
		t.Fatal("the filter did not reach the log")
	}
}

// A page without rows has a nil list; the callers decide whether it answers
// null or an empty list.
func TestLogPageWithoutRowsIsNil(t *testing.T) {
	total, list, err := logPage(context.Background(), &pageRows{}, "commission", &log.FilterParams{}, commissionView)
	if err != nil || total != 0 || list != nil {
		t.Fatalf("empty page = %d, %#v, %v", total, list, err)
	}
}

// A corrupt row fails the page instead of disappearing from it; a failed
// query is a database error.
func TestLogPageReportsCorruptRowsAndQueryFailures(t *testing.T) {
	corrupt := &pageRows{rows: []*log.SystemLog{{Id: 7, Content: `{"amount":`}}}
	if _, _, err := logPage(context.Background(), corrupt, "commission", &log.FilterParams{}, commissionView); err == nil || xerr.CodeOf(err) != xerr.ERROR {
		t.Fatalf("corrupt row error = %v (code %d), want an ERROR", err, xerr.CodeOf(err))
	}

	cause := errors.New("database unavailable")
	failing := &pageRows{err: cause}
	if _, _, err := logPage(context.Background(), failing, "commission", &log.FilterParams{}, commissionView); !errors.Is(err, cause) || xerr.CodeOf(err) != xerr.DatabaseQueryError {
		t.Fatalf("query error = %v (code %d), want the cause as a database query error", err, xerr.CodeOf(err))
	}
}
