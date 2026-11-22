package devicesocket

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/gorilla/websocket"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// signedInAs stands in for the session authentication middleware: it puts
// the account and its session in the context, as AuthMiddleware does.
func signedInAs(userID int64, session string) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if userID > 0 {
			ctx = user.NewContext(ctx, &user.User{Id: userID})
			ctx = context.WithValue(ctx, requestctx.CtxKeySessionID, session)
		}
		c.Next(ctx)
	}
}

// hertzDeviceServer serves the device socket route on a Hertz server bound
// to a free port, the way the HTTP API serves it, and returns its base URL.
func hertzDeviceServer(t *testing.T, dm *DeviceManager, auth app.HandlerFunc, deps HandlerDeps) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	h := server.New(server.WithHostPorts(addr), server.WithDisablePrintRoute(true), server.WithExitWaitTime(50*time.Millisecond))
	h.GET("/v1/app/ws/:userid/:identifier", auth, Handler(dm, deps))
	go h.Spin()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = h.Shutdown(ctx)
	})
	waitFor(t, "the server to listen", func() bool {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	})
	return "http://" + addr
}

// handshake is what a refused dial answered with.
type handshake struct {
	status int
	body   []byte
}

func dialHertz(t *testing.T, base, path string) (*websocket.Conn, handshake, error) {
	t.Helper()
	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+path, nil)
	var answer handshake
	if resp != nil {
		answer.status = resp.StatusCode
		answer.body, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
	}
	return conn, answer, err
}

// The route upgrades the signed-in user's request on Hertz's connection:
// heartbeats are answered, pushes arrive, and a kick delivers its
// notification and closes the socket at once, without waiting on the
// silent client.
func TestHandlerServesTheDeviceSocketOverHertz(t *testing.T) {
	dm := NewDeviceManager(3600, 3600, nil)
	defer dm.Stop()
	dm.OnDeviceKicked = func(userID int64, deviceID, session string, operator Operator) {
		if err := dm.SendToDevice(userID, deviceID, `{"method":"kicked_admin"}`); err != nil {
			t.Errorf("SendToDevice during kick callback: %v", err)
		}
	}
	limitAsked := int64(0)
	base := hertzDeviceServer(t, dm, signedInAs(7, "session-7"), HandlerDeps{
		MaxDevices: func(_ context.Context, userID int64) int { limitAsked = userID; return 3 },
	})

	conn, _, err := dialHertz(t, base, "/v1/app/ws/7/phone")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	waitFor(t, "the device to register", func() bool { return dm.Online() == 1 })
	if limitAsked != 7 {
		t.Fatalf("device limit asked for user %d, want 7", limitAsked)
	}
	devices := dm.snapshotDevices(7)
	if len(devices) != 1 || devices[0].DeviceID != "phone" || devices[0].Session != "session-7" {
		t.Fatalf("registered devices = %+v", devices)
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, msg, err := conn.ReadMessage(); err != nil || string(msg) != heartbeatReply {
		t.Fatalf("heartbeat reply = %q, %v", msg, err)
	}
	if err := dm.SendToDevice(7, "phone", "pushed"); err != nil {
		t.Fatal(err)
	}
	if _, msg, err := conn.ReadMessage(); err != nil || string(msg) != "pushed" {
		t.Fatalf("push = %q, %v", msg, err)
	}

	start := time.Now()
	dm.KickDevice(7, "phone")
	if _, msg, err := conn.ReadMessage(); err != nil || string(msg) != `{"method":"kicked_admin"}` {
		t.Fatalf("kick notification = %q, %v", msg, err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("the kicked socket stayed open")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("the kicked socket closed after %s; the kick must close it at once", elapsed)
	}
	waitFor(t, "the device to be removed", func() bool { return dm.Online() == 0 })
}

// The route refuses a caller without a session, a user id that is not the
// signed-in user's, and a request that is no WebSocket handshake, each
// before any upgrade.
func TestHandlerRefusesForeignUsersAndPlainRequests(t *testing.T) {
	dm := NewDeviceManager(3600, 3600, nil)
	defer dm.Stop()
	base := hertzDeviceServer(t, dm, signedInAs(7, "session-7"), HandlerDeps{})

	expectCode := func(t *testing.T, answer handshake, want uint32) {
		t.Helper()
		var result struct {
			Code uint32 `json:"code"`
		}
		if err := json.Unmarshal(answer.body, &result); err != nil || result.Code != want {
			t.Fatalf("handshake answered %d %s, want code %d", answer.status, answer.body, want)
		}
	}

	conn, answer, err := dialHertz(t, base, "/v1/app/ws/8/phone")
	if !errors.Is(err, websocket.ErrBadHandshake) {
		_ = conn.Close()
		t.Fatalf("dial as another user = %v, want a refused handshake", err)
	}
	expectCode(t, answer, xerr.UseridNotMatch)

	unauthenticated := hertzDeviceServer(t, dm, signedInAs(0, ""), HandlerDeps{})
	conn, answer, err = dialHertz(t, unauthenticated, "/v1/app/ws/7/phone")
	if !errors.Is(err, websocket.ErrBadHandshake) {
		_ = conn.Close()
		t.Fatalf("dial without a session = %v, want a refused handshake", err)
	}
	expectCode(t, answer, xerr.ErrorTokenInvalid)

	plain, err := http.Get(base + "/v1/app/ws/7/phone")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = plain.Body.Close() }()
	if plain.StatusCode != http.StatusBadRequest {
		t.Fatalf("plain GET answered %d, want 400", plain.StatusCode)
	}
	if dm.Online() != 0 {
		t.Fatalf("a refused request registered a device: %d online", dm.Online())
	}
}
