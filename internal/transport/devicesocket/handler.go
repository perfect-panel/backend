package devicesocket

import (
	"context"
	"errors"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/httpx"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// HandlerDeps is what the device socket route needs besides the manager.
type HandlerDeps struct {
	// MaxDevices returns how many sockets the user may keep open at once;
	// nil, or a result of zero or less, leaves the manager's default cap.
	MaxDevices func(ctx context.Context, userID int64) int
}

// Handler returns the Hertz handler of the device WebSocket route. It runs
// behind the session authentication middleware, which puts the account and
// its session in the context; the handler refuses a request without them,
// or whose user id is not the signed-in user's, and then upgrades the
// connection to the device's socket. The device identifier is the label
// the user's client chose; kicks and pushes address the device by it.
//
// @Summary Device WebSocket
// @Description Upgrades the connection to the signed-in user's device socket. The client sends "ping" (or "heartbeat") as its heartbeat and is answered "ping"; the server pushes JSON messages such as {"method":"subscribe_update"}, {"method":"kicked_device"} and {"method":"kicked_admin"}.
// @Tags user
// @Security BearerAuth
// @Param userid path int true "User ID of the signed-in user"
// @Param identifier path string true "Device identifier"
// @Success 101 {string} string "Switching Protocols"
// @Router /v1/app/ws/{userid}/{identifier} [get]
func Handler(manager *DeviceManager, deps HandlerDeps) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if manager == nil {
			httpx.HttpResult(c, nil, xerr.NewErrCode(xerr.ERROR))
			return
		}
		account, ok := user.FromContext(ctx)
		session, _ := ctx.Value(requestctx.CtxKeySessionID).(string)
		if !ok || account == nil || session == "" {
			httpx.HttpResult(c, nil, xerr.NewErrCode(xerr.ErrorTokenInvalid))
			return
		}
		userID, err := strconv.ParseInt(c.Param("userid"), 10, 64)
		if err != nil || userID != account.Id {
			httpx.HttpResult(c, nil, xerr.NewErrCode(xerr.UseridNotMatch))
			return
		}
		identifier := c.Param("identifier")
		if identifier == "" {
			httpx.HttpResult(c, nil, xerr.NewErrCode(xerr.DeviceNotExist))
			return
		}
		maxDevices := 0
		if deps.MaxDevices != nil {
			maxDevices = deps.MaxDevices(ctx, userID)
		}
		if err := manager.UpgradeHertz(c, session, userID, identifier, maxDevices); err != nil {
			var refused *HandshakeError
			if errors.As(err, &refused) {
				c.String(refused.Status, refused.Reason)
				return
			}
			httpx.HttpResult(c, nil, err)
		}
	}
}
