package profile

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/perfect-panel/backend/pkg/xerr"
)

// The chat the bot redeemed an account's bind token in becomes the account's
// verified Telegram binding, and the account's cached row is dropped so the
// binding shows at once.
func TestBindTelegramChatStoresAVerifiedBinding(t *testing.T) {
	f := newBindFixture(t)
	u := f.user(t)
	ctx := context.Background()
	if _, err := f.Store.User().FindOne(ctx, u.Id); err != nil {
		t.Fatal(err)
	}
	cached := fmt.Sprintf("cache:user:id:%d", u.Id)
	if !f.Mini.Exists(cached) {
		t.Fatalf("the account was not cached under %s", cached)
	}

	if err := f.svc.BindTelegramChat(ctx, u.Id, "1001"); err != nil {
		t.Fatalf("BindTelegramChat: %v", err)
	}
	methods := f.Identities(t, u.Id)
	if len(methods) != 1 || methods[0].AuthType != "telegram" || methods[0].AuthIdentifier != "1001" || !methods[0].Verified {
		t.Fatalf("bindings = %+v, want one verified telegram binding for chat 1001", methods)
	}
	if f.Mini.Exists(cached) {
		t.Fatal("the cached account survived the binding")
	}
}

// An account holds one Telegram binding: a second chat is refused, also
// when two redemptions race, so a leaked deep link opened twice cannot leave
// two chats signing in to the account.
func TestBindTelegramChatBindsOneChatPerAccount(t *testing.T) {
	f := newBindFixture(t)
	u := f.user(t)
	ctx := context.Background()
	if err := f.svc.BindTelegramChat(ctx, u.Id, "1001"); err != nil {
		t.Fatal(err)
	}
	assertCode(t, f.svc.BindTelegramChat(ctx, u.Id, "1002"), xerr.UserExist)
	if methods := f.Identities(t, u.Id); len(methods) != 1 || methods[0].AuthIdentifier != "1001" {
		t.Fatalf("bindings = %+v, want the first chat alone", methods)
	}

	racer := f.user(t)
	const redemptions = 8
	outcomes := make([]error, redemptions)
	var wg sync.WaitGroup
	for i := range outcomes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outcomes[i] = f.svc.BindTelegramChat(ctx, racer.Id, fmt.Sprintf("20%02d", i))
		}(i)
	}
	wg.Wait()
	bound := 0
	for _, err := range outcomes {
		if err == nil {
			bound++
			continue
		}
		if xerr.CodeOf(err) != xerr.UserExist {
			t.Fatalf("unexpected outcome: %v", err)
		}
	}
	if methods := f.Identities(t, racer.Id); bound != 1 || len(methods) != 1 {
		t.Fatalf("%d redemptions bound, %d bindings stored; want one of each", bound, len(methods))
	}
}

// A chat holds one binding: the store refuses to bind it to a second account.
func TestBindTelegramChatRefusesABoundChat(t *testing.T) {
	f := newBindFixture(t)
	first, second := f.user(t), f.user(t)
	ctx := context.Background()
	if err := f.svc.BindTelegramChat(ctx, first.Id, "1001"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.BindTelegramChat(ctx, second.Id, "1001"); err == nil {
		t.Fatal("the bound chat was bound to a second account")
	}
	if methods := f.Identities(t, second.Id); len(methods) != 0 {
		t.Fatalf("second account bindings = %+v, want none", methods)
	}
}
