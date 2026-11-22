package repo

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	walletEntity "github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
)

func TestWithdrawalRepoUsesMigratedTableAndGuardsTransitions(t *testing.T) {
	db := openMemoryDB(t, "withdrawal-repo")
	if err := db.AutoMigrate(&walletEntity.Withdrawal{}); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasTable("withdrawals") || db.Migrator().HasTable("user_withdrawal") {
		t.Fatal("withdrawal entity is not mapped to the migrated withdrawals table")
	}
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	repo := NewWalletRepo(repository.ModuleConn{DB: db, Redis: redisClient}.Conn())

	pending := &walletEntity.Withdrawal{UserId: 9, Amount: 1200, Status: walletEntity.WithdrawalStatusPending}
	if err := repo.InsertWithdrawal(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	status := walletEntity.WithdrawalStatusPending
	list, total, err := repo.QueryWithdrawalList(context.Background(), 9, &status, 1, 20)
	if err != nil || total != 1 || len(list) != 1 || list[0].Id != pending.Id {
		t.Fatalf("query = %#v total=%d err=%v", list, total, err)
	}

	updated, err := repo.UpdateWithdrawalStatus(context.Background(), pending.Id, walletEntity.WithdrawalStatusPending, walletEntity.WithdrawalStatusApproved, "")
	if err != nil || !updated {
		t.Fatalf("first transition updated=%v err=%v", updated, err)
	}
	updated, err = repo.UpdateWithdrawalStatus(context.Background(), pending.Id, walletEntity.WithdrawalStatusPending, walletEntity.WithdrawalStatusRejected, "retry")
	if err != nil || updated {
		t.Fatalf("second transition updated=%v err=%v", updated, err)
	}
}
