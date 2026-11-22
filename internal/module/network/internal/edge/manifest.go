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

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/internal/module/subscription/entity/usersub"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// ErrManifestNotFound reports a token that yields no manifest: an unknown
// token, or a subscription whose owner is deleted or disabled. The handler
// answers it like a failed credential.
var ErrManifestNotFound = errors.New("edge manifest not found")

// Manifest builds the edge contract of the subscription behind token from
// the domain model. It intentionally does not reuse the client subscription
// rendering.
func (s *Service) Manifest(ctx context.Context, token string) (*dto.EdgeManifestResponse, error) {
	userSubscribe, err := s.deps.Subscriptions.SubscriptionByToken(ctx, token)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrManifestNotFound
		}
		return nil, err
	}
	if userSubscribe == nil {
		return nil, ErrManifestNotFound
	}
	account, err := s.deps.Accounts.FindAccountState(ctx, userSubscribe.UserId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrManifestNotFound
		}
		return nil, err
	}
	if account == nil || account.DeletedAt.Valid || account.Enable == nil || !*account.Enable {
		return nil, ErrManifestNotFound
	}
	plan, err := s.deps.Plans.PlanByID(ctx, userSubscribe.SubscribeId)
	if err != nil {
		return nil, err
	}

	now := timeutil.Now().UTC()
	state := subscriptionState(userSubscribe, now)
	manifest := &dto.EdgeManifestResponse{
		SchemaVersion: "1.0",
		GeneratedAt:   now.Format(time.RFC3339),
		Subscription:  subscriptionDTO(plan, userSubscribe, state, s.deps.Config().Subscribe),
		Proxies:       make([]dto.EdgeManifestProxy, 0),
		Notices:       make([]string, 0),
	}
	if state == "active" {
		proxies, notices, err := s.proxies(ctx, userSubscribe, plan)
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

// proxies lists the plan's enabled nodes the Worker supports as proxies for
// the subscription, in sort order, with a notice for each node it cannot
// offer.
func (s *Service) proxies(ctx context.Context, userSubscribe *usersub.Subscribe, plan *subscribe.Subscribe) ([]dto.EdgeManifestProxy, []string, error) {
	nodeIDs, tags, err := plan.NodeScope()
	if err != nil {
		return nil, nil, xerr.Wrapf(err, xerr.ERROR, "plan nodes: %v", err)
	}
	if len(nodeIDs) == 0 && len(tags) == 0 {
		return []dto.EdgeManifestProxy{}, nil, nil
	}
	enabled := true
	nodes, err := s.deps.Nodes.ListNodesByScope(ctx, nodeIDs, tags, &enabled, true)
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

func subscriptionDTO(plan *subscribe.Subscribe, userSubscribe *usersub.Subscribe, state string, cfg config.SubscribeConfig) dto.EdgeManifestSubscription {
	result := dto.EdgeManifestSubscription{
		Name:         plan.Name,
		State:        state,
		TrafficLimit: userSubscribe.Traffic,
		Upload:       userSubscribe.Upload,
		Download:     userSubscribe.Download,
		WebPageURL:   strings.TrimSpace(cfg.ProfileWebPageURL),
	}
	if !usersub.NoExpiry(userSubscribe.ExpireTime) {
		result.ExpiresAt = userSubscribe.ExpireTime.UTC().Format(time.RFC3339)
	}
	if cfg.ProfileUpdateInterval > 0 {
		result.UpdateIntervalHours = cfg.ProfileUpdateInterval
	}
	return result
}

// subscriptionState is the manifest state of the subscription at now. Only
// "active" lists proxies, by the rule the node user list and delivery apply
// (usersub.AvailabilityAt), so the Worker never offers a node that refuses
// the user.
func subscriptionState(item *usersub.Subscribe, now time.Time) string {
	if item == nil {
		return "disabled"
	}
	switch item.AvailabilityAt(now) {
	case usersub.Available:
		return "active"
	case usersub.Stopped:
		return "suspended"
	case usersub.Expired:
		return "expired"
	case usersub.TrafficExhausted:
		return "traffic_exhausted"
	default:
		// Refunded, or a status no rule serves.
		return "disabled"
	}
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

// proxyFromNode is the proxy of a node for the subscription credential
// userSecret, through the server protocol the node names; without one it
// reports why the node is not offered.
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

// uniqueProxyName names a proxy after its node, suffixing the node id to a
// name an earlier proxy already took.
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

// revision is a digest of what the manifest shows and of the records behind
// it, leaving out the credentials, so that equal revisions mean an unchanged
// manifest.
func revision(account *user.AccountState, userSubscribe *usersub.Subscribe, plan *subscribe.Subscribe, subscription dto.EdgeManifestSubscription, proxies []dto.EdgeManifestProxy, notices []string) string {
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
