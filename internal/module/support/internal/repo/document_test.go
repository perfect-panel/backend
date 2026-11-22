package repo

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/support/entity/document"
	"github.com/perfect-panel/backend/pkg/cache"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openSupportTestDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:support-repo-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	return db
}

// The admin detail must return hidden documents too; it used to preload a
// relation Document does not have, which failed every lookup.
func TestQueryDocumentDetailReturnsHiddenDocument(t *testing.T) {
	db := openSupportTestDB(t, &document.Document{})
	hidden := false
	row := &document.Document{Title: "draft", Content: "unpublished", Show: &hidden}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}

	got, err := NewDocumentRepo(cache.NewConn(db, nil)).QueryDocumentDetail(context.Background(), row.Id)
	if err != nil {
		t.Fatalf("QueryDocumentDetail: %v", err)
	}
	if got.Id != row.Id || got.Content != "unpublished" || got.Show == nil || *got.Show {
		t.Fatalf("detail = %+v, want the hidden document", got)
	}
}

// A missing document is not found: the admin detail used to answer an empty
// document with id 0 for it.
func TestQueryDocumentDetailReportsMissingDocument(t *testing.T) {
	db := openSupportTestDB(t, &document.Document{})

	_, err := NewDocumentRepo(cache.NewConn(db, nil)).QueryDocumentDetail(context.Background(), 404)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("QueryDocumentDetail of a missing document: %v, want gorm.ErrRecordNotFound", err)
	}
}
