package identity

import (
	"context"
	"fmt"
	"testing"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/identitytest"
)

// accountsFixture caches one account with an email binding under its id,
// state and email keys.
type accountsFixture struct {
	*identitytest.Env
	accounts accounts
	user     *user.User
	keys     map[string]string
}

func newAccountsFixture(t *testing.T) *accountsFixture {
	t.Helper()
	env := identitytest.New(t)
	enabled := true
	u := &user.User{Enable: &enabled}
	if err := env.DB.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	if err := env.DB.Create(&user.AuthMethods{UserId: u.Id, AuthType: "email", AuthIdentifier: "cached@example.com", Verified: true}).Error; err != nil {
		t.Fatal(err)
	}
	return &accountsFixture{
		Env: env,
		accounts: newAccounts(Deps{
			Users: env.Store.User(), UserAuths: env.Store.UserAuth(), Devices: env.Store.UserDevice(),
			Cache: env.Store.UserCache(), Auths: env.Store.Auth(),
		}),
		user: u,
		keys: map[string]string{
			"id":    fmt.Sprintf("cache:user:id:%d", u.Id),
			"state": fmt.Sprintf("cache:user:state:%d", u.Id),
			"email": "cache:user:email:v2:cached@example.com",
		},
	}
}

// cache reads the account by id, state and email, so all three keys hold it,
// and returns the account as loaded.
func (f *accountsFixture) cache(t *testing.T) *user.User {
	t.Helper()
	ctx := context.Background()
	loaded, err := f.accounts.FindUser(ctx, f.user.Id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.accounts.FindAccountState(ctx, f.user.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Store.User().FindOneByEmail(ctx, "cached@example.com"); err != nil {
		t.Fatal(err)
	}
	for name, key := range f.keys {
		if !f.Mini.Exists(key) {
			t.Fatalf("the %s key %s holds nothing", name, key)
		}
	}
	return loaded
}

func (f *accountsFixture) assertCached(t *testing.T, want map[string]bool) {
	t.Helper()
	for name, cached := range want {
		if got := f.Mini.Exists(f.keys[name]); got != cached {
			t.Fatalf("%s key cached = %t, want %t", name, got, cached)
		}
	}
}

// Clearing by id drops what an id identifies; the email-keyed entry stays,
// as it did for the callers that passed &user.User{Id: id}.
func TestClearUserCacheDropsTheIdKeyedEntries(t *testing.T) {
	f := newAccountsFixture(t)
	f.cache(t)
	if err := f.accounts.ClearUserCache(context.Background(), f.user.Id); err != nil {
		t.Fatal(err)
	}
	f.assertCached(t, map[string]bool{"id": false, "state": false, "email": true})
}

// Clearing a loaded account drops every entry it derives, its email-keyed
// one included.
func TestClearUserCacheOfDropsEveryEntryOfTheLoadedAccount(t *testing.T) {
	f := newAccountsFixture(t)
	loaded := f.cache(t)
	if err := f.accounts.ClearUserCacheOf(context.Background(), loaded); err != nil {
		t.Fatal(err)
	}
	f.assertCached(t, map[string]bool{"id": false, "state": false, "email": false})
}
