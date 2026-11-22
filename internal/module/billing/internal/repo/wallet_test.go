package repo

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	walletEntity "github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestWalletRepoUpdateBalanceFieldsWritesWalletTable(t *testing.T) {
	var logs bytes.Buffer
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DryRun:                 true,
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 gormlogger.New(log.New(&logs, "", 0), gormlogger.Config{LogLevel: gormlogger.Info}),
	})
	if err != nil {
		t.Fatalf("open gorm db: %v", err)
	}
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })

	conn := repository.ModuleConn{DB: db, Redis: redisClient}.Conn()
	err = NewWalletRepo(conn).UpdateBalanceFields(context.Background(), &walletEntity.Wallet{UserId: 42, Balance: 100, GiftAmount: 20})
	if err != nil {
		t.Fatalf("UpdateBalanceFields: %v", err)
	}
	sql := logs.String()
	for _, want := range []string{"UPDATE `user_wallet`", "`balance`=100", "`gift_amount`=20", "WHERE user_id = 42"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("SQL missing %q:\n%s", want, sql)
		}
	}
	for _, unwanted := range []string{"UPDATE `user` ", "`commission`"} {
		if strings.Contains(sql, unwanted) {
			t.Fatalf("SQL should not contain %q:\n%s", unwanted, sql)
		}
	}
}
