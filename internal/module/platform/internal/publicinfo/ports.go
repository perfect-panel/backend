package publicinfo

import (
	"context"
	"net"

	"github.com/oschwald/geoip2-golang"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/platform/entity/system"
	"github.com/perfect-panel/backend/internal/module/platform/internal/readmodel"
	"github.com/redis/go-redis/v9"
)

// SettingsReader reads the stored settings the public pages show; the
// system settings repository satisfies it.
type SettingsReader interface {
	GetCurrencyConfig(ctx context.Context) ([]*system.System, error)
	GetVerifyCodeConfig(ctx context.Context) ([]*system.System, error)
	// GetTosConfig reads the terms of service and the privacy policy.
	GetTosConfig(ctx context.Context) ([]*system.System, error)
	// FindOneByKey reads a single setting, such as the web ads switch.
	FindOneByKey(ctx context.Context, key string) (*system.System, error)
}

// AuthMethodLister lists the authentication methods the identity domain has
// configured, as the platform's read model; the composition root adapts the
// identity facade to it.
type AuthMethodLister interface {
	ListAuthMethods(ctx context.Context) ([]readmodel.AuthMethod, error)
}

// AccountStats is the read port onto the identity domain: the configured
// authentication methods and the number of enabled accounts.
type AccountStats interface {
	AuthMethodLister
	CountEnabledUsers(ctx context.Context) (int64, error)
}

// NodeStats is the read port onto the network domain: the enabled nodes, the
// server addresses and the protocols the nodes offer.
type NodeStats interface {
	CountEnabledNodes(ctx context.Context) (int64, error)
	QueryServerAddresses(ctx context.Context) ([]string, error)
	QueryEnabledNodeProtocols(ctx context.Context) ([]string, error)
}

// GlobalConfigSnapshot is the public runtime configuration consumed when
// assembling the global configuration response.
type GlobalConfigSnapshot struct {
	Site      config.SiteConfig
	Subscribe config.SubscribeConfig
	Email     config.EmailConfig
	Mobile    config.MobileConfig
	Register  config.RegisterConfig
	Verify    config.Verify
	Invite    config.InviteConfig
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	// Settings reads the stored public settings.
	Settings SettingsReader
	// Accounts and Nodes read the identity and network domains' figures.
	Accounts AccountStats
	Nodes    NodeStats
	Redis    *redis.Client
	// Config snapshots the runtime-mutable public configuration per request.
	Config func() GlobalConfigSnapshot
	// GeoIP returns the local GeoIP (City) database the node countries are
	// looked up in, or nil when none is loaded.
	GeoIP func() *geoip2.Reader
	// Resolver looks up node hostnames; nil means net.DefaultResolver.
	Resolver HostResolver
	// Clients lists the client applications of the public download page.
	Clients ClientApplicationLister
}

// ClientApplication is a client application the public download page lists.
// The subscription module owns the rows; the composition root copies the
// fields the page shows into this platform-owned type.
type ClientApplication struct {
	Id          int64
	Name        string
	Description string
	Icon        string
	Scheme      string
	IsDefault   bool
	// DownloadLink is the JSON-encoded download link per platform.
	DownloadLink string
}

// ClientApplicationLister lists the client applications in their stored
// order.
type ClientApplicationLister interface {
	ListClientApplications(ctx context.Context) ([]ClientApplication, error)
}

// HostResolver resolves a hostname; *net.Resolver satisfies it.
type HostResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}
