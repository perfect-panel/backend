package app

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	serverv1 "github.com/perfect-panel/backend/api/server/v1"
	"github.com/perfect-panel/backend/internal/module/network"
	"google.golang.org/protobuf/proto"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	subdto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/transport/http/routes"
)

// With a real Terraris binary, verify new VLESS connections using a previously
// downloaded credential. A separate active subscriber must still reach the
// loopback echo service, so an unavailable listener cannot masquerade as denial.
func TestLTSLiveNodeRevocation(t *testing.T) {
	bin := os.Getenv("PPANEL_LTS_TERRARIS_BIN")
	if bin == "" {
		t.Skip("set PPANEL_LTS_TERRARIS_BIN for real node polling and data-plane probes")
	}
	f := newLTSFixture(t)
	ctx := user.NewContext(t.Context(), f.account)
	o := &order.Order{OrderNo: order.GenerateTradeNo(), UserId: f.account.Id, SubscribeId: f.plan.Id, Type: order.TypeSubscribe, Method: "GMPay", Status: order.StatusFinished, Price: 1000, Amount: 800, GiftAmount: 200}
	f.create(o)
	if err := f.DB.Model(f.sub).Updates(map[string]any{"order_id": o.Id, "start_time": time.Now().Add(time.Hour), "expire_time": time.Now().AddDate(0, 1, 0)}).Error; err != nil {
		t.Fatal(err)
	}
	on := true
	control := &user.User{Enable: &on, ReferCode: "CONTROL"}
	f.create(control)
	makeSub := func(expires time.Time) *usersub.Subscribe {
		s := &usersub.Subscribe{UserId: control.Id, SubscribeId: f.plan.Id, Status: usersub.SubscribeStatusActive, StartTime: time.Now(), ExpireTime: expires, UUID: uuid.NewV4().String(), Token: usersub.NewToken(), Traffic: 1 << 30}
		f.create(s)
		return s
	}
	controlSub := makeSub(time.Now().Add(time.Hour))
	expiring := makeSub(time.Now().Add(time.Hour))
	remote := f.startTerraris(bin, nil)
	probe := newVLESSProbe(t, remote)
	probe.wait(controlSub.UUID, true)
	probe.wait(expiring.UUID, true)
	target := f.sub
	if !f.distributed(target.Token, target.UUID) {
		t.Fatal("could not download starting subscription")
	}
	probe.wait(target.UUID, true)
	oldStream := probe.hold(target.UUID)
	if err := f.Subscription.ResetOwnSubscribeToken(ctx, &subdto.ResetUserSubscribeTokenRequest{UserSubscribeId: target.Id}); err != nil {
		t.Fatal(err)
	}
	fresh := f.reloadSub()
	probe.wait(fresh.UUID, true)
	probe.wait(target.UUID, false)
	probe.assertClosed(oldStream)
	activeStream := probe.hold(fresh.UUID)
	if err := f.Identity.SetUserEnabled(ctx, f.account.Id, false); err != nil {
		t.Fatal(err)
	}
	probe.wait(fresh.UUID, false)
	probe.assertClosed(activeStream)
	probe.wait(controlSub.UUID, true)
	if err := f.Identity.SetUserEnabled(ctx, f.account.Id, true); err != nil {
		t.Fatal(err)
	}
	probe.wait(fresh.UUID, true)
	refundStream := probe.hold(fresh.UUID)
	f.refund(ctx)
	probe.wait(fresh.UUID, false)
	probe.assertClosed(refundStream)
	probe.wait(controlSub.UUID, true)
	expiryStream := probe.hold(expiring.UUID)
	if err := f.DB.Model(expiring).Update("expire_time", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Subscription.CheckSubscriptions(ctx); err != nil {
		t.Fatal(err)
	}
	probe.wait(expiring.UUID, false)
	probe.assertClosed(expiryStream)
	probe.wait(controlSub.UUID, true)
}

func (f *ltsFixture) startTerraris(bin string, dropAck func(*http.Request, []byte) bool) string {
	t := f.t
	t.Helper()
	dir := t.TempDir()
	free, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := free.Addr().(*net.TCPAddr)
	_ = free.Close()
	if err := f.server.MarshalProtocols([]node.Protocol{{Type: "vless", Enable: true, Port: uint16(address.Port), Network: "tcp", Transport: "tcp", Security: "none", Encryption: "none", Ratio: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := f.DB.Model(f.server).UpdateColumn("protocols", f.server.Protocols).Error; err != nil {
		t.Fatal(err)
	}
	h := server.New(server.WithDisablePrintRoute(true))
	routes.RegisterHandlers(h, f.routeDependencies())
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read", 400)
			return
		}
		var headers []ut.Header
		for _, k := range []string{"Content-Type", "Accept", "If-None-Match", "Idempotency-Key"} {
			if v := r.Header.Get(k); v != "" {
				headers = append(headers, ut.Header{Key: k, Value: v})
			}
		}
		got := ut.PerformRequest(h.Engine, r.Method, r.URL.RequestURI(), &ut.Body{Body: bytes.NewReader(raw), Len: len(raw)}, headers...)
		if got.Code == http.StatusOK && r.URL.Path == "/v1/server/push" && dropAck != nil && dropAck(r, raw) {
			con, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("drop ACK: %v", err)
				return
			}
			_ = con.Close()
			return
		}
		for _, k := range []string{"Content-Type", "ETag"} {
			if v := got.Header().Get(k); v != "" {
				w.Header().Set(k, v)
			}
		}
		w.WriteHeader(got.Code)
		_, _ = w.Write(got.Body.Bytes())
	}))
	t.Cleanup(panel.Close)
	conf := fmt.Sprintf("[panel]\nbase_url=%q\nserver_id=%d\nsecret_env=\"PPANEL_NODE_ACCEPT_SECRET\"\n[node]\nlisten=\"127.0.0.1\"\n", panel.URL, f.server.Id)
	file := filepath.Join(dir, "terraris.toml")
	if err := os.WriteFile(file, []byte(conf), 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "terraris.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), bin, "run", "--config", file)
	cmd.Env = append(os.Environ(), "PPANEL_NODE_ACCEPT_SECRET=node-acceptance")
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = log.Close()
		if t.Failed() {
			b, _ := os.ReadFile(logPath)
			t.Logf("node output: %.2400s", b)
		}
	})
	return address.String()
}

type vlessProbe struct {
	t        *testing.T
	node     string
	echoPort int
}

func newVLESSProbe(t *testing.T, nodeAddress string) *vlessProbe {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_ = conn.SetDeadline(time.Now().Add(time.Minute))
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return &vlessProbe{t: t, node: nodeAddress, echoPort: l.Addr().(*net.TCPAddr).Port}
}
func (p *vlessProbe) wait(credential string, want bool) {
	p.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		con := p.connect(credential)
		ok := con != nil
		if ok {
			_ = con.Close()
		}
		if ok == want {
			return
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("local VLESS listener authorization = %v, want %v within the polling window (port %s)", ok, want, strconv.Itoa(p.echoPort))
		}
		time.Sleep(100 * time.Millisecond)
	}
}
func (p *vlessProbe) hold(credential string) net.Conn {
	p.t.Helper()
	con := p.connect(credential)
	if con == nil {
		p.t.Fatal("expected an authorized live stream")
	}
	p.t.Cleanup(func() { _ = con.Close() })
	return con
}

func (p *vlessProbe) assertClosed(con net.Conn) {
	p.t.Helper()
	_ = con.SetDeadline(time.Now().Add(time.Second))
	_, err := con.Write([]byte("lts"))
	if err == nil {
		var b [3]byte
		_, err = io.ReadFull(con, b[:])
	}
	if err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		p.t.Fatalf("revoked stream did not close: %v", err)
	}
}

func (p *vlessProbe) connect(credential string) net.Conn {
	id, err := hex.DecodeString(strings.ReplaceAll(credential, "-", ""))
	if err != nil || len(id) != 16 {
		p.t.Fatal("invalid fixture UUID")
	}
	con, err := net.DialTimeout("tcp4", p.node, time.Second)
	if err != nil {
		return nil
	}

	_ = con.SetDeadline(time.Now().Add(time.Second))
	request := append([]byte{0}, id...)
	request = append(request, 0, 1)
	request = binary.BigEndian.AppendUint16(request, uint16(p.echoPort))
	request = append(request, 1, 127, 0, 0, 1, 'l', 't', 's')
	if _, err := con.Write(request); err != nil {
		_ = con.Close()
		return nil
	}
	var reply [5]byte
	_, err = io.ReadFull(con, reply[:])
	if err != nil || !bytes.Equal(reply[:], []byte{0, 0, 'l', 't', 's'}) {
		_ = con.Close()
		return nil
	}
	return con
}

func TestLTSLiveTrafficRetry(t *testing.T) {
	bin := os.Getenv("PPANEL_LTS_TERRARIS_BIN")
	if bin == "" {
		t.Skip("set PPANEL_LTS_TERRARIS_BIN to run a real reporter")
	}
	f := newLTSFixture(t)
	var mu sync.Mutex
	var firstID string
	var firstBody []byte
	seen := make(map[string]bool)
	first, retry, fresh := make(chan struct{}), make(chan struct{}), make(chan struct{})
	retried := sync.OnceFunc(func() { close(retry) })
	next := sync.OnceFunc(func() { close(fresh) })
	var up, down int64
	drop := func(r *http.Request, body []byte) bool {
		mu.Lock()
		defer mu.Unlock()
		id := r.Header.Get("Idempotency-Key")
		if id == "" {
			t.Error("node omitted batch ID")
			return false
		}
		if seen[id] {
			if id == firstID {
				if !bytes.Equal(body, firstBody) {
					t.Error("in-flight payload changed on retry")
				}
				retried()
			}
			return false
		}
		seen[id] = true
		var report serverv1.PushUserTrafficRequest
		if err := proto.Unmarshal(body, &report); err != nil {
			t.Errorf("traffic payload: %v", err)
			return false
		}
		for _, v := range report.Traffic {
			if v.UserId == f.sub.Id {
				up += v.Upload
				down += v.Download
			}
		}
		if firstID == "" {
			firstID = id
			firstBody = bytes.Clone(body)
			close(first)
			return true
		}
		next()
		return false
	}
	probe := newVLESSProbe(t, f.startTerraris(bin, drop))
	wait := func(ch chan struct{}, phase string) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(20 * time.Second):
			t.Fatalf("no %s report", phase)
		}
	}
	probe.wait(f.sub.UUID, true)
	wait(first, "first")
	probe.wait(f.sub.UUID, true)
	wait(retry, "retry")
	wait(fresh, "new")
	if err := network.NewTrafficAggregator(f.taskDependencies(nil).Traffic.Aggregator).FlushDueBuckets(t.Context(), time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got := f.reloadSub()
	mu.Lock()
	defer mu.Unlock()
	if up == 0 || down == 0 || got.Upload != up || got.Download != down {
		t.Fatalf("unique wire deltas=%d/%d DB=%d/%d", up, down, got.Upload, got.Download)
	}
}
