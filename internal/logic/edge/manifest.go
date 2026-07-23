package edge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/internal/model/entity/subscribe"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"gorm.io/gorm"
)

var ErrManifestNotFound = errors.New("edge manifest not found")

// ManifestLogic builds an edge contract from the domain model. It intentionally
// does not invoke adapter.NewAdapter or the legacy subscription logic.
type ManifestLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
	now    func() time.Time
}

func NewManifestLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ManifestLogic {
	return &ManifestLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
		now:    time.Now,
	}
}

func (l *ManifestLogic) Manifest(token string) (*dto.EdgeManifestResponse, error) {
	userSubscribe, err := l.svcCtx.Store.UserSubscription().FindOneSubscribeByToken(l.ctx, token)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrManifestNotFound
		}
		return nil, err
	}
	if userSubscribe == nil {
		return nil, ErrManifestNotFound
	}
	account, err := l.svcCtx.Store.User().FindOne(l.ctx, userSubscribe.UserId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrManifestNotFound
		}
		return nil, err
	}
	if account == nil || account.Enable == nil || !*account.Enable {
		return nil, ErrManifestNotFound
	}
	plan, err := l.svcCtx.Store.Subscribe().FindOne(l.ctx, userSubscribe.SubscribeId)
	if err != nil {
		return nil, err
	}

	now := l.now().UTC()
	state := subscriptionState(userSubscribe, now)
	manifest := &dto.EdgeManifestResponse{
		SchemaVersion: "1.0",
		GeneratedAt:   now.Format(time.RFC3339),
		Subscription:  subscriptionDTO(plan, userSubscribe, state, l.svcCtx.Config.Subscribe),
		Proxies:       make([]dto.EdgeManifestProxy, 0),
		Notices:       make([]string, 0),
	}
	if state == "active" {
		proxies, notices, err := l.proxies(userSubscribe, plan)
		if err != nil {
			return nil, err
		}
		manifest.Proxies = proxies
		manifest.Notices = notices
	} else {
		manifest.Notices = append(manifest.Notices, stateNotice(state))
	}
	manifest.Revision = revision(account, userSubscribe, plan, manifest.Subscription, manifest.Proxies, manifest.Notices)
	return manifest, nil
}

func (l *ManifestLogic) proxies(userSubscribe *user.Subscribe, plan *subscribe.Subscribe) ([]dto.EdgeManifestProxy, []string, error) {
	nodeIDs := tool.StringToInt64Slice(plan.Nodes)
	tags := cleanTags(strings.Split(plan.NodeTags, ","))
	if len(nodeIDs) == 0 && len(tags) == 0 {
		return []dto.EdgeManifestProxy{}, nil, nil
	}
	enabled := true
	_, nodes, err := l.svcCtx.Store.Node().FilterNodeList(l.ctx, &node.FilterNodeParams{
		Page:    1,
		Size:    1000,
		NodeId:  nodeIDs,
		Tag:     tags,
		Enabled: &enabled,
		Preload: true,
	})
	if err != nil {
		return nil, nil, err
	}

	proxies := make([]dto.EdgeManifestProxy, 0, len(nodes))
	notices := make([]string, 0)
	usedNames := make(map[string]int)
	for _, item := range nodes {
		proxy, supported, reason := proxyFromNode(item, userSubscribe.UUID)
		if !supported {
			notices = append(notices, "Node "+item.Name+" is unavailable in Edge Manifest v1: "+reason)
			continue
		}
		proxy.Name = uniqueProxyName(proxy.Name, item.Id, usedNames)
		proxies = append(proxies, proxy)
	}
	sort.Slice(proxies, func(i, j int) bool {
		if proxies[i].Sort == proxies[j].Sort {
			return proxies[i].Name < proxies[j].Name
		}
		return proxies[i].Sort < proxies[j].Sort
	})
	return proxies, notices, nil
}

func subscriptionDTO(plan *subscribe.Subscribe, userSubscribe *user.Subscribe, state string, cfg config.SubscribeConfig) dto.EdgeManifestSubscription {
	result := dto.EdgeManifestSubscription{
		Name:         plan.Name,
		State:        state,
		TrafficLimit: userSubscribe.Traffic,
		Upload:       userSubscribe.Upload,
		Download:     userSubscribe.Download,
		WebPageURL:   strings.TrimSpace(cfg.ProfileWebPageURL),
	}
	if userSubscribe.ExpireTime.Unix() > 0 {
		result.ExpiresAt = userSubscribe.ExpireTime.UTC().Format(time.RFC3339)
	}
	if cfg.ProfileUpdateInterval > 0 {
		result.UpdateIntervalHours = cfg.ProfileUpdateInterval
	}
	return result
}

func subscriptionState(item *user.Subscribe, now time.Time) string {
	if item == nil {
		return "disabled"
	}
	switch item.Status {
	case 0, 4:
		return "disabled"
	case 2, 3:
		return "expired"
	case 5:
		return "suspended"
	case 1:
		// Continue with expiration and traffic checks below.
	default:
		return "disabled"
	}
	if item.ExpireTime.Unix() > 0 && !item.ExpireTime.After(now) {
		return "expired"
	}
	if item.Traffic > 0 && item.Upload+item.Download >= item.Traffic {
		return "traffic_exhausted"
	}
	return "active"
}

func stateNotice(state string) string {
	switch state {
	case "expired":
		return "Subscription expired"
	case "traffic_exhausted":
		return "Subscription traffic exhausted"
	case "suspended":
		return "Subscription suspended"
	default:
		return "Subscription is unavailable"
	}
}

func proxyFromNode(item *node.Node, userSecret string) (dto.EdgeManifestProxy, bool, string) {
	if item == nil || item.Server == nil {
		return dto.EdgeManifestProxy{}, false, "node server is missing"
	}
	protocols, err := item.Server.UnmarshalProtocols()
	if err != nil {
		return dto.EdgeManifestProxy{}, false, "node protocol definition is invalid"
	}
	for _, protocol := range protocols {
		if strings.EqualFold(strings.TrimSpace(protocol.Type), strings.TrimSpace(item.Protocol)) {
			return proxyFromProtocol(item, protocol, userSecret)
		}
	}
	return dto.EdgeManifestProxy{}, false, "matching node protocol is missing"
}

func proxyFromProtocol(item *node.Node, protocol node.Protocol, userSecret string) (dto.EdgeManifestProxy, bool, string) {
	protocolType := strings.ToLower(strings.TrimSpace(protocol.Type))
	security := strings.ToLower(strings.TrimSpace(protocol.Security))
	transport := strings.ToLower(strings.TrimSpace(protocol.Transport))
	if !protocol.Enable {
		return dto.EdgeManifestProxy{}, false, "node protocol is disabled"
	}
	if userSecret == "" {
		return dto.EdgeManifestProxy{}, false, "subscription credential is missing"
	}
	if security == "reality" {
		return dto.EdgeManifestProxy{}, false, "REALITY is not supported by the Worker V1 contract"
	}
	if protocolType == "shadowsocks" && (strings.Contains(strings.ToLower(protocol.Cipher), "2022") || protocol.Plugin != "") {
		return dto.EdgeManifestProxy{}, false, "Shadowsocks 2022 and plugins are not supported by the Worker V1 contract"
	}
	if protocolType == "shadowsocks" && security == "tls" {
		return dto.EdgeManifestProxy{}, false, "Shadowsocks TLS is only supported through plugins, which are not in the Worker V1 contract"
	}
	if transport != "" && transport != "tcp" && transport != "none" && transport != "ws" && transport != "grpc" && transport != "httpupgrade" {
		return dto.EdgeManifestProxy{}, false, "transport " + transport + " is not supported by the Worker V1 contract"
	}
	if protocolType != "shadowsocks" && protocolType != "vmess" && protocolType != "vless" && protocolType != "trojan" {
		return dto.EdgeManifestProxy{}, false, "protocol " + protocolType + " is not supported by the Worker V1 contract"
	}

	proxy := dto.EdgeManifestProxy{
		Name:     item.Name,
		Protocol: protocolType,
		Server:   item.Address,
		Port:     item.Port,
		UDP:      true,
		Tags:     cleanTags(strings.Split(item.Tags, ",")),
		Sort:     item.Sort,
	}
	switch protocolType {
	case "shadowsocks":
		if protocol.Cipher == "" {
			return dto.EdgeManifestProxy{}, false, "Shadowsocks cipher is missing"
		}
		proxy.Cipher = protocol.Cipher
		proxy.Password = userSecret
	case "vmess", "vless":
		proxy.UUID = userSecret
		proxy.Flow = protocol.Flow
	case "trojan":
		proxy.Password = userSecret
	}
	if security == "tls" {
		proxy.TLS = &dto.EdgeManifestTLS{
			Enabled:     true,
			ServerName:  protocol.SNI,
			Insecure:    protocol.AllowInsecure,
			ALPN:        protocol.ALPN,
			Fingerprint: protocol.Fingerprint,
		}
	}
	if transport == "ws" || transport == "grpc" || transport == "httpupgrade" {
		proxy.Transport = &dto.EdgeManifestTransport{
			Type:        transport,
			Path:        protocol.Path,
			Host:        protocol.Host,
			ServiceName: protocol.ServiceName,
		}
	}
	return proxy, true, ""
}

func cleanTags(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func uniqueProxyName(name string, nodeID int64, used map[string]int) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Node"
	}
	used[name]++
	if used[name] == 1 {
		return name
	}
	return name + " #" + strconv.FormatInt(nodeID, 10)
}

func revision(account *user.User, userSubscribe *user.Subscribe, plan *subscribe.Subscribe, subscription dto.EdgeManifestSubscription, proxies []dto.EdgeManifestProxy, notices []string) string {
	type proxySource struct {
		Name      string                     `json:"name"`
		Protocol  string                     `json:"protocol"`
		Server    string                     `json:"server"`
		Port      uint16                     `json:"port"`
		Cipher    string                     `json:"cipher"`
		Flow      string                     `json:"flow"`
		TLS       *dto.EdgeManifestTLS       `json:"tls,omitempty"`
		Transport *dto.EdgeManifestTransport `json:"transport,omitempty"`
		Tags      []string                   `json:"tags,omitempty"`
		Sort      int                        `json:"sort"`
	}
	type source struct {
		UserID             int64                        `json:"user_id"`
		UserUpdatedAt      int64                        `json:"user_updated_at"`
		UserSubscribeID    int64                        `json:"user_subscribe_id"`
		SubscribeUpdatedAt int64                        `json:"subscribe_updated_at"`
		Status             uint8                        `json:"status"`
		ExpiresAt          int64                        `json:"expires_at"`
		Traffic            int64                        `json:"traffic"`
		Upload             int64                        `json:"upload"`
		Download           int64                        `json:"download"`
		PlanID             int64                        `json:"plan_id"`
		PlanUpdatedAt      int64                        `json:"plan_updated_at"`
		Subscription       dto.EdgeManifestSubscription `json:"subscription"`
		Proxies            []proxySource                `json:"proxies"`
		Notices            []string                     `json:"notices"`
	}
	proxySources := make([]proxySource, 0, len(proxies))
	for _, proxy := range proxies {
		proxySources = append(proxySources, proxySource{
			Name:      proxy.Name,
			Protocol:  proxy.Protocol,
			Server:    proxy.Server,
			Port:      proxy.Port,
			Cipher:    proxy.Cipher,
			Flow:      proxy.Flow,
			TLS:       proxy.TLS,
			Transport: proxy.Transport,
			Tags:      proxy.Tags,
			Sort:      proxy.Sort,
		})
	}
	data, _ := json.Marshal(source{
		UserID:             account.Id,
		UserUpdatedAt:      account.UpdatedAt.UnixMilli(),
		UserSubscribeID:    userSubscribe.Id,
		SubscribeUpdatedAt: userSubscribe.UpdatedAt.UnixMilli(),
		Status:             userSubscribe.Status,
		ExpiresAt:          userSubscribe.ExpireTime.UnixMilli(),
		Traffic:            userSubscribe.Traffic,
		Upload:             userSubscribe.Upload,
		Download:           userSubscribe.Download,
		PlanID:             plan.Id,
		PlanUpdatedAt:      plan.UpdatedAt.UnixMilli(),
		Subscription:       subscription,
		Proxies:            proxySources,
		Notices:            notices,
	})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
