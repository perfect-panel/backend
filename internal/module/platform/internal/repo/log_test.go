package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	logEntity "github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestInsertAttachesRequestMetadataToAuditContent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:system-log-metadata-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&logEntity.SystemLog{}); err != nil {
		t.Fatal(err)
	}

	ctx := requestmeta.With(context.Background(), requestmeta.Metadata{
		ClientIP: "203.0.113.9", UserAgent: "RiskClient/1.0", ActorID: 42,
		IPMetadata: requestmeta.IPMetadata{IPCountryCode: "SG", IPCountry: "Singapore", IPCity: "Singapore", IPASN: 64500, IPASOrganization: "Example Network"},
	})
	row := &logEntity.SystemLog{Type: logEntity.TypeBalance.Uint8(), Date: "2026-08-29", Content: `{"amount":100}`}
	if err := NewLogRepo(db).Insert(ctx, row); err != nil {
		t.Fatal(err)
	}

	var stored logEntity.SystemLog
	if err := db.First(&stored, row.Id).Error; err != nil {
		t.Fatal(err)
	}
	var content map[string]any
	if err := json.Unmarshal([]byte(stored.Content), &content); err != nil {
		t.Fatal(err)
	}
	if content["client_ip"] != "203.0.113.9" || content["user_agent"] != "RiskClient/1.0" || content["actor_id"] != float64(42) || content["amount"] != float64(100) ||
		content["ip_country_code"] != "SG" || content["ip_country"] != "Singapore" || content["ip_city"] != "Singapore" || content["ip_asn"] != float64(64500) || content["ip_as_organization"] != "Example Network" {
		t.Fatalf("stored audit content = %#v", content)
	}
}

func TestAttachRequestMetadataPreservesExplicitLoginMetadata(t *testing.T) {
	ctx := requestmeta.With(context.Background(), requestmeta.New("203.0.113.9", "middleware-agent"))
	row := &logEntity.SystemLog{Type: logEntity.TypeLogin.Uint8(), Content: `{"login_ip":"192.0.2.1","user_agent":"handler-agent"}`}
	attachRequestMetadata(ctx, row)

	var content map[string]any
	if err := json.Unmarshal([]byte(row.Content), &content); err != nil {
		t.Fatal(err)
	}
	if content["login_ip"] != "192.0.2.1" || content["user_agent"] != "handler-agent" {
		t.Fatalf("explicit login metadata was overwritten: %#v", content)
	}
	if _, exists := content["client_ip"]; exists {
		t.Fatalf("login metadata was duplicated: %#v", content)
	}
}

func TestDeleteBeforePreservesFinancialLedgers(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:system-log-retention-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&logEntity.SystemLog{}); err != nil {
		t.Fatal(err)
	}

	oldDate := "2025-01-01"
	rows := []*logEntity.SystemLog{
		{Type: logEntity.TypeEmailMessage.Uint8(), Date: oldDate, Content: `{}`},
		{Type: logEntity.TypeLogin.Uint8(), Date: oldDate, Content: `{}`},
		{Type: logEntity.TypeBalance.Uint8(), Date: oldDate, Content: `{}`},
		{Type: logEntity.TypeCommission.Uint8(), Date: oldDate, Content: `{}`},
		{Type: logEntity.TypeGift.Uint8(), Date: oldDate, Content: `{}`},
		{Type: 99, Date: oldDate, Content: `{}`},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	repo := NewLogRepo(db)
	if err := repo.DeleteBefore(context.Background(), time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	var remaining []logEntity.SystemLog
	if err := db.Order("type ASC").Find(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	want := []uint8{logEntity.TypeBalance.Uint8(), logEntity.TypeCommission.Uint8(), logEntity.TypeGift.Uint8(), 99}
	if len(remaining) != len(want) {
		t.Fatalf("remaining types = %v, want %v", logTypes(remaining), want)
	}
	for i, typ := range want {
		if remaining[i].Type != typ {
			t.Fatalf("remaining types = %v, want %v", logTypes(remaining), want)
		}
	}
}

func logTypes(rows []logEntity.SystemLog) []uint8 {
	result := make([]uint8, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.Type)
	}
	return result
}

func TestFilterSystemLogDateRanges(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&logEntity.SystemLog{}); err != nil {
		t.Fatal(err)
	}
	for _, date := range []string{"2025-12-31", "2026-01-01", "2026-01-02", "2026-02-01"} {
		if err := db.Create(&logEntity.SystemLog{Type: 30, Date: date, Content: `{}`}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, date, start, end string
		want                   int64
	}{
		{"unfiltered", "", "", "", 4},
		{"legacy", "2026-01-01", "", "", 1},
		{"inclusive across year", "", "2025-12-31", "2026-01-01", 2},
		{"same day", "", "2026-01-01", "2026-01-01", 1},
		{"start only overrides legacy", "2025-12-31", "2026-01-01", "", 3},
		{"end only overrides legacy", "2026-02-01", "", "2026-01-01", 2},
		{"range overrides legacy", "2026-02-01", "2026-01-01", "2026-01-02", 2},
		{"reversed", "", "2026-02-01", "2026-01-01", 0},
		{"empty", "", "2027-01-01", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filter := &logEntity.FilterParams{Page: 1, Size: 1, Type: 30, Data: tc.date, StartDate: tc.start, EndDate: tc.end}
			rows, total, err := NewLogRepo(db).FilterSystemLog(context.Background(), filter)
			if err != nil || total != tc.want || len(rows) != min(1, int(tc.want)) {
				t.Fatalf("rows=%v total=%d err=%v", rows, total, err)
			}
			filter.Page = int(tc.want) + 1
			rows, total, err = NewLogRepo(db).FilterSystemLog(context.Background(), filter)
			if err != nil || total != tc.want || len(rows) != 0 {
				t.Fatalf("empty page: rows=%v total=%d err=%v", rows, total, err)
			}
		})
	}
}
