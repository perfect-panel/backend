// Package delivery implements the subscription delivery subdomain of the
// subscription module: token-authenticated rendering of client configs via
// the adapter, with notice placeholders for expired/exhausted subscriptions.
package delivery

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/client"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/internal/module/subscription/internal/render"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// RequestMeta carries the raw transport details of the subscription request.
type RequestMeta struct {
	Host       string
	RequestURI string
	UserAgent  string
	ClientIP   string
}

// Deliver renders the client configuration for a subscription token with the
// first client application whose user-agent keyword the request's user agent
// contains, or with the default application. The runtime configuration is
// read once per request.
//
// The token is the request's only credential, so a request without one that
// can be a token is refused before anything is looked up, and the fetches of
// each client address are limited (admitFetch) before the token costs a
// query.
func (s *Service) Deliver(ctx context.Context, meta RequestMeta, req *dto.SubscribeRequest) (*dto.SubscribeResponse, error) {
	lg := logger.WithContext(ctx)
	if !usersub.AcceptableToken(req.Token) {
		lg.Infow("[SubscribeLogic] Refusing a request without an acceptable token", logger.Field("client_ip", meta.ClientIP), logger.Field("token_length", len(req.Token)))
		return nil, xerr.Errorf(xerr.ErrorTokenInvalid, "subscribe token invalid")
	}
	if err := s.admitFetch(ctx, meta.ClientIP); err != nil {
		return nil, err
	}
	cfg := s.deps.config()
	clients, err := s.deps.Clients.List(ctx)
	if err != nil {
		lg.Errorw("[SubscribeLogic] Query client list failed", logger.Field("error", err.Error()))
		return nil, err
	}

	userAgent := strings.ToLower(meta.UserAgent)

	var targetApp, defaultApp *client.SubscribeApplication

	for _, item := range clients {
		u := strings.ToLower(item.UserAgent)
		if item.IsDefault {
			defaultApp = item
		}

		if strings.Contains(userAgent, u) {
			// A Stash user agent may also carry another client's keyword
			// (such as clash); only an application meant for Stash matches it.
			if strings.Contains(userAgent, "stash") && !strings.Contains(u, "stash") {
				continue
			}
			targetApp = item
			break
		}
	}
	if targetApp == nil {
		lg.Debugw("[SubscribeLogic] No matching client found", logger.Field("userAgent", userAgent))
		if defaultApp == nil {
			return nil, xerr.Errorf(xerr.ERROR, "No matching client found for user agent: %s", userAgent)
		}
		targetApp = defaultApp
	}
	userSubscribe, err := s.getUserSubscribe(ctx, req.Token)
	if err != nil {
		lg.Errorw("[SubscribeLogic] Get user subscribe failed", logger.Field("error", err.Error()))
		return nil, err
	}

	subscribeInfo, err := s.deps.Plans.FindOne(ctx, userSubscribe.SubscribeId)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			lg.Errorw("[SubscribeLogic] Find subscribe info failed", logger.Field("error", err.Error()), logger.Field("subscribeId", userSubscribe.SubscribeId))
			return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "Find subscribe info failed: %v", err.Error())
		}
		// A plan in use cannot be deleted, but ended subscriptions keep
		// pointing at a deleted one: their owners get a notice, not an
		// error.
		lg.Infow("[SubscribeLogic] Plan of the subscription no longer exists; delivering a notice",
			logger.Field("subscribeId", userSubscribe.SubscribeId), logger.Field("user_subscribe_id", userSubscribe.Id))
		subscribeInfo = nil
	}

	servers, err := s.getServers(ctx, cfg.SiteHost, userSubscribe, subscribeInfo)
	if err != nil {
		return nil, err
	}
	defaultParams, err := targetApp.DefaultParamValues()
	if err != nil {
		// A malformed default must not cost the user their subscription; fall back
		// to whatever the request carried.
		lg.Errorw("[SubscribeLogic] Ignoring malformed default params",
			logger.Field("application", targetApp.Name),
			logger.Field("defaultParams", targetApp.DefaultParams),
			logger.Field("error", err.Error()))
	}

	a := render.NewAdapter(
		targetApp.SubscribeTemplate,
		render.WithServers(servers),
		render.WithSiteName(cfg.SiteName),
		render.WithSubscribeName(planName(subscribeInfo)),
		render.WithOutputFormat(targetApp.OutputFormat),
		render.WithUserInfo(render.User{
			ID:           userSubscribe.Id,
			Password:     userSubscribe.UUID,
			ExpiredAt:    userSubscribe.ExpireTime,
			Download:     userSubscribe.Download,
			Upload:       userSubscribe.Upload,
			Traffic:      userSubscribe.Traffic,
			SubscribeURL: getSubscribeV2URL(cfg, meta),
		}),
		render.WithParams(mergeParams(defaultParams, req.Params)),
	)

	lg.Debugw("[SubscribeLogic] Building client config",
		logger.Field("user_id", userSubscribe.UserId),
		logger.Field("application", targetApp.Name),
	)

	adapterClient, err := a.Client()
	if err != nil {
		lg.Errorw("[SubscribeLogic] Client error", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, 500, "Client error: %v", err.Error())
	}
	bytes, err := adapterClient.Build()
	if err != nil {
		lg.Errorw("[SubscribeLogic] Build client config failed", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, 500, "Build client config failed: %v", err.Error())
	}

	var formats = []string{"json", "yaml", "conf"}

	headers := make(map[string]string)
	for _, format := range formats {
		if format == strings.ToLower(targetApp.OutputFormat) {
			headers["Content-Disposition"] = fmt.Sprintf("attachment;filename*=UTF-8''%s", url.PathEscape(cfg.SiteName))
			headers["Content-Type"] = "application/octet-stream; charset=UTF-8"
			if cfg.ProfileUpdateInterval > 0 {
				headers["profile-update-interval"] = fmt.Sprintf("%d", cfg.ProfileUpdateInterval)
			}
			if profileURL := strings.TrimSpace(cfg.ProfileWebPageURL); profileURL != "" {
				headers["profile-web-page-url"] = profileURL
			}
		}
	}

	resp := &dto.SubscribeResponse{
		Config: bytes,
		Header: fmt.Sprintf(
			"upload=%d;download=%d;total=%d;expire=%d",
			userSubscribe.Upload, userSubscribe.Download, userSubscribe.Traffic, expireHeader(userSubscribe.ExpireTime),
		),
		Headers: headers,
	}
	if err = s.logSubscribeActivity(ctx, meta, userSubscribe); err != nil {
		return nil, err
	}
	return resp, nil
}

// planName is the name a template shows for the plan; a deleted plan has
// none.
func planName(plan *subscribe.Subscribe) string {
	if plan == nil {
		return ""
	}
	return plan.Name
}

// getSubscribeV2URL is the subscription URL the client config carries: the
// request's path on the first configured subscribe domain, or on the
// request's own host without one.
func getSubscribeV2URL(cfg Config, meta RequestMeta) string {
	uri := meta.RequestURI
	if cfg.SubscribeDomain != "" {
		domains := strings.Split(cfg.SubscribeDomain, "\n")
		return fmt.Sprintf("https://%s%s", domains[0], uri)
	}
	return fmt.Sprintf("https://%s%s", meta.Host, uri)
}

// getUserSubscribe resolves the token to its subscription, refusing an
// unknown token and a deleted or disabled owner account.
func (s *Service) getUserSubscribe(ctx context.Context, token string) (*usersub.Subscribe, error) {
	lg := logger.WithContext(ctx)
	userSub, err := s.deps.UserSubs.FindOneSubscribeByToken(ctx, token)
	if err != nil {
		lg.Infow("[Generate Subscribe] find subscribe error", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find subscribe error: %v", err.Error())
	}
	// The repository contract does not rule out a missing row without an
	// error; the token resolves to nothing then.
	if userSub == nil {
		lg.Infow("[Generate Subscribe] token invalid or user not found")
		return nil, errors.New("subscribe token invalid")
	}
	userInfo, err := s.deps.Users.FindAccountState(ctx, userSub.UserId)
	if err != nil {
		lg.Infow("[Generate Subscribe] failed to get user info", logger.Field("error", err.Error()), logger.Field("userId", userSub.UserId))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "failed to get user info: %v", err.Error())
	}
	if userInfo.DeletedAt.Valid {
		lg.Infow("[Generate Subscribe] user account is deleted", logger.Field("userId", userSub.UserId))
		return nil, xerr.Errorf(xerr.UserNotExist, "User account does not exist")
	}
	if userInfo.Enable == nil || !*userInfo.Enable {
		lg.Infow("[Generate Subscribe] user account is disabled", logger.Field("userId", userSub.UserId))
		return nil, xerr.Errorf(xerr.UserDisabled, "User account is disabled")
	}

	// No status gate here: every subscription renders, and getServers
	// decides between real nodes and a notice placeholder (expired, out of
	// traffic, deducted or stopped).
	return userSub, nil
}

// logSubscribeActivity records the fetch: the only trail that the token was
// used, from where and by which client, so it stays synchronous and a fetch
// that cannot be recorded is refused. It costs one single-row INSERT into the
// append-only audit table. The row carries the request's metadata already, so
// the audit store has nothing to merge in and stores the content as encoded
// here instead of decoding and encoding it again.
func (s *Service) logSubscribeActivity(ctx context.Context, meta RequestMeta, userSub *usersub.Subscribe) error {
	metadata, _ := requestmeta.From(ctx)
	subscribeLog := log.Subscribe{
		IPMetadata:      metadata.IPMetadata,
		Token:           logger.RedactedValue,
		UserAgent:       meta.UserAgent,
		ClientIP:        meta.ClientIP,
		UserSubscribeId: userSub.Id,
		ActorID:         metadata.ActorID,
	}

	content, err := subscribeLog.Marshal()
	if err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "marshal subscription audit log")
	}

	err = s.deps.Logs.Insert(ctx, &log.SystemLog{
		Type:     log.TypeSubscribe.Uint8(),
		ObjectID: userSub.UserId,
		Date:     timeutil.Now().Format(time.DateOnly),
		Content:  string(content),
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("[Generate Subscribe] Insert subscription audit log failed", logger.Field("error", err.Error()), logger.Field("user_subscribe_id", userSub.Id))
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "insert subscription audit log")
	}
	return nil
}

// expireHeader is the subscription-userinfo expiry in Unix seconds; 0 tells
// clients there is none, for the epoch sentinel and a NULL expiry alike.
func expireHeader(expireTime time.Time) int64 {
	if usersub.NoExpiry(expireTime) {
		return 0
	}
	return expireTime.Unix()
}

// Notices a client shows in place of real nodes, by why the subscription may
// not use the service.
const (
	noticeUnavailable = "订阅不可用 / Subscribe Unavailable"
	noticeExpired     = "订阅已过期 / Subscribe Expired"
	noticeExhausted   = "流量已用尽 / Traffic Exhausted"
)

// unavailableNotice returns the notice for a subscription that may not use
// the service, or "" for one that may. It asks the same rule the node user
// list, the storefront and the edge manifest ask (usersub.AvailabilityAt).
func unavailableNotice(sub *usersub.Subscribe, now time.Time) string {
	switch sub.AvailabilityAt(now) {
	case usersub.Available:
		return ""
	case usersub.Expired:
		return noticeExpired
	case usersub.TrafficExhausted:
		return noticeExhausted
	default:
		// Refunded, stopped or a status no rule serves.
		return noticeUnavailable
	}
}

// getServers returns the nodes the client config lists. Subscriptions that
// may not use the service get notice placeholders instead of real nodes, so
// the client shows why; so does a subscription whose plan (nil) was deleted,
// which has no nodes left to list.
func (s *Service) getServers(ctx context.Context, siteHost string, userSub *usersub.Subscribe, subDetails *subscribe.Subscribe) ([]*node.Node, error) {
	if notice := unavailableNotice(userSub, timeutil.Now()); notice != "" {
		return createNoticeServers(siteHost, notice), nil
	}
	if subDetails == nil {
		return createNoticeServers(siteHost, noticeUnavailable), nil
	}

	nodeIds, tags, err := subDetails.NodeScope()
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "plan nodes: %v", err)
	}
	lg := logger.WithContext(ctx)
	if len(nodeIds) == 0 && len(tags) == 0 {
		lg.Infow("[Generate Subscribe] plan selects no nodes", logger.Field("subscribe_id", subDetails.Id))
		return []*node.Node{}, nil
	}
	nodes, err := s.deps.Nodes.ListEnabledNodesByScope(ctx, nodeIds, tags)
	if err != nil {
		lg.Errorw("[Generate Subscribe] List plan nodes failed", logger.Field("error", err.Error()), logger.Field("subscribe_id", subDetails.Id))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "list nodes of plan %d", subDetails.Id)
	}
	lg.Debugf("[Generate Subscribe] found %d nodes", len(nodes))
	return nodes, nil
}

// createNoticeServers returns placeholder (non-functional) nodes whose names
// carry a notice (e.g. expired / traffic exhausted) so clients display the
// reason instead of silently failing. The second one is named after the
// site: the first host of the site host setting.
func createNoticeServers(siteHost, message string) []*node.Node {
	enable := true
	host := getFirstHostLine(siteHost)

	return []*node.Node{
		{
			Name:    message,
			Tags:    "",
			Port:    18080,
			Address: "127.0.0.1",
			Server: &node.Server{
				Id:        1,
				Name:      message,
				Protocols: "[{\"type\":\"shadowsocks\",\"cipher\":\"aes-256-gcm\",\"port\":1}]",
			},
			Protocol: "shadowsocks",
			Enabled:  &enable,
		},
		{
			Name:    host,
			Tags:    "",
			Port:    18080,
			Address: "127.0.0.1",
			Server: &node.Server{
				Id:        1,
				Name:      message,
				Protocols: "[{\"type\":\"shadowsocks\",\"cipher\":\"aes-256-gcm\",\"port\":1}]",
			},
			Protocol: "shadowsocks",
			Enabled:  &enable,
		},
	}
}

func getFirstHostLine(host string) string {
	lines := strings.Split(host, "\n")
	if len(lines) > 0 {
		return lines[0]
	}
	return host
}
