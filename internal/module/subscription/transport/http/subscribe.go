// Package handler serves subscription delivery over HTTP: the client
// configuration for a subscription token or a pan-domain host.
package handler

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/infra/protocolkey"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Deliverer is the part of the subscription facade the delivery endpoints
// use: the user-agent allowlist and the rendering of a subscription's client
// configuration.
type Deliverer interface {
	IsUserAgentAllowed(ctx context.Context, userAgent string) bool
	Deliver(ctx context.Context, meta subscription.RequestMeta, req *dto.SubscribeRequest) (*dto.SubscribeResponse, error)
}

var _ Deliverer = subscription.Service(nil)

type SubscribeDeps struct {
	Service Deliverer
	Config  func() config.SubscribeConfig
}

// SubscribeHandler returns a client subscription configuration.
//
// @Summary Get subscription configuration
// @Tags user
// @Produce plain
// @Param token query string false "Subscription token; alternatively send the token header"
// @Param token header string false "Subscription token"
// @Param flag query string false "Subscription format flag"
// @Param type query string false "Subscription format type"
// @Param User-Agent header string false "Client user agent"
// @Success 200 {string} string
// @Router /v1/subscribe/config [get]
func SubscribeHandler(deps SubscribeDeps) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		req := dto.SubscribeRequest{
			Token:  string(ctx.GetHeader("token")),
			Params: getQueryMap(ctx),
		}
		if req.Token == "" {
			req.Token = ctx.Query("token")
		}

		config := deps.Config()
		if config.PanDomain {
			domainArr := strings.Split(string(ctx.Host()), ".")
			short, err := protocolkey.FixedUniqueString(req.Token, 8, "")
			if err != nil {
				logger.WithContext(c).Errorf("[SubscribeHandler] Generate short token failed: %v", err)
				ctx.String(consts.StatusInternalServerError, "Internal Server")
				return
			}
			if !strings.EqualFold(short, domainArr[0]) {
				logger.WithContext(c).Debug("[SubscribeHandler] short token mismatch")
				ctx.String(consts.StatusForbidden, "Access denied")
				return
			}
		}

		if config.UserAgentLimit && !deps.Service.IsUserAgentAllowed(c, string(ctx.UserAgent())) {
			ctx.String(consts.StatusForbidden, "Access denied")
			return
		}
		writeSubscribeResponse(c, ctx, deps.Service, req)
	}
}

// PanDomainSubscribeHandler returns a subscription selected by the request host.
//
// @Summary Get pan-domain subscription configuration
// @Tags user
// @Produce plain
// @Param User-Agent header string false "Client user agent"
// @Success 200 {string} string
// @Router / [get]
func PanDomainSubscribeHandler(deps SubscribeDeps) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		config := deps.Config()
		if config.UserAgentLimit && !deps.Service.IsUserAgentAllowed(c, string(ctx.UserAgent())) {
			ctx.String(consts.StatusForbidden, "Access denied")
			return
		}

		// A subscription host is <token>.<label>.<domain>: a host without a
		// second label is not one.
		domainArr := strings.Split(string(ctx.Host()), ".")
		if len(domainArr) < 2 {
			ctx.String(consts.StatusForbidden, "Access denied")
			return
		}

		writeSubscribeResponse(c, ctx, deps.Service, dto.SubscribeRequest{
			Token:  domainArr[0],
			Params: getQueryMap(ctx),
		})
	}
}

func writeSubscribeResponse(c context.Context, ctx *app.RequestContext, service Deliverer, req dto.SubscribeRequest) {
	resp, err := service.Deliver(c, subscription.RequestMeta{
		Host:       string(ctx.Host()),
		RequestURI: string(ctx.URI().RequestURI()),
		UserAgent:  string(ctx.UserAgent()),
		ClientIP:   ctx.ClientIP(),
	}, &req)
	if err != nil {
		// A client over its fetch limit backs off on 429; every other
		// refusal stays the one plain-text server error.
		if xerr.CodeOf(err) == xerr.TooManyRequests {
			ctx.String(consts.StatusTooManyRequests, "Too Many Requests")
			return
		}
		ctx.String(consts.StatusInternalServerError, "Internal Server")
		return
	}
	// Data sets the content type it is given over any header set before, so
	// the type the delivery asks for (the downloadable formats are
	// application/octet-stream) is handed to it rather than set as a header.
	contentType := "text/plain; charset=utf-8"
	for key, value := range resp.Headers {
		if strings.EqualFold(key, "Content-Type") {
			contentType = value
			continue
		}
		ctx.Header(key, value)
	}
	ctx.Header("subscription-userinfo", resp.Header)
	ctx.Data(consts.StatusOK, contentType, resp.Config)
}

func getQueryMap(ctx *app.RequestContext) map[string]string {
	result := make(map[string]string)
	ctx.QueryArgs().VisitAll(func(key, value []byte) {
		k := string(key)
		if _, ok := result[k]; !ok {
			result[k] = string(value)
		}
	})
	return result
}
