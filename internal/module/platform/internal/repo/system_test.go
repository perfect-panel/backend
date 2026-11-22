package repo

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/platform/entity/system"
	"github.com/perfect-panel/backend/pkg/cache"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Every settings read is cached under a key that a write to its category
// invalidates: after an update, the next read returns the new value.
func TestSettingsReadsFollowWrites(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:system-settings-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&system.System{}); err != nil {
		t.Fatal(err)
	}
	server := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rds.Close() })
	systemRepo := NewSystemRepo(cache.NewConn(db, rds))
	ctx := context.Background()

	for category, read := range map[string]func(context.Context) ([]*system.System, error){
		"sms":         systemRepo.GetSmsConfig,
		"site":        systemRepo.GetSiteConfig,
		"email":       systemRepo.GetEmailConfig,
		"subscribe":   systemRepo.GetSubscribeConfig,
		"register":    systemRepo.GetRegisterConfig,
		"verify":      systemRepo.GetVerifyConfig,
		"server":      systemRepo.GetNodeConfig,
		"invite":      systemRepo.GetInviteConfig,
		"telegram":    systemRepo.GetTelegramConfig,
		"tos":         systemRepo.GetTosConfig,
		"currency":    systemRepo.GetCurrencyConfig,
		"verify_code": systemRepo.GetVerifyCodeConfig,
		"log":         systemRepo.GetLogConfig,
	} {
		key := category + "_setting"
		if err := systemRepo.UpdateValueByCategoryKey(ctx, category, key, "old"); err != nil {
			t.Fatalf("%s: seed: %v", category, err)
		}
		if rows, err := read(ctx); err != nil || len(rows) != 1 || rows[0].Value != "old" {
			t.Fatalf("%s: first read = %+v (err %v)", category, rows, err)
		}
		if err := systemRepo.UpdateValueByCategoryKey(ctx, category, key, "new"); err != nil {
			t.Fatalf("%s: update: %v", category, err)
		}
		rows, err := read(ctx)
		if err != nil || len(rows) != 1 || rows[0].Value != "new" || rows[0].Category != category {
			t.Fatalf("%s: read after update = %+v (err %v), want the new value", category, rows, err)
		}
	}
}

func TestSystemCategoryCacheKeys(t *testing.T) {
	keys := systemCategoryCacheKeys("site")
	for _, want := range []string{config.SiteConfigKey, config.GlobalConfigKey} {
		if !slices.Contains(keys, want) {
			t.Fatalf("site cache keys missing %q: %v", want, keys)
		}
	}
	if keys := systemCategoryCacheKeys("log"); len(keys) != 0 {
		t.Fatalf("uncached category should have no cache keys: %v", keys)
	}
}
