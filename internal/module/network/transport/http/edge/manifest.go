// Package edge holds the HTTP handler of the edge manifest, authenticated by
// the edge credential.
package edge

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/network"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/internal/edgeauth"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/redis/go-redis/v9"
)

// ManifestBuilder is the part of the network facade ManifestHandler calls.
type ManifestBuilder interface {
	EdgeManifest(ctx context.Context, token string) (*dto.EdgeManifestResponse, error)
}

var _ ManifestBuilder = network.Service(nil)

type ManifestDeps struct {
	Network ManifestBuilder
	Redis   *redis.Client
	Config  func() config.EdgeSubscribeConfig
}

// ManifestHandler serves the private Edge Manifest contract. It does not use
// normal user authentication and does not emit the application's JSON envelope.
// A failed credential and an unknown user token deliberately share a 404 reply.
//
// @Summary Get Edge subscription manifest
// @Tags edge
// @Produce json
// @Param token query string true "Subscription token"
// @Param Authorization header string true "PPanel Edge HMAC credential"
// @Param X-Request-ID header string true "One-time UUID bound into the HMAC"
// @Success 200 {object} dto.EdgeManifestResponse
// @Failure 404 {string} string
// @Router /api/edge/v1/manifest [get]
func ManifestHandler(deps ManifestDeps) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		config := deps.Config()
		token := strings.TrimSpace(ctx.Query("token"))
		kid, valid := edgeauth.AuthenticateManifestRequest(string(ctx.GetHeader("Authorization")), token, string(ctx.GetHeader("X-Request-ID")), config, time.Now())
		if !valid {
			ctx.String(consts.StatusNotFound, "Not Found")
			return
		}
		// A nil client must reach ClaimManifestRequest as a nil interface:
		// wrapped in redis.Cmdable it would pass its fail-closed check and
		// panic.
		var replayCache redis.Cmdable
		if deps.Redis != nil {
			replayCache = deps.Redis
		}
		claimed, err := edgeauth.ClaimManifestRequest(c, replayCache, kid, string(ctx.GetHeader("X-Request-ID")), config)
		if err != nil {
			logger.WithContext(c).Errorw("[Edge Manifest] replay protection unavailable", logger.Field("error", err.Error()))
			ctx.String(consts.StatusServiceUnavailable, "Service Unavailable")
			return
		}
		if !claimed {
			ctx.String(consts.StatusNotFound, "Not Found")
			return
		}

		response, err := deps.Network.EdgeManifest(c, token)
		if err != nil {
			if errors.Is(err, network.ErrManifestNotFound) {
				ctx.String(consts.StatusNotFound, "Not Found")
				return
			}
			logger.WithContext(c).Errorw("[Edge Manifest] build failed", logger.Field("error", err.Error()))
			ctx.String(consts.StatusInternalServerError, "Internal Server Error")
			return
		}
		ctx.Header("Cache-Control", "no-store")
		ctx.JSON(consts.StatusOK, response)
	}
}
