package repo

import (
	"context"
	"fmt"
	"strings"
	"testing"

	logEntity "github.com/perfect-panel/backend/internal/module/platform/entity/log"
)

// A content field filter matches the field's value itself, in the
// database's JSON extraction: 12 is not 120 or 1200, which a LIKE pattern
// would also find.
func TestFilterSystemLogContentInt64MatchesTheFieldExactly(t *testing.T) {
	db := openTestDB(t, "content-int64", &logEntity.SystemLog{})
	for _, id := range []int64{12, 120, 1200, 12} {
		if err := db.Create(&logEntity.SystemLog{Type: 20, ObjectID: 7, Date: "2026-09-01", Content: fmt.Sprintf(`{"token":"t","user_subscribe_id":%d}`, id)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows, total, err := NewLogRepo(db).FilterSystemLog(context.Background(), &logEntity.FilterParams{Page: 1, Size: 10, Type: 20, ContentInt64: map[string]int64{"user_subscribe_id": 12}})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("total = %d, rows = %d, want the two of subscription 12", total, len(rows))
	}
	for _, row := range rows {
		if !strings.Contains(row.Content, `"user_subscribe_id":12}`) {
			t.Fatalf("row %s does not belong to subscription 12", row.Content)
		}
	}
	// A field name that is not one is refused rather than spliced into SQL.
	if _, _, err := NewLogRepo(db).FilterSystemLog(context.Background(), &logEntity.FilterParams{Page: 1, Size: 10, ContentInt64: map[string]int64{"id') OR 1=1 --": 1}}); err == nil {
		t.Fatal("an unsafe field name was accepted")
	}
}

// Update rewrites the entry's content and nothing else: the type, date and
// object of the row stay as stored, whatever the caller's copy says.
func TestUpdateRewritesOnlyTheContent(t *testing.T) {
	db := openTestDB(t, "log-update", &logEntity.SystemLog{})
	row := &logEntity.SystemLog{Type: 10, ObjectID: 7, Date: "2026-09-01", Content: `{"status":0}`}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	stale := *row
	stale.Type, stale.ObjectID, stale.Date, stale.Content = 99, 8, "2020-01-01", `{"status":1}`
	if err := NewLogRepo(db).Update(context.Background(), &stale); err != nil {
		t.Fatal(err)
	}
	var stored logEntity.SystemLog
	if err := db.First(&stored, row.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Type != 10 || stored.ObjectID != 7 || stored.Date != "2026-09-01" || stored.Content != `{"status":1}` {
		t.Fatalf("stored = %+v, want only the content rewritten", stored)
	}
}
