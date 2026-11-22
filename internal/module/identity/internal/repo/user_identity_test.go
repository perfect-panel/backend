package repo

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"gorm.io/gorm"
)

// Sign-in looks the account up by email through a cache that also remembers
// misses. Registration and the admin panel look the address up before they
// bind it, so the miss is remembered by the time the binding is written; the
// write must drop it, or the new account is "not found" until it expires.
func TestInsertUserAuthMethodsClearsTheRememberedEmailMiss(t *testing.T) {
	db, repo := newSQLiteUserRepo(t, "insert-auth-method-email-miss")
	ctx := context.Background()
	if _, err := repo.FindOneByEmail(ctx, "new@example.com"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("lookup before the account exists = %v, want not found", err)
	}
	u := &user.User{}
	if err := db.Create(u).Error; err != nil {
		t.Fatal(err)
	}

	if err := repo.InsertUserAuthMethods(ctx, &user.AuthMethods{UserId: u.Id, AuthType: "email", AuthIdentifier: "New@Example.com"}); err != nil {
		t.Fatal(err)
	}

	found, err := repo.FindOneByEmail(ctx, "new@example.com")
	if err != nil || found.Id != u.Id {
		t.Fatalf("lookup after the binding was created = %+v, %v; want user %d", found, err, u.Id)
	}
}

// Changing an email binding must drop the remembered miss of the new address
// as well as the cached account under the old one.
func TestUpdateUserAuthMethodsClearsTheRememberedEmailMiss(t *testing.T) {
	db, repo := newSQLiteUserRepo(t, "update-auth-method-email-miss")
	ctx := context.Background()
	u := &user.User{AuthMethods: []user.AuthMethods{{AuthType: "email", AuthIdentifier: "old@example.com"}}}
	if err := db.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindOneByEmail(ctx, "old@example.com"); err != nil {
		t.Fatalf("warm cache: %v", err)
	}
	if _, err := repo.FindOneByEmail(ctx, "new@example.com"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("lookup before the change = %v, want not found", err)
	}

	binding := u.AuthMethods[0]
	binding.AuthIdentifier = "new@example.com"
	if err := repo.UpdateUserAuthMethods(ctx, &binding); err != nil {
		t.Fatal(err)
	}

	found, err := repo.FindOneByEmail(ctx, "new@example.com")
	if err != nil || found.Id != u.Id {
		t.Fatalf("lookup under the new address = %+v, %v; want user %d", found, err, u.Id)
	}
	if _, err := repo.FindOneByEmail(ctx, "old@example.com"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("lookup under the old address = %v, want not found", err)
	}
}
