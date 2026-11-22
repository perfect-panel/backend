// Package routes is the URL table of the HTTP API: it registers every route
// with its middleware and hands each handler its module's facade. The
// handlers live in the modules' transport/http packages; composing them here
// keeps the whole API surface in one place, pinned by the route inventory in
// testdata/routes.golden.
package routes

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/billing"
	"github.com/perfect-panel/backend/internal/module/identity"
	"github.com/perfect-panel/backend/internal/module/network"
	"github.com/perfect-panel/backend/internal/module/platform"
	"github.com/perfect-panel/backend/internal/module/subscription"
	"github.com/perfect-panel/backend/internal/module/support"
	"github.com/perfect-panel/backend/internal/transport/devicesocket"
	"github.com/perfect-panel/backend/internal/transport/http/middleware"
	"github.com/redis/go-redis/v9"
)

// Dependencies is the HTTP routing boundary. Each handler receives one module
// facade or a smaller endpoint-specific dependency set from this structure.
type Dependencies struct {
	Config         config.Config
	ConfigProvider func() config.Config
	Redis          *redis.Client
	Support        support.Service
	Billing        billing.Service
	Platform       platform.Service
	Subscription   subscription.Service
	Identity       identity.Service
	Network        network.Service
	// Devices is the device WebSocket manager the device route serves;
	// DeviceLimit caps the sockets one account may keep open.
	Devices     *devicesocket.DeviceManager
	DeviceLimit func(ctx context.Context, userID int64) int
}

func (deps Dependencies) runtimeConfig() config.Config {
	if deps.ConfigProvider != nil {
		return deps.ConfigProvider()
	}
	return deps.Config
}

func (deps Dependencies) subscribeConfig() config.SubscribeConfig {
	return deps.runtimeConfig().Subscribe
}

func (deps Dependencies) edgeSubscribeConfig() config.EdgeSubscribeConfig {
	return deps.runtimeConfig().EdgeSubscribe
}

func (deps Dependencies) nodeSecret() string {
	return deps.runtimeConfig().Node.NodeSecret
}

// authDeps resolves sessions through the identity facade, which owns the
// accounts and devices a session belongs to.
func (deps Dependencies) authDeps() middleware.AuthDeps {
	return middleware.AuthDeps{
		JWT: deps.runtimeConfig().JwtAuth, Redis: deps.Redis, Accounts: deps.Identity,
	}
}

func (deps Dependencies) authMiddleware() app.HandlerFunc {
	return middleware.AuthMiddleware(deps.authDeps())
}

func (deps Dependencies) optionalAuthMiddleware() app.HandlerFunc {
	return middleware.OptionalAuthMiddleware(deps.authDeps())
}

// adminGroup opens an admin route group. It is the only way to open one
// (TestAdminGroupsAreGuarded): it installs authentication and the
// administrator guard, so no admin route can be registered without them.
func (deps Dependencies) adminGroup(router *server.Hertz, path string) *route.RouterGroup {
	group := router.Group(path)
	group.Use(deps.authMiddleware(), middleware.AdminGuard())
	return group
}

func (deps Dependencies) deviceMiddleware() app.HandlerFunc {
	if deps.Redis == nil {
		return middleware.DeviceMiddleware(func() config.DeviceConfig { return deps.runtimeConfig().Device }, nil)
	}
	return middleware.DeviceMiddleware(func() config.DeviceConfig { return deps.runtimeConfig().Device }, deps.Redis)
}

func RegisterHandlers(router *server.Hertz, deps Dependencies) {
	registerEdgeRoutes(router, deps)
	registerSubscribeConfigRoutes(router, deps)
	registerServerRoutes(router, deps)
	registerDeviceRoutes(router, deps)

	registerAdminAdsRoutes(router, deps)
	registerAdminAnnouncementRoutes(router, deps)
	registerAdminApplicationRoutes(router, deps)
	registerAdminAuthMethodRoutes(router, deps)
	registerAdminConsoleRoutes(router, deps)
	registerAdminCouponRoutes(router, deps)
	registerAdminDocumentRoutes(router, deps)
	registerAdminLogRoutes(router, deps)
	registerAdminMarketingRoutes(router, deps)
	registerAdminOrderRoutes(router, deps)
	registerAdminPaymentRoutes(router, deps)
	registerAdminWithdrawalRoutes(router, deps)
	registerAdminServerRoutes(router, deps)
	registerAdminSubscribeRoutes(router, deps)
	registerAdminSystemRoutes(router, deps)
	registerAdminTicketRoutes(router, deps)
	registerAdminToolRoutes(router, deps)
	registerAdminUserRoutes(router, deps)

	registerAuthRoutes(router, deps)
	registerCommonRoutes(router, deps)

	registerPublicAnnouncementRoutes(router, deps)
	registerPublicDocumentRoutes(router, deps)
	registerPublicOrderRoutes(router, deps)
	registerPublicOrderV2Routes(router, deps)
	registerPublicPaymentRoutes(router, deps)
	registerPublicPortalRoutes(router, deps)
	registerPublicSubscribeRoutes(router, deps)
	registerPublicTicketRoutes(router, deps)
	registerPublicUserRoutes(router, deps)
}
