package subscribe

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/perfect-panel/server/adapter"
	"github.com/perfect-panel/server/internal/model/entity/client"
	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/internal/report"

	"github.com/perfect-panel/server/internal/model/entity/user"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

//goland:noinspection GoNameStartsWithPackageName
type SubscribeLogic struct {
	ctx     context.Context
	svc     *svc.ServiceContext
	request RequestMeta
	logger.Logger
}

type RequestMeta struct {
	Host       string
	RequestURI string
	UserAgent  string
	ClientIP   string
}

func NewSubscribeLogic(ctx context.Context, svc *svc.ServiceContext, request RequestMeta) *SubscribeLogic {
	return &SubscribeLogic{
		ctx:     ctx,
		svc:     svc,
		request: request,
		Logger:  logger.WithContext(ctx),
	}
}

func (l *SubscribeLogic) Handler(req *dto.SubscribeRequest) (resp *dto.SubscribeResponse, err error) {
	// query client list
	clients, err := l.svc.Store.Client().List(l.ctx)
	if err != nil {
		l.Errorw("[SubscribeLogic] Query client list failed", logger.Field("error", err.Error()))
		return nil, err
	}

	userAgent := strings.ToLower(l.request.UserAgent)

	var targetApp, defaultApp *client.SubscribeApplication

	for _, item := range clients {
		u := strings.ToLower(item.UserAgent)
		if item.IsDefault {
			defaultApp = item
		}

		if strings.Contains(userAgent, u) {
			// Special handling for Stash
			if strings.Contains(userAgent, "stash") && !strings.Contains(u, "stash") {
				continue
			}
			targetApp = item
			break
		}
	}
	if targetApp == nil {
		l.Debugf("[SubscribeLogic] No matching client found", logger.Field("userAgent", userAgent))
		if defaultApp == nil {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "No matching client found for user agent: %s", userAgent)
		}
		targetApp = defaultApp
	}
	// Find user subscribe by token
	userSubscribe, err := l.getUserSubscribe(req.Token)
	if err != nil {
		l.Errorw("[SubscribeLogic] Get user subscribe failed", logger.Field("error", err.Error()), logger.Field("token", req.Token))
		return nil, err
	}

	var subscribeStatus = false
	defer func() {
		l.logSubscribeActivity(subscribeStatus, userSubscribe, req)
	}()
	// find subscribe info
	subscribeInfo, err := l.svc.Store.Subscribe().FindOne(l.ctx, userSubscribe.SubscribeId)
	if err != nil {
		l.Errorw("[SubscribeLogic] Find subscribe info failed", logger.Field("error", err.Error()), logger.Field("subscribeId", userSubscribe.SubscribeId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Find subscribe info failed: %v", err.Error())
	}

	// Find server list by user subscribe
	servers, err := l.getServers(userSubscribe)
	if err != nil {
		return nil, err
	}
	a := adapter.NewAdapter(
		targetApp.SubscribeTemplate,
		adapter.WithServers(servers),
		adapter.WithSiteName(l.svc.Config.Site.SiteName),
		adapter.WithSubscribeName(subscribeInfo.Name),
		adapter.WithOutputFormat(targetApp.OutputFormat),
		adapter.WithUserInfo(adapter.User{
			ID:           userSubscribe.Id,
			Password:     userSubscribe.UUID,
			ExpiredAt:    userSubscribe.ExpireTime,
			Download:     userSubscribe.Download,
			Upload:       userSubscribe.Upload,
			Traffic:      userSubscribe.Traffic,
			SubscribeURL: l.getSubscribeV2URL(),
		}),
		adapter.WithParams(req.Params),
	)

	logger.Debugf("[SubscribeLogic] Building client config for user %d with URI %s", userSubscribe.UserId, l.getSubscribeV2URL())

	// Get client config
	adapterClient, err := a.Client()
	if err != nil {
		l.Errorw("[SubscribeLogic] Client error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(500), "Client error: %v", err.Error())
	}
	bytes, err := adapterClient.Build()
	if err != nil {
		l.Errorw("[SubscribeLogic] Build client config failed", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(500), "Build client config failed: %v", err.Error())
	}

	var formats = []string{"json", "yaml", "conf"}

	headers := make(map[string]string)
	for _, format := range formats {
		if format == strings.ToLower(targetApp.OutputFormat) {
			headers["Content-Disposition"] = fmt.Sprintf("attachment;filename*=UTF-8''%s", url.PathEscape(l.svc.Config.Site.SiteName))
			headers["Content-Type"] = "application/octet-stream; charset=UTF-8"
			if l.svc.Config.Subscribe.ProfileUpdateInterval > 0 {
				headers["profile-update-interval"] = fmt.Sprintf("%d", l.svc.Config.Subscribe.ProfileUpdateInterval)
			}
			if profileURL := strings.TrimSpace(l.svc.Config.Subscribe.ProfileWebPageURL); profileURL != "" {
				headers["profile-web-page-url"] = profileURL
			}
		}
	}

	resp = &dto.SubscribeResponse{
		Config: bytes,
		Header: fmt.Sprintf(
			"upload=%d;download=%d;total=%d;expire=%d",
			userSubscribe.Upload, userSubscribe.Download, userSubscribe.Traffic, userSubscribe.ExpireTime.Unix(),
		),
		Headers: headers,
	}
	subscribeStatus = true
	return
}

func (l *SubscribeLogic) getSubscribeV2URL() string {

	uri := l.request.RequestURI
	// is gateway mode, add /sub prefix
	if report.IsGatewayMode() {
		uri = "/sub" + uri
	}
	// use custom domain if configured
	if l.svc.Config.Subscribe.SubscribeDomain != "" {
		domains := strings.Split(l.svc.Config.Subscribe.SubscribeDomain, "\n")
		return fmt.Sprintf("https://%s%s", domains[0], uri)
	}
	// use current request host
	return fmt.Sprintf("https://%s%s", l.request.Host, uri)
}

// getUserSubscribe 是本次修改的核心部分
func (l *SubscribeLogic) getUserSubscribe(token string) (*user.Subscribe, error) {
	userSub, err := l.svc.Store.UserSubscription().FindOneSubscribeByToken(l.ctx, token)
	if err != nil {
		l.Infow("[Generate Subscribe]find subscribe error: %v", logger.Field("error", err.Error()), logger.Field("token", token))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe error: %v", err.Error())
	}

	// =========================================================
	// 修复开始：添加空指针检查 (Fix start)
	// =========================================================
	if userSub == nil {
		l.Infow("[Generate Subscribe] token invalid or user not found", logger.Field("token", token))
		return nil, errors.New("subscribe token invalid")
	}
	// =========================================================
	// Check if user is enabled
	userInfo, err := l.svc.Store.User().FindOne(l.ctx, userSub.UserId)
	if err != nil {
		l.Infow("[Generate Subscribe] failed to get user info", logger.Field("error", err.Error()), logger.Field("userId", userSub.UserId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "failed to get user info: %v", err.Error())
	}
	if !*userInfo.Enable {
		l.Infow("[Generate Subscribe] user account is disabled", logger.Field("userId", userSub.UserId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserDisabled), "User account is disabled")
	}
	// 修复结束 (Fix end)
	// =========================================================

	//  Ignore expiration check
	//if userSub.Status > 1 {
	// l.Infow("[Generate Subscribe]subscribe is not available", logger.Field("status", int(userSub.Status)), logger.Field("token", token))
	// return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeNotAvailable), "subscribe is not available")
	//}

	return userSub, nil
}

func (l *SubscribeLogic) logSubscribeActivity(subscribeStatus bool, userSub *user.Subscribe, req *dto.SubscribeRequest) {
	if !subscribeStatus {
		return
	}

	subscribeLog := log.Subscribe{
		Token:           req.Token,
		UserAgent:       req.UA,
		ClientIP:        l.request.ClientIP,
		UserSubscribeId: userSub.Id,
	}

	content, _ := subscribeLog.Marshal()

	err := l.svc.Store.Log().Insert(l.ctx, &log.SystemLog{
		Type:     log.TypeSubscribe.Uint8(),
		ObjectID: userSub.UserId, // log user id
		Date:     timeutil.Now().Format(time.DateOnly),
		Content:  string(content),
	})
	if err != nil {
		l.Errorw("[Generate Subscribe]insert subscribe log error: %v", logger.Field("error", err.Error()))
	}
}

func (l *SubscribeLogic) getServers(userSub *user.Subscribe) ([]*node.Node, error) {
	if l.isSubscriptionExpired(userSub) {
		return l.createNoticeServers("订阅已过期 / Subscribe Expired"), nil
	}

	if l.isTrafficExhausted(userSub) {
		return l.createNoticeServers("流量已用尽 / Traffic Exhausted"), nil
	}

	subDetails, err := l.svc.Store.Subscribe().FindOne(l.ctx, userSub.SubscribeId)
	if err != nil {
		l.Errorw("[Generate Subscribe]find subscribe details error: %v", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe details error: %v", err.Error())
	}

	nodeIds := tool.StringToInt64Slice(subDetails.Nodes)
	tags := tool.RemoveStringElement(strings.Split(subDetails.NodeTags, ","), "")

	l.Debugf("[Generate Subscribe]nodes: %v, NodeTags: %v", len(nodeIds), len(tags))
	if len(nodeIds) == 0 && len(tags) == 0 {
		logger.Infow("[Generate Subscribe]no subscribe nodes")
		return []*node.Node{}, nil
	}
	enable := true
	var nodes []*node.Node
	_, nodes, err = l.svc.Store.Node().FilterNodeList(l.ctx, &node.FilterNodeParams{
		Page:    1,
		Size:    1000,
		NodeId:  nodeIds,
		Tag:     tool.RemoveDuplicateElements(tags...),
		Preload: true,
		Enabled: &enable, // Only get enabled nodes
	})

	l.Debugf("[Query Subscribe]found servers: %v", len(nodes))

	if err != nil {
		l.Errorw("[Generate Subscribe]find server details error: %v", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find server details error: %v", err.Error())
	}
	logger.Debugf("[Generate Subscribe]found servers: %v", len(nodes))
	return nodes, nil
}

func (l *SubscribeLogic) isSubscriptionExpired(userSub *user.Subscribe) bool {
	return userSub.ExpireTime.Unix() < timeutil.Now().Unix() && userSub.ExpireTime.Unix() != 0
}

// isTrafficExhausted reports whether the subscription has used up its traffic
// quota. Traffic == 0 means unlimited. Mirrors the condition used by
// FindTrafficExceededSubscribes (upload + download >= traffic AND traffic > 0).
func (l *SubscribeLogic) isTrafficExhausted(userSub *user.Subscribe) bool {
	return userSub.Traffic > 0 && userSub.Download+userSub.Upload >= userSub.Traffic
}

// createNoticeServers returns placeholder (non-functional) nodes whose names
// carry a notice (e.g. expired / traffic exhausted) so clients display the
// reason instead of silently failing.
func (l *SubscribeLogic) createNoticeServers(message string) []*node.Node {
	enable := true
	host := l.getFirstHostLine()

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

func (l *SubscribeLogic) getFirstHostLine() string {
	host := l.svc.Config.Host
	lines := strings.Split(host, "\n")
	if len(lines) > 0 {
		return lines[0]
	}
	return host
}
