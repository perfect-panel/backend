// Package devicesocket keeps the WebSocket connections of signed-in client
// devices. It tracks the sockets each user holds, caps how many one user may
// keep open, answers heartbeats and drops silent sockets, and lets the
// application push to or kick a device. It knows users and devices only by
// their IDs: the online, offline and kick callbacks leave recording presence
// and ending sessions to the application, which owns the accounts.
//
// Every socket is served by two goroutines of its own: a read loop and a
// writer. Pushes are queued to the writer without waiting, so a device that
// stops draining its socket costs nobody else anything: it fails its write
// deadline and is dropped. Handler adapts the socket to the HTTP API.
package devicesocket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/perfect-panel/backend/pkg/logger"
)

type Operator int

const (
	MaxDevices Operator = iota
	Admin
	SubscribeUpdate = "subscribe_update"
)

// The socket's limits. Clients send heartbeats and short commands, and are
// expected to take what the server pushes promptly.
const (
	// writeWait bounds one write to a client; a client that does not take a
	// message within it is dropped.
	writeWait = 10 * time.Second
	// maxMessageSize bounds a frame a client sends; a larger one ends the
	// connection.
	maxMessageSize = 4 << 10
	// sendQueueSize is how many pushes a client may have pending. Further
	// pushes to a client that far behind are refused rather than queued.
	sendQueueSize = 32
	// messageWorkers bounds the OnMessage callbacks running at once across
	// every connection; the read loops wait for a free worker.
	messageWorkers = 64
	// defaultMaxDevices caps a user's sockets when no limit is configured.
	defaultMaxDevices = 99
	// defaultInterval stands in for a heartbeat timeout or check interval
	// that is not positive.
	defaultInterval = 30 * time.Second
	// heartbeatReply answers a client's heartbeat.
	heartbeatReply = "ping"
)

var (
	// ErrSendQueueFull reports a push refused because the device is not
	// draining its socket.
	ErrSendQueueFull = errors.New("device send queue is full")
	// ErrDeviceClosed reports a push to a device whose socket was retired.
	ErrDeviceClosed = errors.New("device socket is closed")
)

// Device is one connected socket of a user.
type Device struct {
	Session   string
	DeviceID  string
	Conn      *websocket.Conn
	CreatedAt time.Time

	lastPing atomic.Int64 // UnixNano of the latest heartbeat
	// send queues messages for the writer goroutine, the only goroutine
	// writing data frames to Conn: gorilla/websocket panics on concurrent
	// writes, and a write to a slow client must block nobody but the writer.
	send chan []byte
	// closing is closed by close: no more messages are accepted and the
	// writer, once it has written what was queued, closes the connection.
	closing   chan struct{}
	closeOnce sync.Once
	writer    sync.WaitGroup
}

func newDevice(conn *websocket.Conn, session, deviceID string, now time.Time) *Device {
	device := &Device{
		Session:   session,
		DeviceID:  deviceID,
		Conn:      conn,
		CreatedAt: now,
		send:      make(chan []byte, sendQueueSize),
		closing:   make(chan struct{}),
	}
	device.lastPing.Store(now.UnixNano())
	return device
}

// LastPingTime returns when the device last sent a heartbeat.
func (d *Device) LastPingTime() time.Time {
	return time.Unix(0, d.lastPing.Load())
}

func (d *Device) touch(now time.Time) {
	d.lastPing.Store(now.UnixNano())
}

// enqueue hands message to the writer without waiting: a device that is
// not draining its socket refuses the message instead of stalling the
// caller.
func (d *Device) enqueue(message string) error {
	select {
	case <-d.closing:
		return ErrDeviceClosed
	default:
	}
	select {
	case d.send <- []byte(message):
		return nil
	case <-d.closing:
		return ErrDeviceClosed
	default:
		return ErrSendQueueFull
	}
}

// startWriter runs the writer goroutine. It writes the queued messages
// until the device closes, then those still queued within one write
// deadline (a kick's notification is queued right before the close), and
// closes the connection, which ends the read loop. A write that misses its
// deadline, or fails otherwise, closes the connection at once.
func (d *Device) startWriter(userID int64) {
	d.writer.Add(1)
	go func() {
		defer d.writer.Done()
		defer func() { _ = d.Conn.Close() }()
		for {
			select {
			case message := <-d.send:
				_ = d.Conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := d.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
					logger.Infow("device write failed; closing the socket", logger.Field("device_id", d.DeviceID), logger.Field("user_id", userID), logger.Field("error", err.Error()))
					return
				}
			case <-d.closing:
				_ = d.Conn.SetWriteDeadline(time.Now().Add(writeWait))
				for {
					select {
					case message := <-d.send:
						if err := d.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
							return
						}
					default:
						return
					}
				}
			}
		}
	}()
}

// close retires the socket: no more messages are accepted, and the writer
// closes the connection once it has written the queued ones, which ends the
// read loop. Repeated calls are no-ops.
func (d *Device) close() {
	d.closeOnce.Do(func() { close(d.closing) })
}

// DeviceManager manages devices
type DeviceManager struct {
	userDevices      sync.Map // userID -> []*Device
	totalOnline      atomic.Int32
	userMutexes      sync.Map // userID level locks
	heartbeatTimeout time.Duration
	checkInterval    time.Duration
	upgrader         websocket.Upgrader
	messageSlots     chan struct{}

	quit     chan struct{}
	quitOnce sync.Once

	// event callbacks
	OnDeviceOnline  func(userID int64, deviceID, session string)
	OnDeviceOffline func(userID int64, deviceID, session string, createAt time.Time)
	OnDeviceKicked  func(userID int64, deviceID, session string, operator Operator)
	OnMessage       func(userID int64, deviceID, session string, message string)
}

// NewDeviceManager creates a device manager that drops a device silent for
// heartbeatTimeout seconds, sweeping every checkInterval seconds (a value
// that is not positive falls back to 30). allowedOrigins are the browser
// origins admitted to connect; an empty list admits every origin, as the
// CORS middleware does.
func NewDeviceManager(heartbeatTimeout, checkInterval int, allowedOrigins []string) *DeviceManager {
	dm := &DeviceManager{
		heartbeatTimeout: positiveSeconds(heartbeatTimeout),
		checkInterval:    positiveSeconds(checkInterval),
		messageSlots:     make(chan struct{}, messageWorkers),
		quit:             make(chan struct{}),
	}
	dm.upgrader = websocket.Upgrader{CheckOrigin: originChecker(allowedOrigins)}
	go dm.StartHeartbeatCheck()
	return dm
}

func positiveSeconds(seconds int) time.Duration {
	if seconds <= 0 {
		return defaultInterval
	}
	return time.Duration(seconds) * time.Second
}

// originChecker admits an Origin from the allowed list, or any when the list
// is empty. A request without an Origin header is admitted too: the check
// defends against cross-site requests from browsers, which always send one,
// not against clients that set no Origin.
func originChecker(allowed []string) func(*http.Request) bool {
	origins := make(map[string]struct{}, len(allowed))
	for _, origin := range allowed {
		if origin = normalizeOrigin(origin); origin != "" {
			origins[origin] = struct{}{}
		}
	}
	_, any := origins["*"]
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" || len(origins) == 0 || any {
			return true
		}
		_, ok := origins[normalizeOrigin(origin)]
		return ok
	}
}

func normalizeOrigin(origin string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(origin), "/"))
}

// Online returns how many device sockets are connected.
func (dm *DeviceManager) Online() int {
	return int(dm.totalOnline.Load())
}

// Get user-level mutex
func (dm *DeviceManager) getUserMutex(userID int64) *sync.Mutex {
	mu, _ := dm.userMutexes.LoadOrStore(userID, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// snapshotDevices returns a copy of the user's device list. The slice is
// mutated in place by removals, so readers must copy it under the user lock
// before iterating.
func (dm *DeviceManager) snapshotDevices(userID int64) []*Device {
	mu := dm.getUserMutex(userID)
	mu.Lock()
	defer mu.Unlock()
	if val, ok := dm.userDevices.Load(userID); ok {
		return append([]*Device(nil), val.([]*Device)...)
	}
	return nil
}

// serve reads the device's socket until it disconnects, answering
// heartbeats and dispatching its messages, then retires the device. It
// returns only once the writer has stopped, so nothing touches the
// connection after it returns; a client silent for the heartbeat timeout,
// or sending a frame over the read limit, is disconnected.
func (dm *DeviceManager) serve(userID int64, device *Device) {
	defer func() {
		dm.removeDevice(userID, device)
		device.close()
		device.writer.Wait()
	}()

	conn := device.Conn
	conn.SetReadLimit(maxMessageSize)
	extend := func() error {
		return conn.SetReadDeadline(time.Now().Add(dm.heartbeatTimeout))
	}
	// A protocol-level ping counts as a heartbeat too.
	conn.SetPingHandler(func(appData string) error {
		device.touch(time.Now())
		_ = extend()
		err := conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(writeWait))
		var netErr net.Error
		if errors.Is(err, websocket.ErrCloseSent) || (errors.As(err, &netErr) && netErr.Timeout()) {
			return nil
		}
		return err
	})

	for {
		if err := extend(); err != nil {
			break
		}
		_, msg, err := conn.ReadMessage()
		if err != nil {
			logger.Infow("device disconnected", logger.Field("device_id", device.DeviceID), logger.Field("user_id", userID), logger.Field("error", err.Error()))
			break
		}
		device.touch(time.Now())

		message := string(msg)
		if message == "ping" || message == "heartbeat" {
			if err := device.enqueue(heartbeatReply); err != nil {
				logger.Errorw("device heartbeat response failed", logger.Field("device_id", device.DeviceID), logger.Field("user_id", userID), logger.Field("error", err.Error()))
			}
			continue
		}
		dm.dispatch(userID, device, message)
	}
}

// dispatch runs OnMessage on one of a bounded number of goroutines. When all
// of them are busy the read loop waits, which holds the client back instead
// of starting a goroutine per frame.
func (dm *DeviceManager) dispatch(userID int64, device *Device, message string) {
	if dm.OnMessage == nil {
		return
	}
	select {
	case dm.messageSlots <- struct{}{}:
	case <-device.closing:
		return
	case <-dm.quit:
		return
	}
	go func() {
		defer func() { <-dm.messageSlots }()
		dm.OnMessage(userID, device.DeviceID, device.Session, message)
	}()
}

// UpdateHeartbeat records a heartbeat of the device and answers it.
func (dm *DeviceManager) UpdateHeartbeat(userID int64, deviceID string) {
	for _, d := range dm.snapshotDevices(userID) {
		if d.DeviceID != deviceID {
			continue
		}
		d.touch(time.Now())
		if err := d.enqueue(heartbeatReply); err != nil {
			logger.Errorw("device heartbeat response failed", logger.Field("device_id", deviceID), logger.Field("user_id", userID), logger.Field("error", err.Error()))
		}
		return
	}
}

// AddDevice upgrades a net/http request to the device's socket, registers
// the device and serves it in the background.
func (dm *DeviceManager) AddDevice(w http.ResponseWriter, r *http.Request, session string, userID int64, deviceID string, maxDevices int) {
	conn, err := dm.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Errorw("device websocket upgrade failed", logger.Field("error", err.Error()))
		return
	}
	device := dm.register(conn, session, userID, deviceID, maxDevices)
	go dm.serve(userID, device)
}

// register adds a freshly upgraded socket to the user's devices and starts
// its writer. A reconnect of the same device retires the previous socket; a
// user over the device limit loses the earliest one. The caller serves the
// returned device.
func (dm *DeviceManager) register(conn *websocket.Conn, session string, userID int64, deviceID string, maxDevices int) *Device {
	// A non-positive limit means no explicit cap is configured; fall back to
	// the historical default instead of allowing unlimited connections.
	if maxDevices < 1 {
		maxDevices = defaultMaxDevices
	}

	newDevice := newDevice(conn, session, deviceID, time.Now())
	newDevice.startWriter(userID)

	var kicked, replaced *Device
	mu := dm.getUserMutex(userID)
	mu.Lock()
	var devices []*Device
	if val, ok := dm.userDevices.Load(userID); ok {
		for _, d := range val.([]*Device) {
			if d.DeviceID == deviceID {
				replaced = d // a reconnect retires the previous socket
				continue
			}
			devices = append(devices, d)
		}
	}

	// Over the limit, the earliest device is kicked.
	if replaced == nil && len(devices) >= maxDevices {
		kicked = devices[0]
		devices = devices[1:]
	}

	// Add new device. A reconnect only swaps sockets, so the online count is
	// settled here, under the lock, before anyone can observe the old socket
	// closing.
	devices = append(devices, newDevice)
	dm.userDevices.Store(userID, devices)
	if replaced == nil {
		dm.totalOnline.Add(1)
	}
	mu.Unlock()

	// Side effects run outside the user lock: the kick callback calls back
	// into SendToDevice, which takes the lock, so running it under the lock
	// would deadlock. The kicked device stays registered until the callback
	// returns so its notification is actually delivered.
	if kicked != nil {
		if dm.OnDeviceKicked != nil {
			dm.OnDeviceKicked(userID, kicked.DeviceID, kicked.Session, MaxDevices)
		}
		dm.removeDevice(userID, kicked)
	}
	if replaced != nil {
		replaced.close()
	}

	// Trigger online event
	if dm.OnDeviceOnline != nil {
		go dm.OnDeviceOnline(userID, deviceID, session)
	}
	return newDevice
}

// removeDevice removes a device, matching by connection identity: several
// sockets may share a DeviceID during a reconnect, and each read loop must
// retire only its own connection. Closing the connection, decrementing the
// online counter and emitting OnDeviceOffline happen only when the device is
// still registered, so retried removals are no-ops.
func (dm *DeviceManager) removeDevice(userID int64, device *Device) {
	mu := dm.getUserMutex(userID)
	mu.Lock()
	defer mu.Unlock()

	if val, ok := dm.userDevices.Load(userID); ok {
		devices := val.([]*Device)
		for i, d := range devices {
			if d != device {
				continue
			}
			devices = append(devices[:i], devices[i+1:]...)
			dm.totalOnline.Add(-1)
			d.close()

			if dm.OnDeviceOffline != nil {
				go dm.OnDeviceOffline(userID, d.DeviceID, d.Session, d.CreatedAt)
			}
			break
		}

		if len(devices) == 0 {
			dm.userDevices.Delete(userID)
		} else {
			dm.userDevices.Store(userID, devices)
		}
	}
}

// KickDevice kicks a device (supports individual device or entire user)
func (dm *DeviceManager) KickDevice(userID int64, deviceID string) {
	devices := dm.snapshotDevices(userID)

	var kicked []*Device
	for _, d := range devices {
		if deviceID == "" || d.DeviceID == deviceID {
			kicked = append(kicked, d)
		}
	}
	if len(kicked) == 0 {
		logger.Infow("user has no online devices to kick", logger.Field("user_id", userID))
		return
	}

	// Callbacks run while the device is still registered so the notification
	// can be delivered through SendToDevice; removeDevice then closes the
	// socket and emits OnDeviceOffline.
	for _, d := range kicked {
		if dm.OnDeviceKicked != nil {
			dm.OnDeviceKicked(userID, d.DeviceID, d.Session, Admin)
		}
		dm.removeDevice(userID, d)
		logger.Infow("device kicked", logger.Field("device_id", d.DeviceID), logger.Field("user_id", userID))
	}
}

// StartHeartbeatCheck periodically checks for heartbeat timeout devices
func (dm *DeviceManager) StartHeartbeatCheck() {
	ticker := time.NewTicker(dm.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-dm.quit:
			return
		case <-ticker.C:
			dm.checkHeartbeats()
		}
	}
}

// checkHeartbeats drops the devices whose heartbeat is overdue. Each user's
// list is rebuilt under the user's lock; the sockets are closed outside it,
// so the sweep does no I/O while holding any lock.
func (dm *DeviceManager) checkHeartbeats() {
	now := time.Now()

	dm.userDevices.Range(func(userID, _ any) bool {
		uid := userID.(int64)
		for _, d := range dm.expireDevices(uid, now) {
			logger.Infow("device heartbeat timed out", logger.Field("device_id", d.DeviceID), logger.Field("user_id", uid))
			d.close()
			if dm.OnDeviceOffline != nil {
				go dm.OnDeviceOffline(uid, d.DeviceID, d.Session, d.CreatedAt)
			}
		}
		return true
	})
	// Deliberately avoid logging every heartbeat sweep.
}

// expireDevices removes the user's devices whose heartbeat is older than the
// timeout at now and returns them.
func (dm *DeviceManager) expireDevices(userID int64, now time.Time) []*Device {
	mu := dm.getUserMutex(userID)
	mu.Lock()
	defer mu.Unlock()

	// Range's value predates the lock: a device that connected while the
	// sweep waited for it is only in the list stored now. The list is
	// rebuilt from that one, or the device would be dropped from the map
	// with its socket open and its online count kept.
	val, ok := dm.userDevices.Load(userID)
	if !ok {
		return nil
	}
	var expired, active []*Device
	for _, d := range val.([]*Device) {
		if now.Sub(d.LastPingTime()) > dm.heartbeatTimeout {
			expired = append(expired, d)
			dm.totalOnline.Add(-1)
		} else {
			active = append(active, d)
		}
	}
	if len(active) == 0 {
		dm.userDevices.Delete(userID)
	} else {
		dm.userDevices.Store(userID, active)
	}
	return expired
}

// Stop terminates the heartbeat sweep. It is idempotent so lifecycle hooks
// can call it unconditionally.
func (dm *DeviceManager) Stop() {
	dm.quitOnce.Do(func() { close(dm.quit) })
}

// SendToDevice queues a message for a device of the user, or for all of them
// when deviceID is empty. It does not wait for the socket: a device that is
// not draining its socket refuses the message with ErrSendQueueFull.
func (dm *DeviceManager) SendToDevice(userID int64, deviceID string, message string) error {
	devices := dm.snapshotDevices(userID)
	for _, d := range devices {
		if deviceID == "" {
			if err := d.enqueue(message); err != nil {
				return fmt.Errorf("device %s (User %d): %w", d.DeviceID, userID, err)
			}
			continue
		}
		if d.DeviceID == deviceID {
			return d.enqueue(message)
		}
	}
	if deviceID == "" && len(devices) > 0 {
		return nil
	}
	return fmt.Errorf("device %s (User %d) is offline", deviceID, userID)
}

// Broadcast queues a message for every connected device. Devices that are
// not draining their sockets are skipped.
func (dm *DeviceManager) Broadcast(message string) {
	dm.userDevices.Range(func(userID, _ any) bool {
		for _, d := range dm.snapshotDevices(userID.(int64)) {
			_ = d.enqueue(message)
		}
		return true
	})
}

// Shutdown waits for ctx to end, then stops the sweep and closes every
// device socket.
func (dm *DeviceManager) Shutdown(ctx context.Context) {
	<-ctx.Done()
	dm.Stop()
	logger.Info("shutting down all device websocket connections")

	dm.userDevices.Range(func(userID, _ any) bool {
		uid := userID.(int64)
		for _, d := range dm.snapshotDevices(uid) {
			dm.removeDevice(uid, d)
		}
		return true
	})
}
