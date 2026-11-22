package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/auth/ratelimit"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/client"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/repo"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/subtest"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// enabledAccounts answers every owner lookup with an enabled account and
// counts the lookups: a request refused before the token is resolved never
// reaches it.
type enabledAccounts struct{ calls int }

var _ AccountStateReader = (*enabledAccounts)(nil)

func (a *enabledAccounts) FindAccountState(_ context.Context, id int64) (*user.AccountState, error) {
	a.calls++
	enabled := true
	return &user.AccountState{Id: id, Enable: &enabled}, nil
}

// fetchLimiter is a FetchLimiter answering every permit with state or err
// and recording the keys asked for.
type fetchLimiter struct {
	state int
	err   error
	keys  []string
}

var _ FetchLimiter = (*fetchLimiter)(nil)

func (l *fetchLimiter) Take(_ context.Context, key string) (int, error) {
	l.keys = append(l.keys, key)
	return l.state, l.err
}

// deliveryFixture is the delivery over the module's real repositories, with
// one default client application whose template lists the plan name and the
// node names.
type deliveryFixture struct {
	*subtest.Fixture
	svc      *Service
	accounts *enabledAccounts
	nodes    *deliveryNodeRepo
	limiter  *fetchLimiter
}

const deliveryTemplate = `{{ .SubscribeName }}|{{ range .Proxies }}{{ .Name }};{{ end }}`

func newDeliveryFixture(t *testing.T) *deliveryFixture {
	t.Helper()
	f := &deliveryFixture{Fixture: subtest.New(t), accounts: &enabledAccounts{}, nodes: &deliveryNodeRepo{}, limiter: &fetchLimiter{state: ratelimit.Allowed}}
	if err := f.DB.AutoMigrate(&client.SubscribeApplication{}); err != nil {
		t.Fatalf("migrate client applications: %v", err)
	}
	if err := f.DB.Create(&client.SubscribeApplication{Name: "Clash", UserAgent: "clash", IsDefault: true, SubscribeTemplate: deliveryTemplate, OutputFormat: "text"}).Error; err != nil {
		t.Fatal(err)
	}
	f.svc = NewService(Deps{
		Clients:        repo.NewClientRepo(repository.ModuleConn{DB: f.DB, Redis: f.Redis}.Conn()),
		Plans:          f.Store.Subscribe(),
		UserSubs:       f.Store.UserSubscription(),
		Users:          f.accounts,
		Nodes:          f.nodes,
		Logs:           subtest.NewLogs(f.DB),
		ConfigSnapshot: func() Config { return Config{SiteName: "Panel", SiteHost: "panel.example"} },
		Limiter:        f.limiter,
	})
	return f
}

func (f *deliveryFixture) deliver(token string) (*dto.SubscribeResponse, error) {
	meta := RequestMeta{Host: "sub.example", RequestURI: "/v1/subscribe/config?token=" + token, UserAgent: "ClashMeta/1.18.0", ClientIP: "203.0.113.9"}
	return f.svc.Deliver(context.Background(), meta, &dto.SubscribeRequest{Token: token})
}

// A subscription of the plan, with the token the server would issue.
func (f *deliveryFixture) active(t *testing.T, planID int64) *usersub.Subscribe {
	t.Helper()
	return f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: planID, ExpireTime: time.Now().Add(24 * time.Hour), Status: usersub.SubscribeStatusActive, Token: usersub.NewToken()})
}

func TestDeliverRendersTheSubscriptionAndRecordsTheFetch(t *testing.T) {
	f := newDeliveryFixture(t)
	f.Plan(t, subscribe.Subscribe{Id: 1, Name: "gold", Nodes: "5"})
	sub := f.active(t, 1)
	resp, err := f.deliver(sub.Token)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(resp.Config); got != "gold|real;" {
		t.Fatalf("config = %q, want the plan name and the node", got)
	}
	if f.nodes.calls != 1 || f.accounts.calls != 1 || len(f.limiter.keys) != 1 || f.limiter.keys[0] != "203.0.113.9" {
		t.Fatalf("lookups: nodes %d accounts %d limiter %v", f.nodes.calls, f.accounts.calls, f.limiter.keys)
	}
	if rows := f.Logs(t, log.TypeSubscribe); len(rows) != 1 || rows[0].ObjectID != 7 {
		t.Fatalf("audit rows = %+v, want the owner's fetch", rows)
	}
}

// A request without a token that can be stored is refused before the limiter,
// the database and the owner's account are consulted: the empty token used to
// be looked up (and could match a token-less row of an older version) and
// cost a negative cache entry.
func TestDeliverRefusesTokensThatCannotBeStoredBeforeAnyLookup(t *testing.T) {
	f := newDeliveryFixture(t)
	f.Plan(t, subscribe.Subscribe{Id: 1, Name: "gold", Nodes: "5"})
	// A row of an older version that stored no token.
	f.Subscription(t, usersub.Subscribe{UserId: 8, SubscribeId: 1, ExpireTime: time.Now().Add(24 * time.Hour), Status: usersub.SubscribeStatusActive, Token: "", UUID: "blank"})
	for name, token := range map[string]string{"empty": "", "short": "abc", "with a space": "0123456789abcdef 0123456789abcdef"} {
		t.Run(name, func(t *testing.T) {
			if _, err := f.deliver(token); xerr.CodeOf(err) != xerr.ErrorTokenInvalid {
				t.Fatalf("Deliver(%q) = %v, want the invalid-token code", token, err)
			}
		})
	}
	if f.accounts.calls != 0 || f.nodes.calls != 0 || len(f.limiter.keys) != 0 {
		t.Fatalf("a refused token was looked up: accounts %d nodes %d limiter %v", f.accounts.calls, f.nodes.calls, f.limiter.keys)
	}
	if f.Cached("cache:user:subscribe:token:") {
		t.Fatal("the empty token left a cache entry")
	}
}

// The fetches of a client address are limited before the token costs a
// query. An unreachable limiter admits the fetch, since the subscription must
// stay deliverable while Redis is down; a request without a client address
// is not keyed.
func TestDeliverLimitsFetchesPerClientAddress(t *testing.T) {
	f := newDeliveryFixture(t)
	f.Plan(t, subscribe.Subscribe{Id: 1, Name: "gold", Nodes: "5"})
	sub := f.active(t, 1)

	f.limiter.state = ratelimit.OverQuota
	if _, err := f.deliver(sub.Token); xerr.CodeOf(err) != xerr.TooManyRequests {
		t.Fatalf("Deliver over quota = %v, want the too-many-requests code", err)
	}
	if f.accounts.calls != 0 {
		t.Fatal("a refused fetch resolved the token")
	}
	f.limiter.state, f.limiter.err = ratelimit.Unknown, errors.New("redis unavailable")
	if _, err := f.deliver(sub.Token); err != nil {
		t.Fatalf("Deliver with the limiter down = %v, want the fetch admitted", err)
	}
	f.limiter.state, f.limiter.err = ratelimit.HitQuota, nil
	if _, err := f.deliver(sub.Token); err != nil {
		t.Fatalf("Deliver on the last permit = %v", err)
	}
	if len(f.limiter.keys) != 3 {
		t.Fatalf("limiter keys = %v, want one per fetch", f.limiter.keys)
	}
	meta := RequestMeta{Host: "sub.example", RequestURI: "/", UserAgent: "clash"}
	if _, err := f.svc.Deliver(context.Background(), meta, &dto.SubscribeRequest{Token: sub.Token}); err != nil || len(f.limiter.keys) != 3 {
		t.Fatalf("a fetch without a client address: %v, limiter keys %v", err, f.limiter.keys)
	}
}

// A subscription whose plan was deleted (only ended subscriptions can point
// at one) is delivered as a notice instead of failing: the expired one shows
// why, a live one is unavailable, and no nodes are looked up.
func TestDeliverServesANoticeWhenThePlanWasDeleted(t *testing.T) {
	f := newDeliveryFixture(t)
	expired := f.Subscription(t, usersub.Subscribe{UserId: 7, SubscribeId: 99, ExpireTime: time.Now().Add(-time.Hour), Status: usersub.SubscribeStatusExpired, Token: usersub.NewToken()})
	live := f.active(t, 99)
	for _, tc := range []struct {
		sub    *usersub.Subscribe
		notice string
	}{{expired, noticeExpired}, {live, noticeUnavailable}} {
		resp, err := f.deliver(tc.sub.Token)
		if err != nil {
			t.Fatalf("Deliver for the deleted plan: %v", err)
		}
		if got := string(resp.Config); !strings.HasPrefix(got, "|") || !strings.Contains(got, tc.notice) {
			t.Fatalf("config = %q, want no plan name and the %q notice", got, tc.notice)
		}
	}
	if f.nodes.calls != 0 {
		t.Fatal("nodes were looked up for a deleted plan")
	}
}
