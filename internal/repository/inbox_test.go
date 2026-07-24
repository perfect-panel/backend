package repository

import (
	"context"
	"testing"

	"github.com/perfect-panel/server/internal/model/entity/inbox"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newInboxTestRepo(t *testing.T, name string) InboxRepo {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&inbox.Record{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return newInboxRepo(db)
}

func TestInboxFindReturnsNilWhenUnprocessed(t *testing.T) {
	repo := newInboxTestRepo(t, "inbox-unprocessed")

	record, err := repo.Find(context.Background(), "subscription.fulfillment", "order-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if record != nil {
		t.Fatalf("expected nil for unprocessed key, got %+v", record)
	}
}

func TestInboxInsertThenFindRoundTrips(t *testing.T) {
	repo := newInboxTestRepo(t, "inbox-roundtrip")
	ctx := context.Background()

	if err := repo.Insert(ctx, "identity.guest_account", "order-2", "42"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	record, err := repo.Find(ctx, "identity.guest_account", "order-2")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if record == nil || record.Result != "42" {
		t.Fatalf("unexpected record: %+v", record)
	}
	if record.ProcessedAt.IsZero() {
		t.Fatal("ProcessedAt must be stamped")
	}
}

func TestInboxInsertRejectsDuplicateDelivery(t *testing.T) {
	repo := newInboxTestRepo(t, "inbox-duplicate")
	ctx := context.Background()

	if err := repo.Insert(ctx, "subscription.fulfillment", "order-3", ""); err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	if err := repo.Insert(ctx, "subscription.fulfillment", "order-3", ""); err == nil {
		t.Fatal("duplicate insert must fail so the racing transaction rolls back")
	}
	// A different consumer processing the same event is a distinct step.
	if err := repo.Insert(ctx, "identity.commission", "order-3", ""); err != nil {
		t.Fatalf("different consumer must be independent: %v", err)
	}
}
