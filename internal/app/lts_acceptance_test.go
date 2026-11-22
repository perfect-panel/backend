package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/backend/internal/app/migration/schema"
	"github.com/perfect-panel/backend/internal/auth/usersession"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/network"
	netdto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/module/platform/entity/inbox"
	"github.com/perfect-panel/backend/internal/module/subscription"
	subdto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/client"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/transport/http/middleware"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/orm"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

type ltsFixture struct {
	queueRedis *miniredis.Miniredis
	*Application
	t       *testing.T
	account *user.User
	sub     *usersub.Subscribe
	server  *node.Server
	plan    *subscribe.Subscribe
}

func newLTSFixture(t *testing.T) *ltsFixture {
	t.Helper()
	return newLTSFixtureFor(t, "PPANEL_LTS_TEST_POSTGRES_DSN")
}

// The supplied login must be able to create databases. Only the unique
// database created below is migrated, populated and finally dropped.
func newLTSFixtureFor(t *testing.T, env string) *ltsFixture {
	t.Helper()
	dsn := os.Getenv(env)
	if dsn == "" {
		t.Skip("set " + env + " to exercise the assembled application")
	}
	logtest.Discard(t)
	params := orm.ParseDSN(dsn)
	if params == nil {
		t.Fatal("invalid acceptance database DSN")
	}
	conn := orm.Mysql{Config: *params}
	admin, err := orm.ConnectDatabase(conn)
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminPool.Close() })
	var random [8]byte
	rand.Read(random[:])
	name := "ppanel_accept_" + hex.EncodeToString(random[:])
	if err := admin.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Exec("DROP DATABASE " + name).Error; err != nil {
			t.Error(err)
		}
	})
	conn.Config.Dbname = name
	if err := schema.Up(conn.Driver(), conn.MigrationDsn()); err != nil {
		t.Fatal(err)
	}
	db, err := orm.ConnectDatabase(conn)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	var c config.Config
	c.AppLocation = "UTC"
	c.Currency.Unit = "CNY"
	c.Redis.Host = miniredis.RunT(t).Addr()
	c.Redis.KeyPrefix = "accept"
	c.Node.NodeSecret = "node-acceptance"
	c.Node.NodePullInterval = 1
	c.Node.NodePushInterval = 1
	r := c.Redis.NewClient()
	queueRedis := miniredis.RunT(t)
	queueConfig := c
	queueConfig.Redis.Host = queueRedis.Addr()
	q := NewAsynqClient(queueConfig)
	i := NewAsynqInspector(queueConfig)
	t.Cleanup(func() { _ = q.Close(); _ = i.Close(); _ = r.Close() })
	f := &ltsFixture{queueRedis: queueRedis, Application: assemble(c, db, r, nil, q, i), t: t}
	f.server = &node.Server{Name: "accept", Address: "local.test", Protocols: `[{"type":"vless","port":443,"enable":true,"ratio":1}]`}
	f.create(f.server)
	n := &node.Node{ServerId: f.server.Id, Name: "lts-real-node", Address: "edge.example.test", Port: 443, Protocol: "vless"}
	f.create(n)
	enabled := true
	f.plan = &subscribe.Subscribe{Name: "lts-plan", AllowDeduction: &enabled, UnitTime: "Month", UnitPrice: 1000, Nodes: strconv.FormatInt(n.Id, 10)}
	f.create(f.plan)
	f.account = &user.User{Enable: &enabled, ReferCode: "LTSACCOUNT"}
	f.create(f.account)
	f.sub = &usersub.Subscribe{UserId: f.account.Id, SubscribeId: f.plan.Id, Status: usersub.SubscribeStatusActive, StartTime: time.Now().Add(-time.Hour), ExpireTime: time.Now().Add(24 * time.Hour), UUID: "f30a026f-e3bb-442e-86ae-d1209e6001fd", Token: usersub.NewToken(), Traffic: 1 << 30}
	f.create(f.sub)
	f.create(&client.SubscribeApplication{Name: "lts", UserAgent: "lts-client", SubscribeTemplate: `{{ range .Proxies }}{{ .Name }}:{{ $.UserInfo.Password }};{{ end }}`, OutputFormat: "text", DownloadLink: "[]"})
	return f
}
func (f *ltsFixture) create(v any) {
	f.t.Helper()
	if err := f.DB.Create(v).Error; err != nil {
		f.t.Fatal(err)
	}
}
func (f *ltsFixture) scope() netdto.ServerCommon {
	return netdto.ServerCommon{Protocol: "vless", ServerId: f.server.Id}
}
func (f *ltsFixture) delivery(token string) (string, error) {
	resp, err := f.Subscription.Deliver(f.t.Context(), subscription.RequestMeta{Host: "panel.example", RequestURI: "/subscribe", UserAgent: "lts-client", ClientIP: "127.0.0.1"}, &subdto.SubscribeRequest{Token: token})
	if err != nil {
		return "", err
	}
	return string(resp.Config), nil
}
func (f *ltsFixture) distributed(token, credential string) bool {
	f.t.Helper()
	body, err := f.delivery(token)
	if err != nil {
		f.t.Fatal(err)
	}
	return strings.Contains(body, "lts-real-node:"+credential+";")
}
func (f *ltsFixture) nodeAccepts(credential string) bool {
	f.t.Helper()
	r, _, err := f.Network.GetServerUserList(f.t.Context(), &netdto.GetServerUserListRequest{ServerCommon: f.scope()}, network.RequestMeta{})
	if err != nil {
		f.t.Fatal(err)
	}
	for _, u := range r.Users {
		if u.Id == f.sub.Id && u.UUID == credential {
			return true
		}
	}
	return false
}
func (f *ltsFixture) reloadSub() *usersub.Subscribe {
	f.t.Helper()
	var sub usersub.Subscribe
	if err := f.DB.First(&sub, f.sub.Id).Error; err != nil {
		f.t.Fatal(err)
	}
	return &sub
}
func TestLTSSubscriptionAccess(t *testing.T) {
	f := newLTSFixture(t)
	ctx := t.Context()
	old := f.sub
	if !f.distributed(old.Token, old.UUID) || !f.nodeAccepts(old.UUID) {
		t.Fatal("active subscriber did not reach both delivery paths")
	}
	if err := f.Subscription.ResetUserSubscribeToken(ctx, &subdto.ResetUserSubscribeTokenRequest{UserSubscribeId: old.Id}); err != nil {
		t.Fatal(err)
	}
	fresh := f.reloadSub()
	if _, err := f.delivery(old.Token); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("old token lookup: %v", err)
	}
	if f.nodeAccepts(old.UUID) || !f.distributed(fresh.Token, fresh.UUID) || !f.nodeAccepts(fresh.UUID) {
		t.Fatal("credential rotation did not converge at both endpoints")
	}
	if err := f.Identity.SetUserEnabled(ctx, old.UserId, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.delivery(fresh.Token); xerr.CodeOf(err) != xerr.UserDisabled {
		t.Fatalf("disabled subscription URL: %v", err)
	}
	if f.nodeAccepts(fresh.UUID) {
		t.Fatal("disabled owner still receives usable credentials")
	}
	if err := f.Identity.SetUserEnabled(ctx, old.UserId, true); err != nil {
		t.Fatal(err)
	}
	if !f.distributed(fresh.Token, fresh.UUID) || !f.nodeAccepts(fresh.UUID) {
		t.Fatal("enabled owner did not regain service")
	}
}

// Use the production request guard with the same auth/session/account adapters
// as a protected API. Re-enabling must require a fresh sign-in.
func TestLTSSessionReactivation(t *testing.T) {
	f := newLTSFixture(t)
	ctx := t.Context()
	const secret = "local-revocation-acceptance"
	auth := middleware.AuthDeps{JWT: config.JwtAuth{AccessSecret: secret}, Redis: f.Redis, Accounts: f.Identity}
	session := func() string {
		t.Helper()
		token, err := usersession.Issue(ctx, f.Redis, secret, 3600, usersession.Grant{UserID: f.account.Id, LoginType: "email"})
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	check := func(token string, allow bool) {
		t.Helper()
		if _, err := middleware.AuthenticateRequest(ctx, auth, token); (err == nil) != allow {
			t.Fatalf("request authentication allow=%t: %v", allow, err)
		}
	}
	old := session()
	check(old, true)
	if err := f.Identity.SetUserEnabled(ctx, f.account.Id, false); err != nil {
		t.Fatal(err)
	}
	check(old, false)
	if err := f.Identity.SetUserEnabled(ctx, f.account.Id, true); err != nil {
		t.Fatal(err)
	}
	check(old, false)
	check(session(), true)
}

func TestLTSReportReplay(t *testing.T) {
	for _, db := range []string{"POSTGRES", "MYSQL", "MARIADB"} {
		t.Run(db, func(t *testing.T) { testLTSReportReplay(t, newLTSFixtureFor(t, "PPANEL_LTS_TEST_"+db+"_DSN")) })
	}
}
func testLTSReportReplay(t *testing.T, f *ltsFixture) {
	t.Helper()
	ctx := t.Context()
	req := netdto.ServerPushUserTrafficRequest{ServerCommon: f.scope(), ReportID: "test-batch-1", Traffic: []netdto.UserTraffic{{SID: f.sub.Id, Upload: 17, Download: 29}}}
	var g sync.WaitGroup
	for range 8 {
		g.Go(func() {
			if err := f.Network.ServerPushUserTraffic(ctx, &req); err != nil {
				t.Error(err)
			}
		})
	}
	g.Wait()
	a := network.NewTrafficAggregator(f.taskDependencies(nil).Traffic.Aggregator)
	flush := func() {
		t.Helper()
		if err := a.FlushDueBuckets(ctx, time.Now().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	// Fail the log transaction after usage has committed, as on a partial
	// storage outage. The same SQL receipt must resume without charging twice.
	if err := f.DB.Exec("ALTER TABLE traffic_log ADD CONSTRAINT lts_fail_log CHECK (upload <> 17)").Error; err != nil {
		t.Fatal(err)
	}
	if err := a.FlushDueBuckets(ctx, time.Now().Add(time.Minute)); err == nil {
		t.Fatal("failed log write acknowledged")
	}
	if row := f.reloadSub(); row.Upload != 17 || row.Download != 29 {
		t.Fatalf("unexpected usage after partial commit: %+v", row)
	}
	if err := f.DB.Exec("ALTER TABLE traffic_log DROP CONSTRAINT lts_fail_log").Error; err != nil {
		t.Fatal(err)
	}
	flush()
	if err := f.Network.ServerPushUserTraffic(ctx, &req); err != nil {
		t.Fatal(err)
	}
	flush()
	row := f.reloadSub()
	if row.Upload != 17 || row.Download != 29 {
		t.Fatalf("usage counted more than once: %d/%d", row.Upload, row.Download)
	}
	req.Traffic[0].Upload++
	if err := f.Network.ServerPushUserTraffic(ctx, &req); err == nil {
		t.Fatal("conflicting payload reused a receipt")
	}
	req.ReportID = "test-batch-2"
	if err := f.Network.ServerPushUserTraffic(ctx, &req); err != nil {
		t.Fatal(err)
	}
	flush()
	var logs []traffic.TrafficLog
	if err := f.DB.Where("subscribe_id = ?", f.sub.Id).Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	var up, down int64
	for _, l := range logs {
		up += l.Upload
		down += l.Download
	}
	row = f.reloadSub()
	if len(logs) != 2 || up != 35 || down != 58 || row.Upload != up || row.Download != down {
		t.Fatalf("usage/log mismatch rows=%d, usage=%d/%d logs=%d/%d", len(logs), row.Upload, row.Download, up, down)
	}
	total, err := f.Store.TrafficLog().QueryTrafficSummary(ctx, time.Now().Add(-24*time.Hour), time.Now().Add(24*time.Hour))
	if err != nil || total.Upload != up || total.Download != down {
		t.Fatalf("daily summary source: %+v, %v", total, err)
	}

	var completed int64
	if err := f.DB.Model(&inbox.TrafficReport{}).Where("completed AND usage_applied AND payload IS NULL").Count(&completed).Error; err != nil || completed != 2 {
		t.Fatalf("compacted receipts = %d, %v", completed, err)
	}
	testLTSLegacyTraffic(t, f, a)

}

func testLTSLegacyTraffic(t *testing.T, f *ltsFixture, a *network.TrafficAggregator) {
	t.Helper()
	ctx := t.Context()
	before := f.reloadSub()
	req := netdto.ServerPushUserTrafficRequest{ServerCommon: f.scope(), Traffic: []netdto.UserTraffic{{SID: f.sub.Id, Upload: 3, Download: 7}}}
	for range 2 {
		if err := f.Network.ServerPushUserTraffic(ctx, &req); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.FlushDueBuckets(ctx, time.Now().Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	after := f.reloadSub()
	if after.Upload-before.Upload != 6 || after.Download-before.Download != 14 {
		t.Fatal("legacy reports with identical deltas were lost")
	}
}

func TestLTSRefundAndExpiryRemoveNodeCredentials(t *testing.T) {
	for _, scenario := range []string{"refund", "expiry"} {
		t.Run(scenario, func(t *testing.T) {
			f := newLTSFixture(t)
			ctx := user.NewContext(t.Context(), f.account)
			if scenario == "refund" {
				o := &order.Order{OrderNo: order.GenerateTradeNo(), UserId: f.account.Id, SubscribeId: f.plan.Id, Type: order.TypeSubscribe, Method: "GMPay", Status: order.StatusFinished, Price: 1000, Amount: 800, GiftAmount: 200}
				f.create(o)
				if err := f.DB.Model(f.sub).Updates(map[string]any{"order_id": o.Id, "start_time": time.Now().Add(time.Hour), "expire_time": time.Now().AddDate(0, 1, 0)}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if !f.distributed(f.sub.Token, f.sub.UUID) || !f.nodeAccepts(f.sub.UUID) {
				t.Fatal("active subscription unavailable")
			}
			switch scenario {
			case "refund":
				f.refund(ctx)
			case "expiry":
				if err := f.DB.Model(f.sub).Update("expire_time", time.Now().Add(-time.Minute)).Error; err != nil {
					t.Fatal(err)
				}
				if err := f.Subscription.CheckSubscriptions(ctx); err != nil {
					t.Fatal(err)
				}
				if f.reloadSub().Status != usersub.SubscribeStatusExpired {
					t.Fatal("sweep did not expire subscription")
				}
			}
			body, err := f.delivery(f.sub.Token)
			if err != nil || strings.Contains(body, "lts-real-node") || f.nodeAccepts(f.sub.UUID) {
				t.Fatalf("revoked subscription still usable or delivery failed: %q %v", body, err)
			}
		})
	}
}

func (f *ltsFixture) refund(ctx context.Context) {
	t := f.t
	t.Helper()
	quote, err := f.Subscription.PreUnsubscribe(ctx, &subdto.PreUnsubscribeRequest{Id: f.sub.Id})
	if err != nil || quote.DeductionAmount <= 200 {
		t.Fatalf("refund quote: %+v %v", quote, err)
	}
	if err := f.Subscription.Unsubscribe(ctx, &subdto.UnsubscribeRequest{Id: f.sub.Id}); err != nil {
		t.Fatal(err)
	}
	var credit wallet.Wallet
	if err := f.DB.Where("user_id = ?", f.account.Id).First(&credit).Error; err != nil {
		t.Fatal(err)
	}
	if credit.GiftAmount != 200 || credit.Balance != quote.DeductionAmount-200 || f.reloadSub().Status != usersub.SubscribeStatusDeducted {
		t.Fatalf("refund ledger/gift allocation mismatch: %+v", credit)
	}
}
