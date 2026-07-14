package handler

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/server/internal/logic/subscribe"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
)

func SubscribeHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		req := dto.SubscribeRequest{
			Token:  string(ctx.GetHeader("token")),
			UA:     string(ctx.UserAgent()),
			Flag:   ctx.Query("flag"),
			Type:   ctx.Query("type"),
			Params: getQueryMap(ctx),
		}
		if req.Token == "" {
			req.Token = ctx.Query("token")
		}

		if svcCtx.Config.Subscribe.PanDomain {
			domainArr := strings.Split(string(ctx.Host()), ".")
			if len(domainArr) == 0 {
				ctx.String(consts.StatusForbidden, "Access denied")
				return
			}
			short, err := tool.FixedUniqueString(req.Token, 8, "")
			if err != nil {
				logger.WithContext(c).Errorf("[SubscribeHandler] Generate short token failed: %v", err)
				ctx.String(consts.StatusInternalServerError, "Internal Server")
				return
			}
			if strings.ToLower(short) != strings.ToLower(domainArr[0]) {
				logger.WithContext(c).Debugf("[SubscribeHandler] short token mismatch, short: %s, domain: %s", short, domainArr[0])
				ctx.String(consts.StatusForbidden, "Access denied")
				return
			}
		}

		if svcCtx.Config.Subscribe.UserAgentLimit && !subscribe.IsUserAgentAllowed(c, svcCtx, req.UA) {
			ctx.String(consts.StatusForbidden, "Access denied")
			return
		}
		writeSubscribeResponse(c, ctx, svcCtx, req)
	}
}

func PanDomainSubscribeHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		ua := string(ctx.UserAgent())
		if svcCtx.Config.Subscribe.UserAgentLimit && !subscribe.IsUserAgentAllowed(c, svcCtx, ua) {
			ctx.String(consts.StatusForbidden, "Access denied")
			return
		}

		domainArr := strings.Split(string(ctx.Host()), ".")
		if len(domainArr) < 2 {
			ctx.String(consts.StatusForbidden, "Access denied")
			return
		}

		writeSubscribeResponse(c, ctx, svcCtx, dto.SubscribeRequest{
			Token:  domainArr[0],
			Flag:   domainArr[1],
			UA:     ua,
			Params: getQueryMap(ctx),
		})
	}
}

func writeSubscribeResponse(c context.Context, ctx *app.RequestContext, svcCtx *svc.ServiceContext, req dto.SubscribeRequest) {
	l := subscribe.NewSubscribeLogic(c, svcCtx, subscribe.RequestMeta{
		Host:       string(ctx.Host()),
		RequestURI: string(ctx.URI().RequestURI()),
		UserAgent:  string(ctx.UserAgent()),
		ClientIP:   ctx.ClientIP(),
	})
	resp, err := l.Handler(&req)
	if err != nil {
		ctx.String(consts.StatusInternalServerError, "Internal Server")
		return
	}
	for key, value := range resp.Headers {
		ctx.Header(key, value)
	}
	ctx.Header("subscription-userinfo", resp.Header)
	ctx.Data(consts.StatusOK, "text/plain; charset=utf-8", resp.Config)
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
