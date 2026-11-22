// Package middleware holds the Hertz middleware of the HTTP API: tracing,
// request logging and request metadata, CORS, session authentication and the
// administrator guard, the device transport envelope and the payment callback
// lookup. Middleware that needs module data declares the narrow interface it
// uses (SessionAccounts, PaymentMethods) and a module facade provides it, so
// the transport layer never reads a module's tables itself.
package middleware

import (
	"context"
	"errors"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/auth/usersession"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/httpx"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

// SessionAccounts resolves the account and device of a session. The
// identity facade provides it: the accounts and devices are identity's
// tables, which only the identity module reads.
type SessionAccounts interface {
	// FindUser returns the account, soft-deleted ones included, so a deleted
	// account is refused rather than reported as a lookup failure.
	FindUser(ctx context.Context, id int64) (*user.User, error)
	// FindAccountStateForAuth reads the account gate (enabled, deleted,
	// administrator) as stored now, bypassing caches, so a ban, deletion or
	// demotion refuses the account's sessions at once.
	FindAccountStateForAuth(ctx context.Context, id int64) (*user.AccountState, error)
	// FindDeviceForAuth reads the current device state, bypassing caches.
	FindDeviceForAuth(ctx context.Context, id int64) (*user.Device, error)
}

// AuthDeps is what request authentication needs: the session signing
// secret, the session store and the lookups of the session's account and
// device.
type AuthDeps struct {
	JWT   config.JwtAuth
	Redis *redis.Client
	// Accounts resolves the session's account and device.
	Accounts SessionAccounts
}

func AuthMiddleware(deps AuthDeps) app.HandlerFunc {
	return func(ctx context.Context, requestCtx *app.RequestContext) {
		ctx, err := AuthenticateRequest(ctx, deps, string(requestCtx.GetHeader("Authorization")))
		if err != nil {
			httpx.HttpResult(requestCtx, nil, err)
			requestCtx.Abort()
			return
		}
		if metadata, ok := requestmeta.From(ctx); ok && metadata.ActorID > 0 {
			requestCtx.Set(requestActorIDKey, metadata.ActorID)
		}
		requestCtx.Next(ctx)
	}
}

// OptionalAuthMiddleware authenticates a request when it supplies an
// Authorization header, while preserving anonymous access for routes that
// intentionally support guest checkout.  Handlers on those routes must still
// explicitly require an authenticated user before operating on a user-owned
// resource.
func OptionalAuthMiddleware(deps AuthDeps) app.HandlerFunc {
	return func(ctx context.Context, requestCtx *app.RequestContext) {
		token := string(requestCtx.GetHeader("Authorization"))
		if token == "" {
			requestCtx.Next(ctx)
			return
		}

		authenticatedCtx, err := AuthenticateRequest(ctx, deps, token)
		if err != nil {
			httpx.HttpResult(requestCtx, nil, err)
			requestCtx.Abort()
			return
		}
		if metadata, ok := requestmeta.From(authenticatedCtx); ok && metadata.ActorID > 0 {
			requestCtx.Set(requestActorIDKey, metadata.ActorID)
		}
		requestCtx.Next(authenticatedCtx)
	}
}

// AdminGuard admits only administrators. It runs after AuthMiddleware on the
// admin route groups, which the routes package opens only through a helper
// that installs both.
func AdminGuard() app.HandlerFunc {
	return func(ctx context.Context, requestCtx *app.RequestContext) {
		if err := RequireAdmin(ctx); err != nil {
			httpx.HttpResult(requestCtx, nil, err)
			requestCtx.Abort()
			return
		}
		requestCtx.Next(ctx)
	}
}

// RequireAdmin reports whether the request's authenticated user is an
// administrator.
func RequireAdmin(ctx context.Context) error {
	u, ok := user.FromContext(ctx)
	if !ok {
		return xerr.Errorf(xerr.InvalidAccess, "admin access needs a signed-in user")
	}
	if u.IsAdmin == nil || !*u.IsAdmin {
		return xerr.Errorf(xerr.InvalidAccess, "user %d is not an administrator", u.Id)
	}
	return nil
}

// AuthenticateRequest resolves the session token to its account and returns
// ctx carrying the account, the session and the actor. A device session also
// needs its device to be enabled and still the account's.
//
// The account gate (enabled, deleted, administrator) is read as stored now,
// not from the cached account row: that row lives for days and its
// invalidation is best effort, so a ban, deletion or demotion must not be
// served from it. The cached row still supplies the rest of the account,
// with the gate columns overlaid, so handlers see the current flags too.
func AuthenticateRequest(ctx context.Context, deps AuthDeps, token string) (context.Context, error) {
	if token == "" {
		logger.WithContext(ctx).Debug("[AuthMiddleware] Token Empty")
		return ctx, xerr.Errorf(xerr.ErrorTokenEmpty, "token empty")
	}

	claims, err := usersession.Validate(ctx, deps.Redis, deps.JWT.AccessSecret, token)
	if err != nil {
		logger.WithContext(ctx).Debug("[AuthMiddleware] session refused", logger.Field("error", err.Error()))
		if errors.Is(err, usersession.ErrInvalidToken) {
			return ctx, xerr.Errorf(xerr.ErrorTokenExpire, "token invalid")
		}
		return ctx, xerr.Wrapf(err, xerr.InvalidAccess, "%v", err)
	}
	accounts := deps.Accounts
	if accounts == nil {
		return ctx, xerr.Errorf(xerr.ERROR, "session accounts unavailable")
	}
	if claims.DeviceID != 0 {
		device, err := accounts.FindDeviceForAuth(ctx, claims.DeviceID)
		if err != nil || device == nil || !device.Enabled || device.UserId != claims.UserID {
			return ctx, xerr.Errorf(xerr.InvalidAccess, "device is unavailable")
		}
	}

	state, err := accounts.FindAccountStateForAuth(ctx, claims.UserID)
	if err != nil {
		return ctx, xerr.Wrapf(err, xerr.DatabaseQueryError, "find account state %d", claims.UserID)
	}
	if state.DeletedAt.Valid {
		return ctx, xerr.Errorf(xerr.UserNotExist, "user deleted")
	}
	if state.Enable == nil || !*state.Enable {
		return ctx, xerr.Errorf(xerr.UserDisabled, "user disabled")
	}
	userInfo, err := accounts.FindUser(ctx, claims.UserID)
	if err != nil {
		return ctx, xerr.Wrapf(err, xerr.DatabaseQueryError, "find user %d", claims.UserID)
	}
	// The gate columns of the cached row give way to the current ones, so
	// the administrator guard and the handlers see a demotion or a ban the
	// cache has not caught up with.
	userInfo.Enable, userInfo.DeletedAt = state.Enable, state.DeletedAt
	if state.IsAdmin != nil {
		userInfo.IsAdmin = state.IsAdmin
	}

	ctx = context.WithValue(ctx, requestctx.LoginType, claims.LoginType)
	ctx = user.NewContext(ctx, userInfo)
	ctx = context.WithValue(ctx, requestctx.CtxKeySessionID, claims.SessionID)
	ctx = requestmeta.WithActor(ctx, claims.UserID)
	ctx = logger.ContextWithFields(ctx, logger.Field("actor_id", claims.UserID))
	return ctx, nil
}
