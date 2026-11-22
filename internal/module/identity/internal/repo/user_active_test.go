package repo

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// sqliteDatabases numbers the in-memory databases the tests open, so two
// tests, or two runs of one, never share one.
var sqliteDatabases atomic.Int64

func newSQLiteUserRepo(t *testing.T, name string) (*gorm.DB, *UserRepo) {
	t.Helper()
	// A shared-cache in-memory database is found again by its name for as
	// long as a connection to it stays open, so each call opens a database
	// of its own and closes it when the test ends.
	dsn := fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", name, sqliteDatabases.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&user.User{}, &user.AuthMethods{}); err != nil {
		t.Fatal(err)
	}
	// user_device shares the idx_user_id index name with user_auth_methods,
	// which SQLite keeps database-wide, so it is created by hand.
	if err := db.Exec("CREATE TABLE IF NOT EXISTS user_device (id integer primary key autoincrement, ip text, user_id integer, user_agent text, identifier text, online numeric, enabled numeric, created_at datetime, updated_at datetime)").Error; err != nil {
		t.Fatal(err)
	}
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	return db, NewUserRepo(repository.ModuleConn{DB: db, Redis: redisClient}.Conn(), repository.IdentityBridges{})
}

func TestFindEnabledUserIDsExcludesDeletedAndDisabledUsers(t *testing.T) {
	db, repo := newSQLiteUserRepo(t, "active-user-ids")

	enabled, disabled := true, false
	users := []*user.User{{Enable: &enabled}, {Enable: &disabled}, {Enable: &enabled}}
	for _, item := range users {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Delete(users[2]).Error; err != nil {
		t.Fatal(err)
	}
	ids, err := repo.FindEnabledUserIDs(context.Background(), []int64{users[0].Id, users[1].Id, users[2].Id})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != users[0].Id {
		t.Fatalf("enabled ids = %v, want [%d]", ids, users[0].Id)
	}
}

// Deleting a user only soft-deletes the user row, so the bindings stay
// behind; the lookup behind subscription notices must not resolve them.
func TestFindUserAuthMethodsByUserIdsSkipsDeletedUsers(t *testing.T) {
	db, repo := newSQLiteUserRepo(t, "auth-methods-deleted-users")

	// The telegram binding makes binding ids differ from user ids, so a join
	// leaking user columns into the result would show.
	live := &user.User{AuthMethods: []user.AuthMethods{
		{AuthType: "telegram", AuthIdentifier: "10001"},
		{AuthType: "email", AuthIdentifier: "live@example.com"},
	}}
	deleted := &user.User{AuthMethods: []user.AuthMethods{
		{AuthType: "email", AuthIdentifier: "deleted@example.com"},
	}}
	for _, item := range []*user.User{live, deleted} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Delete(deleted).Error; err != nil {
		t.Fatal(err)
	}

	methods, err := repo.FindUserAuthMethodsByUserIds(context.Background(), "email", []int64{live.Id, deleted.Id})
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 1 {
		t.Fatalf("methods = %d, want only the live user's email binding", len(methods))
	}
	want := live.AuthMethods[1]
	if got := methods[0]; got.Id != want.Id || got.UserId != live.Id || got.AuthIdentifier != want.AuthIdentifier {
		t.Fatalf("method = %+v, want %+v", got, want)
	}
}

func TestEmailRecipientsSkipDeletedUsers(t *testing.T) {
	db, repo := newSQLiteUserRepo(t, "email-recipients-deleted-users")

	live := &user.User{AuthMethods: []user.AuthMethods{{AuthType: "email", AuthIdentifier: "live@example.com"}}}
	deleted := &user.User{AuthMethods: []user.AuthMethods{{AuthType: "email", AuthIdentifier: "deleted@example.com"}}}
	for _, item := range []*user.User{live, deleted} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Delete(deleted).Error; err != nil {
		t.Fatal(err)
	}

	filter := &user.EmailRecipientFilter{Scope: 1}
	emails, err := repo.QueryEmailRecipients(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(emails) != 1 || emails[0] != "live@example.com" {
		t.Fatalf("recipients = %v, want [live@example.com]", emails)
	}
	count, err := repo.CountEmailRecipients(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("recipient count = %d, want 1", count)
	}
}

// A request saves what it changed from a snapshot taken when it started (the
// authenticated user). An administrator demoting, disabling, unbinding or
// deleting the account meanwhile must stick.
func TestUpdateColumnsKeepsConcurrentAdminChanges(t *testing.T) {
	db, repo := newSQLiteUserRepo(t, "update-columns-stale-snapshot")
	ctx := context.Background()
	enabled, admin := true, true
	u := &user.User{Enable: &enabled, IsAdmin: &admin, AuthMethods: []user.AuthMethods{
		{AuthType: "email", AuthIdentifier: "staff@example.com"},
		{AuthType: "telegram", AuthIdentifier: "424242"},
	}}
	if err := db.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err := repo.FindOne(ctx, u.Id)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Model(&user.User{}).Where("id = ?", u.Id).Updates(map[string]any{"enable": false, "is_admin": false}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("user_id = ? AND auth_type = ?", u.Id, "telegram").Delete(&user.AuthMethods{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateColumns(ctx, snapshot.Id, map[string]any{"enable_login_notify": true}); err != nil {
		t.Fatal(err)
	}

	var after user.User
	if err := db.First(&after, u.Id).Error; err != nil {
		t.Fatal(err)
	}
	var bindings int64
	db.Model(&user.AuthMethods{}).Where("user_id = ? AND auth_type = ?", u.Id, "telegram").Count(&bindings)
	if *after.Enable || *after.IsAdmin || bindings != 0 {
		t.Fatalf("stale save reverted the admin change: enable=%v is_admin=%v telegram bindings=%d", *after.Enable, *after.IsAdmin, bindings)
	}
	if after.EnableLoginNotify == nil || !*after.EnableLoginNotify {
		t.Fatalf("the requested column was not written")
	}

	if err := db.Delete(&user.User{}, u.Id).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateColumns(ctx, snapshot.Id, map[string]any{"rules": "[]"}); err != nil {
		t.Fatal(err)
	}
	var deleted user.User
	if err := db.Unscoped().First(&deleted, u.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !deleted.DeletedAt.Valid {
		t.Fatal("a save after deletion brought the account back")
	}
}

// Login and registration look users up by email through a cache; a batch
// delete must drop that entry like a single delete does.
func TestBatchDeleteUserClearsEmailLookupCache(t *testing.T) {
	db, repo := newSQLiteUserRepo(t, "batch-delete-email-cache")
	ctx := context.Background()
	u := &user.User{AuthMethods: []user.AuthMethods{{AuthType: "email", AuthIdentifier: "gone@example.com"}}}
	if err := db.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	if cached, err := repo.FindOneByEmail(ctx, "gone@example.com"); err != nil || cached.DeletedAt.Valid {
		t.Fatalf("warm cache: %+v, %v", cached, err)
	}

	if err := repo.BatchDeleteUser(ctx, []int64{u.Id}); err != nil {
		t.Fatal(err)
	}

	found, err := repo.FindOneByEmail(ctx, "gone@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !found.DeletedAt.Valid {
		t.Fatal("email lookup still serves the user from before the batch delete")
	}
}

// Device sign-in stores whatever identifier the client picks. That must not
// let someone register a device named after another person's email address
// (or Telegram id) and so keep its owner from ever binding it.
func TestAuthIdentifierIsUniqueWithinItsTypeOnly(t *testing.T) {
	db, repo := newSQLiteUserRepo(t, "auth-identifier-per-type")
	ctx := context.Background()
	squatter := &user.User{AuthMethods: []user.AuthMethods{{AuthType: "device", AuthIdentifier: "victim@example.com"}}}
	owner := &user.User{}
	for _, u := range []*user.User{squatter, owner} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := repo.InsertUserAuthMethods(ctx, &user.AuthMethods{UserId: owner.Id, AuthType: "email", AuthIdentifier: "victim@example.com"}); err != nil {
		t.Fatalf("owner could not bind an email a device was named after: %v", err)
	}
	if err := db.Create(&user.AuthMethods{UserId: squatter.Id, AuthType: "device", AuthIdentifier: "victim@example.com"}).Error; err == nil {
		t.Fatal("the same identifier was stored twice under one auth type")
	}
}

// Registration refuses a new spelling of a mailbox an active account already
// has; the lookup must find real aliases and nothing else.
func TestFindEmailAliasMatchesMailboxSpellings(t *testing.T) {
	db, repo := newSQLiteUserRepo(t, "email-alias-lookup")
	ctx := context.Background()
	for _, email := range []string{"johnsmith@gmail.com", "john.smith@example.com", "axb@example.com"} {
		if err := db.Create(&user.User{AuthMethods: []user.AuthMethods{{AuthType: "email", AuthIdentifier: email}}}).Error; err != nil {
			t.Fatal(err)
		}
	}
	gone := &user.User{AuthMethods: []user.AuthMethods{{AuthType: "email", AuthIdentifier: "gone@example.com"}}}
	if err := db.Create(gone).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(gone).Error; err != nil {
		t.Fatal(err)
	}

	for email, want := range map[string]string{
		"j.o.h.n.smith+x@googlemail.com": "johnsmith@gmail.com",
		"John.Smith+promo@example.com":   "john.smith@example.com",
		"johnsmith@gmail.com":            "", // the same spelling is the exact-match check's case
		"jane@gmail.com":                 "",
		"a_b@example.com":                "", // "_" is literal, not a LIKE wildcard
		"gone+again@example.com":         "", // the account is deleted
	} {
		alias, err := repo.FindEmailAlias(ctx, email)
		switch {
		case want == "" && !errors.Is(err, gorm.ErrRecordNotFound):
			t.Errorf("FindEmailAlias(%q) = %+v, %v, want not found", email, alias, err)
		case want != "" && (err != nil || alias.AuthIdentifier != want):
			t.Errorf("FindEmailAlias(%q) = %+v, %v, want %s", email, alias, err, want)
		}
	}
}
