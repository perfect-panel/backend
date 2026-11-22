package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/auth/usersession"
	"github.com/perfect-panel/backend/internal/transport/devicesocket"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// NewDeviceManager builds the device WebSocket manager. The devices'
// presence is the identity module's: the online and offline callbacks record
// it through the identity facade, resolved when a callback runs because the
// manager is built before the facade. The socket has no one to report a
// failed record to, so it is only logged. Browsers may connect from the boot
// configuration's AllowedOrigins; none configured admits every origin, as
// the CORS middleware does.
func NewDeviceManager(srv *Application) *devicesocket.DeviceManager {
	// The socket callbacks run on the connections' goroutines, outside any
	// request, so their writes use a root context.
	ctx := context.Background()
	manager := devicesocket.NewDeviceManager(30, 30, srv.Runtime.Config().AllowedOrigins)

	manager.OnDeviceOffline = func(userID int64, deviceID, session string, createAt time.Time) {
		if err := srv.Identity.MarkDeviceOffline(ctx, userID, deviceID, createAt); err != nil {
			logger.Errorw("[DeviceManager] record device offline failed", logger.Field("error", err.Error()), logger.Field("device_id", deviceID))
		}
	}

	manager.OnDeviceOnline = func(userID int64, deviceID, session string) {
		if err := srv.Identity.MarkDeviceOnline(ctx, deviceID); err != nil {
			logger.Errorw("[DeviceManager] record device online failed", logger.Field("error", err.Error()), logger.Field("device_id", deviceID))
		}
	}

	manager.OnDeviceKicked = func(userID int64, deviceID, session string, operator devicesocket.Operator) {
		var message DeviceMessage
		switch operator {
		case devicesocket.Admin:
			// An administrator kicked the device.
			message = DeviceMessage{Method: DeviceKickedAdmin}
		case devicesocket.MaxDevices:
			// The user signed in on more devices than the limit.
			message = DeviceMessage{Method: DeviceKickedMax}
		default:
			return
		}
		_ = manager.SendToDevice(userID, deviceID, message.Json())
		// The kicked session ends; the user's other sessions stay.
		if err := usersession.End(ctx, srv.Redis, session); err != nil {
			logger.Errorw("[DeviceManager] end kicked session failed", logger.Field("error", err.Error()), logger.Field("device_id", deviceID))
		}
	}

	manager.OnMessage = func(userID int64, deviceID, session string, message string) {
		logger.Infof("userid: %d ,device_number: %s,session: %s, message: %v", userID, deviceID, session, message)
	}
	return manager
}

// DeviceSocketHandler returns the Hertz handler of the device WebSocket
// route, GET /v1/app/ws/:userid/:identifier, which the routes register
// behind the session authentication. A user may keep as many sockets open
// as the largest device limit among their unexpired subscriptions allows,
// as the route computed it before it went unregistered; without one the
// manager's default cap applies.
func (srv *Application) DeviceSocketHandler() app.HandlerFunc {
	return devicesocket.Handler(srv.DeviceManager, devicesocket.HandlerDeps{MaxDevices: srv.deviceLimit})
}

// deviceLimit is the largest device limit among the signed-in user's
// unexpired subscriptions, read through the subscription facade from the
// authenticated request context; zero when there is none or the read fails.
func (srv *Application) deviceLimit(ctx context.Context, userID int64) int {
	resp, err := srv.Subscription.QueryUserSubscribe(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[DeviceManager] read the subscriptions for the device limit failed", logger.Field("user_id", userID), logger.Field("error", err.Error()))
		return 0
	}
	// The view's times are Unix milliseconds.
	now := timeutil.Now().UnixMilli()
	limit := 0
	for _, sub := range resp.List {
		if sub.ExpireTime > now && int(sub.Subscribe.DeviceLimit) > limit {
			limit = int(sub.Subscribe.DeviceLimit)
		}
	}
	return limit
}

// DeviceMessage is a message the server pushes to a connected device.
type DeviceMessage struct {
	Method DeviceMessageMethod `json:"method"`
}

// Json encodes the message as the device protocol's JSON text.
func (dm *DeviceMessage) Json() string {
	jsonData, _ := json.Marshal(dm)
	return string(jsonData)
}

// DeviceMessageMethod names what a device message tells the device.
type DeviceMessageMethod string

const (
	// DeviceKickedMax tells a device it was signed out because its user
	// signed in on more devices than the limit allows.
	DeviceKickedMax DeviceMessageMethod = "kicked_device"
	// DeviceKickedAdmin tells a device an administrator signed it out.
	DeviceKickedAdmin DeviceMessageMethod = "kicked_admin"
	// SubscribeUpdate tells a device its subscription changed.
	SubscribeUpdate DeviceMessageMethod = "subscribe_update"
)
