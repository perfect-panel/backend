package devicesocket

import (
	"bufio"
	"context"
	"crypto/sha1" //nolint:gosec // G505: RFC 6455 prescribes SHA-1 for Sec-WebSocket-Accept; not a security primitive here
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/network"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/gorilla/websocket"
	"github.com/perfect-panel/backend/pkg/logger"
)

// HandshakeError reports a refused WebSocket handshake with the HTTP status
// to answer it with.
type HandshakeError struct {
	Status int
	Reason string
}

func (e *HandshakeError) Error() string {
	return fmt.Sprintf("websocket handshake refused (%d): %s", e.Status, e.Reason)
}

// websocketGUID is the handshake constant of RFC 6455, section 1.3.
const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// UpgradeHertz accepts a Hertz request as the device's socket. Hertz writes
// a response only after the handler returns and hands the connection over
// afterwards, so the handshake is validated here, its response is set on
// the request context for Hertz to write, and the device is registered and
// served in the hijack handler Hertz then runs until the socket closes. The
// caller answers a HandshakeError with its status; nil means the socket is
// accepted and the handler must write nothing more.
func (dm *DeviceManager) UpgradeHertz(c *app.RequestContext, session string, userID int64, deviceID string, maxDevices int) error {
	req, err := compatRequest(c)
	if err != nil {
		return &HandshakeError{Status: http.StatusBadRequest, Reason: "malformed request"}
	}
	if err := dm.checkHandshake(req); err != nil {
		return err
	}

	c.SetStatusCode(consts.StatusSwitchingProtocols)
	c.Response.Header.Set("Upgrade", "websocket")
	c.Response.Header.Set("Connection", "Upgrade")
	c.Response.Header.Set("Sec-WebSocket-Accept", acceptKey(req.Header.Get("Sec-WebSocket-Key")))

	// Hertz's hijacked connection ignores Close (Hertz closes the raw
	// connection when the hijack handler returns), so the raw one is kept
	// to close a kicked device's socket, which ends its blocked read.
	raw := c.GetConn()
	c.Hijack(func(conn network.Conn) {
		hijacked := &hijackedConn{Conn: conn, raw: raw}
		ws, err := dm.upgrader.Upgrade(&hijackResponseWriter{conn: hijacked, header: http.Header{}}, req, nil)
		if err != nil {
			logger.Errorw("device websocket upgrade failed", logger.Field("user_id", userID), logger.Field("device_id", deviceID), logger.Field("error", err.Error()))
			return
		}
		device := dm.register(ws, session, userID, deviceID, maxDevices)
		dm.serve(userID, device)
	})
	return nil
}

// compatRequest is the Hertz request as the net/http request
// gorilla/websocket's Upgrade reads: its method, URL and headers.
func compatRequest(c *app.RequestContext) (*http.Request, error) {
	// The request only carries the handshake headers to gorilla/websocket; it
	// is never sent, so no deadline applies.
	req, err := http.NewRequestWithContext(context.Background(), string(c.Method()), c.URI().String(), http.NoBody)
	if err != nil {
		return nil, err
	}
	c.Request.Header.VisitAll(func(key, value []byte) {
		req.Header.Add(string(key), string(value))
	})
	return req, nil
}

// checkHandshake applies the checks gorilla/websocket's Upgrade applies,
// before Hertz commits to the 101 response.
func (dm *DeviceManager) checkHandshake(req *http.Request) error {
	if req.Method != http.MethodGet {
		return &HandshakeError{Status: http.StatusMethodNotAllowed, Reason: "request method is not GET"}
	}
	if !websocket.IsWebSocketUpgrade(req) {
		return &HandshakeError{Status: http.StatusBadRequest, Reason: "not a websocket handshake"}
	}
	if !headerHasToken(req.Header, "Sec-WebSocket-Version", "13") {
		return &HandshakeError{Status: http.StatusBadRequest, Reason: "unsupported websocket version"}
	}
	if !validChallengeKey(req.Header.Get("Sec-WebSocket-Key")) {
		return &HandshakeError{Status: http.StatusBadRequest, Reason: "missing or malformed Sec-WebSocket-Key"}
	}
	if !dm.upgrader.CheckOrigin(req) {
		return &HandshakeError{Status: http.StatusForbidden, Reason: "origin not allowed"}
	}
	return nil
}

// headerHasToken reports whether one of the header's comma-separated values
// is the token, ignoring case and surrounding spaces.
func headerHasToken(header http.Header, name, token string) bool {
	for _, value := range header.Values(name) {
		for _, candidate := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(candidate), token) {
				return true
			}
		}
	}
	return false
}

// validChallengeKey reports whether the key is the base64 form of 16 bytes,
// as RFC 6455 requires and gorilla/websocket checks.
func validChallengeKey(key string) bool {
	if key == "" {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(key)
	return err == nil && len(decoded) == 16
}

// acceptKey computes the Sec-WebSocket-Accept value of a challenge key.
func acceptKey(challengeKey string) string {
	// RFC 6455 prescribes SHA-1 for the accept key; it is not used for security here.
	h := sha1.New() //nolint:gosec // G401: see the import note; the handshake digest is not security-relevant
	h.Write([]byte(challengeKey))
	h.Write([]byte(websocketGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// hijackedConn is the connection Hertz handed over, as gorilla/websocket's
// Upgrade sees it. Hertz has already written the handshake response, so the
// copy Upgrade writes is dropped. Close closes the raw connection, since
// Hertz's wrapper ignores Close until the hijack handler returns: closing
// ends a blocked read, which is how a kicked device's read loop returns.
type hijackedConn struct {
	network.Conn
	raw       io.Closer
	handshake atomic.Bool
	closeOnce sync.Once
	closeErr  error
}

func (c *hijackedConn) Write(p []byte) (int, error) {
	if c.handshake.CompareAndSwap(false, true) {
		return len(p), nil
	}
	return c.Conn.Write(p)
}

func (c *hijackedConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.raw.Close()
	})
	return c.closeErr
}

// hijackResponseWriter is the http.ResponseWriter gorilla/websocket's
// Upgrade hijacks the connection from. The handshake is already answered by
// Hertz, so writes through it go nowhere.
type hijackResponseWriter struct {
	conn   net.Conn
	header http.Header
}

func (w *hijackResponseWriter) Header() http.Header { return w.header }

func (w *hijackResponseWriter) Write(p []byte) (int, error) { return len(p), nil }

func (w *hijackResponseWriter) WriteHeader(int) {}

func (w *hijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}
