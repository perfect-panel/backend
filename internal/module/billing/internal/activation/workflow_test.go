package activation

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/identity"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var errGuestOrderBind = errors.New("order binding failed")

type workflowOrders struct {
	bindFails bool
	boundUser int64
}

var _ WorkflowOrders = (*workflowOrders)(nil)

func (r *workflowOrders) FindOneByOrderNo(context.Context, string) (*order.Order, error) {
	return nil, gorm.ErrRecordNotFound
}

func (r *workflowOrders) Update(_ context.Context, o *order.Order) error {
	if r.bindFails {
		return errGuestOrderBind
	}
	r.boundUser = o.UserId
	return nil
}

type workflowGuests struct {
	id      int64
	creates int
	command identity.GuestAccountCommand
}

func (g *workflowGuests) FindGuestAccount(context.Context, string) (int64, bool, error) {
	return g.id, g.id != 0, nil
}
func (g *workflowGuests) EnsureGuestAccount(_ context.Context, command identity.GuestAccountCommand) (int64, error) {
	g.creates++
	g.id = 11
	g.command = command
	return g.id, nil
}

func TestGuestBindingRetryWorksAfterLegacyCacheExpires(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	legacy := order.TemporaryOrderInfo{OrderNo: "legacy-order", AuthType: "email", Identifier: "guest@example.test", PasswordHash: "existing-hash", InviteCode: "referral"}
	payload, err := legacy.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf(order.TempOrderCacheKey, legacy.OrderNo)
	if err := client.Set(ctx, key, payload, 0).Err(); err != nil {
		t.Fatal(err)
	}
	orders := &workflowOrders{bindFails: true}
	guests := &workflowGuests{}
	workflow := NewWorkflow(WorkflowDeps{Orders: orders, GuestAccounts: guests, LegacyGuestCache: client}, nil)
	if err := workflow.ensureGuestAccount(ctx, &order.Order{OrderNo: legacy.OrderNo}); !errors.Is(err, errGuestOrderBind) {
		t.Fatalf("expected failed billing bind, got %v", err)
	}
	if guests.creates != 1 || guests.command.PasswordHash != legacy.PasswordHash || guests.command.InviteCode != legacy.InviteCode {
		t.Fatal("identity did not receive the historical checkout data")
	}
	if err := client.Del(ctx, key).Err(); err != nil {
		t.Fatal(err)
	}
	orders.bindFails = false
	if err := workflow.ensureGuestAccount(ctx, &order.Order{OrderNo: legacy.OrderNo}); err != nil {
		t.Fatalf("committed identity should be reusable without Redis checkout data: %v", err)
	}
	if orders.boundUser != 11 || guests.creates != 1 {
		t.Fatal("retry created a second identity or bound the wrong user")
	}
}

func TestDurableGuestSnapshotDoesNotRequireRedis(t *testing.T) {
	orders := &workflowOrders{}
	guests := &workflowGuests{}
	workflow := NewWorkflow(WorkflowDeps{Orders: orders, GuestAccounts: guests}, nil)
	err := workflow.ensureGuestAccount(context.Background(), &order.Order{
		OrderNo: "durable-order", GuestAuthType: "email", GuestIdentifier: "guest@example.test", GuestPasswordHash: "durable-hash", GuestInviteCode: "referral",
	})
	if err != nil {
		t.Fatal(err)
	}
	if orders.boundUser != 11 || guests.command.PasswordHash != "durable-hash" || guests.command.LegacyPassword != "" {
		t.Fatal("durable checkout data was not used")
	}
}

// aliasIdentities is the identity port of the mailbox-alias check, answering
// with one alias binding or none.
type aliasIdentities struct{ alias *user.AuthMethods }

func (aliasIdentities) FindUserAuthMethodByOpenID(context.Context, string, string) (*user.AuthMethods, error) {
	return nil, gorm.ErrRecordNotFound
}

func (a aliasIdentities) FindEmailAlias(context.Context, string) (*user.AuthMethods, error) {
	if a.alias == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return a.alias, nil
}

// A mailbox that gained an account under another spelling since the purchase
// must not get a second account; the stage reports the account the identity
// belongs to, so the order is refunded to it.
func TestGuestAccountStageRefusesAMailboxAlias(t *testing.T) {
	ctx := context.Background()
	guestOrder := &order.Order{OrderNo: "aliased", GuestAuthType: "email", GuestIdentifier: "gu.est@gmail.com", GuestPasswordHash: "durable-hash"}
	guests := &workflowGuests{}
	taken := aliasIdentities{alias: &user.AuthMethods{UserId: 5, AuthType: "email", AuthIdentifier: "guest@gmail.com"}}
	workflow := NewWorkflow(WorkflowDeps{Orders: &workflowOrders{}, GuestAccounts: guests, GuestIdentities: taken}, nil)
	var owned *guestAccountTaken
	if err := workflow.ensureGuestAccount(ctx, guestOrder); !errors.As(err, &owned) || owned.userID != 5 || guests.creates != 0 {
		t.Fatalf("ensureGuestAccount = %v with %d accounts created, want the identity reported as user 5's and none created", err, guests.creates)
	}

	orders := &workflowOrders{}
	workflow = NewWorkflow(WorkflowDeps{Orders: orders, GuestAccounts: guests, GuestIdentities: aliasIdentities{}}, nil)
	if err := workflow.ensureGuestAccount(ctx, guestOrder); err != nil || guests.creates != 1 || orders.boundUser != 11 {
		t.Fatalf("ensureGuestAccount without an alias = %v, %d accounts, bound %d; want the account created and bound", err, guests.creates, orders.boundUser)
	}
}
