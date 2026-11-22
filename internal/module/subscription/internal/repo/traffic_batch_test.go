package repo

import (
	"context"
	"errors"
	"fmt"
	"testing"

	trafficEntity "github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/repository"
	"gorm.io/gorm"
)

// largeMinute is a minute of more subscriptions than one statement may name:
// at about five bound parameters each, 7000 of them exceed SQLite's 32766
// placeholders (MySQL and PostgreSQL allow 65535, reached near 13k).
const largeMinute = 7000

func seedLargeMinute(t *testing.T, f *writeFixture) []trafficEntity.SubscribeTrafficDelta {
	t.Helper()
	subs := make([]usersub.Subscribe, largeMinute)
	deltas := make([]trafficEntity.SubscribeTrafficDelta, largeMinute)
	for i := range subs {
		id := int64(i + 1)
		subs[i] = usersub.Subscribe{
			Id: id, UserId: id, SubscribeId: 1, Status: usersub.SubscribeStatusActive,
			Token: fmt.Sprintf("token-%d", id), UUID: fmt.Sprintf("uuid-%d", id),
			Download: 10, Upload: 1,
		}
		deltas[i] = trafficEntity.SubscribeTrafficDelta{SubscribeId: id, Download: id, Upload: 2 * id}
	}
	if err := f.db.CreateInBatches(&subs, 500).Error; err != nil {
		t.Fatal(err)
	}
	return deltas
}

func trafficSums(t *testing.T, db *gorm.DB) (download, upload int64) {
	t.Helper()
	var sums struct{ Download, Upload int64 }
	if err := db.Model(&usersub.Subscribe{}).Select("COALESCE(SUM(download), 0) AS download, COALESCE(SUM(upload), 0) AS upload").Scan(&sums).Error; err != nil {
		t.Fatal(err)
	}
	return sums.Download, sums.Upload
}

// A large minute is billed in chunks, each subscription by its own delta.
func TestBatchUpdateUserSubscribeWithTrafficChunksALargeMinute(t *testing.T) {
	f := newWriteFixture(t)
	deltas := seedLargeMinute(t, f)

	if err := f.subs().BatchUpdateUserSubscribeWithTraffic(context.Background(), deltas); err != nil {
		t.Fatalf("BatchUpdateUserSubscribeWithTraffic: %v", err)
	}

	const n = int64(largeMinute)
	download, upload := trafficSums(t, f.db)
	if wantDownload := n*10 + n*(n+1)/2; download != wantDownload {
		t.Fatalf("total download = %d, want %d", download, wantDownload)
	}
	if wantUpload := n + n*(n+1); upload != wantUpload {
		t.Fatalf("total upload = %d, want %d", upload, wantUpload)
	}
	for _, id := range []int64{1, batchUpdateSize, batchUpdateSize + 1, n} {
		got := f.load(t, id)
		if got.Download != 10+id || got.Upload != 1+2*id {
			t.Fatalf("subscription %d = %d/%d, want %d/%d", id, got.Download, got.Upload, 10+id, 1+2*id)
		}
	}
}

// The chunks run on the caller's connection: inside a transaction that rolls
// back, none of them stays applied.
func TestBatchUpdateUserSubscribeWithTrafficStaysInTheCallersTransaction(t *testing.T) {
	f := newWriteFixture(t)
	deltas := seedLargeMinute(t, f)
	abort := errors.New("abort after the update")

	err := f.db.Transaction(func(tx *gorm.DB) error {
		repo := NewUserSubscriptionRepo(repository.ModuleConn{DB: tx, Redis: f.rds}.Conn())
		if err := repo.BatchUpdateUserSubscribeWithTraffic(context.Background(), deltas); err != nil {
			return err
		}
		if download, _ := trafficSums(t, tx); download == largeMinute*10 {
			return errors.New("the update is not visible inside its own transaction")
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatalf("transaction = %v, want %v", err, abort)
	}
	if download, upload := trafficSums(t, f.db); download != largeMinute*10 || upload != largeMinute {
		t.Fatalf("a rolled-back minute left %d/%d billed", download, upload)
	}
}
