// Package platform is the facade of the platform module, the shared kernel:
// audit and message logs with their retention settings, the system settings,
// the admin console dashboard and tools, and the unauthenticated site-level
// reads of the public portal. See docs/design/adr-001-modular-monolith.md.
package platform

import (
	"context"

	"github.com/oschwald/geoip2-golang"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/internal/auditlog"
	"github.com/perfect-panel/backend/internal/module/platform/internal/dashboard"
	"github.com/perfect-panel/backend/internal/module/platform/internal/publicinfo"
	"github.com/perfect-panel/backend/internal/module/platform/internal/readmodel"
	"github.com/perfect-panel/backend/internal/module/platform/internal/repo"
	"github.com/perfect-panel/backend/internal/module/platform/internal/systemsetting"
	"github.com/perfect-panel/backend/internal/module/platform/internal/tool"
	"github.com/perfect-panel/backend/internal/repository/kernel"
	"github.com/redis/go-redis/v9"
)

// Service is the only surface other code may depend on; the implementation
// lives under internal/ where the compiler seals it off.
type Service interface {
	FilterBalanceLog(ctx context.Context, req *dto.FilterBalanceLogRequest) (*dto.FilterBalanceLogResponse, error)
	FilterCommissionLog(ctx context.Context, req *dto.FilterCommissionLogRequest) (*dto.FilterCommissionLogResponse, error)
	FilterEmailLog(ctx context.Context, req *dto.FilterLogParams) (*dto.FilterEmailLogResponse, error)
	FilterGiftLog(ctx context.Context, req *dto.FilterGiftLogRequest) (*dto.FilterGiftLogResponse, error)
	FilterLoginLog(ctx context.Context, req *dto.FilterLoginLogRequest) (*dto.FilterLoginLogResponse, error)
	FilterMobileLog(ctx context.Context, req *dto.FilterLogParams) (*dto.FilterMobileLogResponse, error)
	FilterOrderLog(ctx context.Context, req *dto.FilterOrderLogRequest) (*dto.FilterOrderLogResponse, error)
	FilterRegisterLog(ctx context.Context, req *dto.FilterRegisterLogRequest) (*dto.FilterRegisterLogResponse, error)
	FilterResetSubscribeLog(ctx context.Context, req *dto.FilterResetSubscribeLogRequest) (*dto.FilterResetSubscribeLogResponse, error)
	FilterServerTrafficLog(ctx context.Context, req *dto.FilterServerTrafficLogRequest) (*dto.FilterServerTrafficLogResponse, error)
	FilterSubscribeLog(ctx context.Context, req *dto.FilterSubscribeLogRequest) (*dto.FilterSubscribeLogResponse, error)
	FilterTrafficLogDetails(ctx context.Context, req *dto.FilterTrafficLogDetailsRequest) (*dto.FilterTrafficLogDetailsResponse, error)
	FilterUserSubscribeTrafficLog(ctx context.Context, req *dto.FilterSubscribeTrafficRequest) (*dto.FilterSubscribeTrafficResponse, error)
	GetLogSetting(ctx context.Context) (*dto.LogSetting, error)
	// UpdateLogSetting persists the retention settings and propagates them to
	// the running configuration.
	UpdateLogSetting(ctx context.Context, req *dto.LogSetting) error
	// CleanupLogs applies the log retention settings to the system log and
	// the network's raw traffic log.
	CleanupLogs(ctx context.Context) error
	GetMessageLogList(ctx context.Context, req *dto.GetMessageLogListRequest) (*dto.GetMessageLogListResponse, error)
	// FilterAdminActionLog pages the administrators' audit trail.
	FilterAdminActionLog(ctx context.Context, req *dto.FilterAdminActionLogRequest) (*dto.FilterAdminActionLogResponse, error)
	// FilterUnmatchedPaymentLog pages the gateway payments that could not
	// settle an order.
	FilterUnmatchedPaymentLog(ctx context.Context, req *dto.FilterUnmatchedPaymentLogRequest) (*dto.FilterUnmatchedPaymentLogResponse, error)

	// System configuration management; updates persist the settings and
	// re-initialize the owning subsystem through injected callbacks.
	GetCurrencyConfig(ctx context.Context) (*dto.CurrencyConfig, error)
	GetInviteConfig(ctx context.Context) (*dto.InviteConfig, error)
	GetNodeConfig(ctx context.Context) (*dto.NodeConfig, error)
	GetNodeMultiplier(ctx context.Context) (*dto.GetNodeMultiplierResponse, error)
	GetPrivacyPolicyConfig(ctx context.Context) (*dto.PrivacyPolicyConfig, error)
	GetRegisterConfig(ctx context.Context) (*dto.RegisterConfig, error)
	GetSiteConfig(ctx context.Context) (*dto.SiteConfig, error)
	GetSubscribeConfig(ctx context.Context) (*dto.SubscribeConfig, error)
	GetTosConfig(ctx context.Context) (*dto.TosConfig, error)
	GetVerifyCodeConfig(ctx context.Context) (*dto.VerifyCodeConfig, error)
	GetVerifyConfig(ctx context.Context) (*dto.VerifyConfig, error)
	PreViewNodeMultiplier(ctx context.Context) (*dto.PreViewNodeMultiplierResponse, error)
	SetNodeMultiplier(ctx context.Context, req *dto.SetNodeMultiplierRequest) error
	SettingTelegramBot(ctx context.Context) error
	UpdateCurrencyConfig(ctx context.Context, req *dto.CurrencyConfig) error
	UpdateInviteConfig(ctx context.Context, req *dto.InviteConfig) error
	UpdateNodeConfig(ctx context.Context, req *dto.NodeConfig) error
	UpdatePrivacyPolicyConfig(ctx context.Context, req *dto.PrivacyPolicyConfig) error
	UpdateRegisterConfig(ctx context.Context, req *dto.RegisterConfig) error
	UpdateSiteConfig(ctx context.Context, req *dto.SiteConfig) error
	UpdateSubscribeConfig(ctx context.Context, req *dto.SubscribeConfig) error
	UpdateTosConfig(ctx context.Context, req *dto.TosConfig) error
	UpdateVerifyCodeConfig(ctx context.Context, req *dto.VerifyCodeConfig) error
	UpdateVerifyConfig(ctx context.Context, req *dto.VerifyConfig) error

	// Admin console reporting aggregates (cross-domain reads through ports).
	QueryRevenueStatistics(ctx context.Context) (*dto.RevenueStatisticsResponse, error)
	QueryServerTotalData(ctx context.Context) (*dto.ServerTotalDataResponse, error)
	QueryTicketWaitReply(ctx context.Context) (*dto.TicketWaitRelpyResponse, error)
	QueryUserStatistics(ctx context.Context) (*dto.UserStatisticsResponse, error)

	// Admin utility tools: system log tail, version, IP geolocation and
	// process restart.
	GetSystemLog(ctx context.Context) (*dto.LogResponse, error)
	GetVersion(ctx context.Context) (*dto.VersionResponse, error)
	QueryIPLocation(ctx context.Context, req *dto.QueryIPLocationRequest) (*dto.QueryIPLocationResponse, error)
	RestartSystem(ctx context.Context) error

	// Unauthenticated site-level reads for the public portal.
	GetGlobalConfig(ctx context.Context) (*dto.GetGlobalConfigResponse, error)
	GetTos(ctx context.Context) (*dto.GetTosResponse, error)
	GetPrivacyPolicy(ctx context.Context) (*dto.PrivacyPolicyConfig, error)
	GetStat(ctx context.Context) (*dto.GetStatResponse, error)
	GetClient(ctx context.Context) (*dto.GetSubscribeClientResponse, error)
	Heartbeat(ctx context.Context) (*dto.HeartbeatResponse, error)
}

// GlobalConfigSnapshot re-exports the public-info subdomain's per-request
// view of the public runtime configuration for the composition root.
type GlobalConfigSnapshot = publicinfo.GlobalConfigSnapshot

// TrafficLogPruner re-exports the audit-log subdomain's port onto the network
// domain's raw traffic log; the composition root adapts the network facade.
type TrafficLogPruner = auditlog.TrafficLogPruner

// ClientApplication and ClientApplicationLister re-export the public-info
// subdomain's port onto the subscription module's client applications; the
// composition root adapts the subscription facade to it.
type (
	ClientApplication       = publicinfo.ClientApplication
	ClientApplicationLister = publicinfo.ClientApplicationLister
)

// The platform's read models of the other modules' data. The shared kernel
// depends on no module, so the composition root converts the owning modules'
// facade results into these.
type (
	OrdersTotal             = readmodel.OrdersTotal
	OrdersTotalWithDate     = readmodel.OrdersTotalWithDate
	UserStatisticsWithDate  = readmodel.UserStatisticsWithDate
	Server                  = readmodel.Server
	TotalTraffic            = readmodel.TotalTraffic
	ServerTrafficRanking    = readmodel.ServerTrafficRanking
	UserTrafficRanking      = readmodel.UserTrafficRanking
	TrafficLogDetailsFilter = readmodel.TrafficLogDetailsFilter
	TrafficLog              = readmodel.TrafficLog
	AuthMethod              = readmodel.AuthMethod
)

// The read ports onto the other domains, typed with the read models above:
// OrderReader onto billing, UserReader onto identity, TicketReader onto
// support, NodeReader and TrafficReader onto network. They serve the
// dashboard, the audit views and the public statistics.
type (
	OrderReader  = dashboard.OrderStatsReader
	TicketReader = dashboard.TicketStatsReader

	UserReader interface {
		dashboard.UserStatsReader
		publicinfo.AccountStats
	}
	NodeReader interface {
		dashboard.NodeStatsReader
		publicinfo.NodeStats
	}
	TrafficReader interface {
		dashboard.TrafficStatsReader
		auditlog.TrafficReader
	}
)

// Deps declares everything the module needs; the composition root
// (internal/app) provides them.
type Deps struct {
	Logs    kernel.LogRepo
	System  kernel.SystemRepo
	Traffic TrafficReader
	Store   auditlog.PlatformTransactor
	// OnLogSettingChanged propagates a committed retention change to the
	// running configuration.
	OnLogSettingChanged func(autoClear bool, clearDays int64)
	// LogRetention reads the current (mutable) retention configuration.
	LogRetention func() (autoClear bool, clearDays int64)
	// TrafficRetention prunes the network's raw traffic log with the system
	// log when the retention settings ask for it.
	TrafficRetention TrafficLogPruner

	// System-setting dependencies (see internal/systemsetting).
	Reinitialize  func(subsystem string) error
	Restart       func() error
	SubscribePath func() string
	Multiplier    systemsetting.MultiplierFunc

	// Dashboard and public-info read ports onto the other domains, and the
	// dashboard's cache.
	Orders  OrderReader
	Users   UserReader
	Tickets TicketReader
	Nodes   NodeReader
	Cache   dashboard.Cache

	// Public-info dependencies: the full read surface, the shared Redis
	// cache and the runtime-mutable public configuration snapshot.
	PublicStore  Store
	Redis        *redis.Client
	PublicConfig func() GlobalConfigSnapshot
	// Clients lists the client applications of the public download page
	// (the subscription facade).
	Clients ClientApplicationLister

	// Tool dependencies: the logger output path and the GeoIP reader, which
	// the public statistics also locate the nodes with.
	LogPath string
	GeoIP   func() *geoip2.Reader
}

// NewRepoBuilder exports the module-owned repository implementations for
// store assembly (ADR-001 step-6 preparation).
func NewRepoBuilder() kernel.PlatformBuilder {
	return func(c kernel.ModuleConn) kernel.PlatformRepos {
		conn := c.Conn()
		return kernel.PlatformRepos{
			System: repo.NewSystemRepo(conn),
			Logs:   repo.NewLogRepo(c.DB),
			Tasks:  repo.NewTaskRepo(c.DB),
			Inbox:  repo.NewInboxRepo(c.DB),
			Outbox: repo.NewOutboxRepo(c.DB),
		}
	}
}

// New builds the platform module from its dependencies.
func New(deps Deps) Service {
	return &service{
		settings: systemsetting.NewService(systemsetting.Deps{
			System:        deps.System,
			Store:         systemsetting.NewSettingsTransactor(deps.Store),
			Reinitialize:  deps.Reinitialize,
			Restart:       deps.Restart,
			SubscribePath: deps.SubscribePath,
			Multiplier:    deps.Multiplier,
		}),
		dashboard: dashboard.NewService(dashboard.Deps{
			Orders:  deps.Orders,
			Users:   deps.Users,
			Tickets: deps.Tickets,
			Nodes:   deps.Nodes,
			Traffic: deps.Traffic,
			Logs:    deps.Logs,
			Cache:   deps.Cache,
		}),
		tools: tool.NewService(tool.Deps{
			LogPath: deps.LogPath,
			GeoIP:   deps.GeoIP,
			Restart: deps.Restart,
		}),
		public: publicinfo.NewService(publicinfo.Deps{
			Settings: publicSettings(deps.PublicStore),
			Accounts: deps.Users,
			Nodes:    deps.Nodes,
			Redis:    deps.Redis,
			Config:   deps.PublicConfig,
			GeoIP:    deps.GeoIP,
			// The subscription module's client applications.
			Clients: deps.Clients,
		}),
		logs: auditlog.NewService(auditlog.Deps{
			Logs:                deps.Logs,
			System:              deps.System,
			Traffic:             deps.Traffic,
			Store:               deps.Store,
			OnLogSettingChanged: deps.OnLogSettingChanged,
			LogRetention:        deps.LogRetention,
			TrafficRetention:    deps.TrafficRetention,
		}),
	}
}

type service struct {
	logs      *auditlog.Service
	settings  *systemsetting.Service
	dashboard *dashboard.Service
	public    *publicinfo.Service
	tools     *tool.Service
}

func (s *service) FilterBalanceLog(ctx context.Context, req *dto.FilterBalanceLogRequest) (*dto.FilterBalanceLogResponse, error) {
	return s.logs.FilterBalanceLog(ctx, req)
}

func (s *service) FilterCommissionLog(ctx context.Context, req *dto.FilterCommissionLogRequest) (*dto.FilterCommissionLogResponse, error) {
	return s.logs.FilterCommissionLog(ctx, req)
}

func (s *service) FilterEmailLog(ctx context.Context, req *dto.FilterLogParams) (*dto.FilterEmailLogResponse, error) {
	return s.logs.FilterEmailLog(ctx, req)
}

func (s *service) FilterGiftLog(ctx context.Context, req *dto.FilterGiftLogRequest) (*dto.FilterGiftLogResponse, error) {
	return s.logs.FilterGiftLog(ctx, req)
}

func (s *service) FilterLoginLog(ctx context.Context, req *dto.FilterLoginLogRequest) (*dto.FilterLoginLogResponse, error) {
	return s.logs.FilterLoginLog(ctx, req)
}

func (s *service) FilterMobileLog(ctx context.Context, req *dto.FilterLogParams) (*dto.FilterMobileLogResponse, error) {
	return s.logs.FilterMobileLog(ctx, req)
}

func (s *service) FilterOrderLog(ctx context.Context, req *dto.FilterOrderLogRequest) (*dto.FilterOrderLogResponse, error) {
	return s.logs.FilterOrderLog(ctx, req)
}

func (s *service) FilterRegisterLog(ctx context.Context, req *dto.FilterRegisterLogRequest) (*dto.FilterRegisterLogResponse, error) {
	return s.logs.FilterRegisterLog(ctx, req)
}

func (s *service) FilterResetSubscribeLog(ctx context.Context, req *dto.FilterResetSubscribeLogRequest) (*dto.FilterResetSubscribeLogResponse, error) {
	return s.logs.FilterResetSubscribeLog(ctx, req)
}

func (s *service) FilterServerTrafficLog(ctx context.Context, req *dto.FilterServerTrafficLogRequest) (*dto.FilterServerTrafficLogResponse, error) {
	return s.logs.FilterServerTrafficLog(ctx, req)
}

func (s *service) FilterSubscribeLog(ctx context.Context, req *dto.FilterSubscribeLogRequest) (*dto.FilterSubscribeLogResponse, error) {
	return s.logs.FilterSubscribeLog(ctx, req)
}

func (s *service) FilterTrafficLogDetails(ctx context.Context, req *dto.FilterTrafficLogDetailsRequest) (*dto.FilterTrafficLogDetailsResponse, error) {
	return s.logs.FilterTrafficLogDetails(ctx, req)
}

func (s *service) FilterUserSubscribeTrafficLog(ctx context.Context, req *dto.FilterSubscribeTrafficRequest) (*dto.FilterSubscribeTrafficResponse, error) {
	return s.logs.FilterUserSubscribeTrafficLog(ctx, req)
}

func (s *service) GetLogSetting(ctx context.Context) (*dto.LogSetting, error) {
	return s.logs.GetLogSetting(ctx)
}

func (s *service) UpdateLogSetting(ctx context.Context, req *dto.LogSetting) error {
	return s.logs.UpdateLogSetting(ctx, req)
}

func (s *service) GetMessageLogList(ctx context.Context, req *dto.GetMessageLogListRequest) (*dto.GetMessageLogListResponse, error) {
	return s.logs.GetMessageLogList(ctx, req)
}

func (s *service) FilterAdminActionLog(ctx context.Context, req *dto.FilterAdminActionLogRequest) (*dto.FilterAdminActionLogResponse, error) {
	return s.logs.FilterAdminActionLog(ctx, req)
}

func (s *service) FilterUnmatchedPaymentLog(ctx context.Context, req *dto.FilterUnmatchedPaymentLogRequest) (*dto.FilterUnmatchedPaymentLogResponse, error) {
	return s.logs.FilterUnmatchedPaymentLog(ctx, req)
}

func (s *service) GetCurrencyConfig(ctx context.Context) (*dto.CurrencyConfig, error) {
	return s.settings.GetCurrencyConfig(ctx)
}

func (s *service) GetInviteConfig(ctx context.Context) (*dto.InviteConfig, error) {
	return s.settings.GetInviteConfig(ctx)
}

func (s *service) GetNodeConfig(ctx context.Context) (*dto.NodeConfig, error) {
	return s.settings.GetNodeConfig(ctx)
}

func (s *service) GetNodeMultiplier(ctx context.Context) (*dto.GetNodeMultiplierResponse, error) {
	return s.settings.GetNodeMultiplier(ctx)
}

func (s *service) GetPrivacyPolicyConfig(ctx context.Context) (*dto.PrivacyPolicyConfig, error) {
	return s.settings.GetPrivacyPolicyConfig(ctx)
}

func (s *service) GetRegisterConfig(ctx context.Context) (*dto.RegisterConfig, error) {
	return s.settings.GetRegisterConfig(ctx)
}

func (s *service) GetSiteConfig(ctx context.Context) (*dto.SiteConfig, error) {
	return s.settings.GetSiteConfig(ctx)
}

func (s *service) GetSubscribeConfig(ctx context.Context) (*dto.SubscribeConfig, error) {
	return s.settings.GetSubscribeConfig(ctx)
}

func (s *service) GetTosConfig(ctx context.Context) (*dto.TosConfig, error) {
	return s.settings.GetTosConfig(ctx)
}

func (s *service) GetVerifyCodeConfig(ctx context.Context) (*dto.VerifyCodeConfig, error) {
	return s.settings.GetVerifyCodeConfig(ctx)
}

func (s *service) GetVerifyConfig(ctx context.Context) (*dto.VerifyConfig, error) {
	return s.settings.GetVerifyConfig(ctx)
}

func (s *service) PreViewNodeMultiplier(ctx context.Context) (*dto.PreViewNodeMultiplierResponse, error) {
	return s.settings.PreViewNodeMultiplier(ctx)
}

func (s *service) SetNodeMultiplier(ctx context.Context, req *dto.SetNodeMultiplierRequest) error {
	return s.settings.SetNodeMultiplier(ctx, req)
}

func (s *service) SettingTelegramBot(ctx context.Context) error {
	return s.settings.SettingTelegramBot(ctx)
}

func (s *service) UpdateCurrencyConfig(ctx context.Context, req *dto.CurrencyConfig) error {
	return s.settings.UpdateCurrencyConfig(ctx, req)
}

func (s *service) UpdateInviteConfig(ctx context.Context, req *dto.InviteConfig) error {
	return s.settings.UpdateInviteConfig(ctx, req)
}

func (s *service) UpdateNodeConfig(ctx context.Context, req *dto.NodeConfig) error {
	return s.settings.UpdateNodeConfig(ctx, req)
}

func (s *service) UpdatePrivacyPolicyConfig(ctx context.Context, req *dto.PrivacyPolicyConfig) error {
	return s.settings.UpdatePrivacyPolicyConfig(ctx, req)
}

func (s *service) UpdateRegisterConfig(ctx context.Context, req *dto.RegisterConfig) error {
	return s.settings.UpdateRegisterConfig(ctx, req)
}

func (s *service) UpdateSiteConfig(ctx context.Context, req *dto.SiteConfig) error {
	return s.settings.UpdateSiteConfig(ctx, req)
}

func (s *service) UpdateSubscribeConfig(ctx context.Context, req *dto.SubscribeConfig) error {
	return s.settings.UpdateSubscribeConfig(ctx, req)
}

func (s *service) UpdateTosConfig(ctx context.Context, req *dto.TosConfig) error {
	return s.settings.UpdateTosConfig(ctx, req)
}

func (s *service) UpdateVerifyCodeConfig(ctx context.Context, req *dto.VerifyCodeConfig) error {
	return s.settings.UpdateVerifyCodeConfig(ctx, req)
}

func (s *service) UpdateVerifyConfig(ctx context.Context, req *dto.VerifyConfig) error {
	return s.settings.UpdateVerifyConfig(ctx, req)
}

func (s *service) QueryRevenueStatistics(ctx context.Context) (*dto.RevenueStatisticsResponse, error) {
	return s.dashboard.QueryRevenueStatistics(ctx)
}

func (s *service) QueryServerTotalData(ctx context.Context) (*dto.ServerTotalDataResponse, error) {
	return s.dashboard.QueryServerTotalData(ctx)
}

func (s *service) QueryTicketWaitReply(ctx context.Context) (*dto.TicketWaitRelpyResponse, error) {
	return s.dashboard.QueryTicketWaitReply(ctx)
}

func (s *service) QueryUserStatistics(ctx context.Context) (*dto.UserStatisticsResponse, error) {
	return s.dashboard.QueryUserStatistics(ctx)
}

func (s *service) GetGlobalConfig(ctx context.Context) (*dto.GetGlobalConfigResponse, error) {
	return s.public.GetGlobalConfig(ctx)
}

func (s *service) GetTos(ctx context.Context) (*dto.GetTosResponse, error) {
	return s.public.GetTos(ctx)
}

func (s *service) GetPrivacyPolicy(ctx context.Context) (*dto.PrivacyPolicyConfig, error) {
	return s.public.GetPrivacyPolicy(ctx)
}

func (s *service) GetStat(ctx context.Context) (*dto.GetStatResponse, error) {
	return s.public.GetStat(ctx)
}

func (s *service) GetClient(ctx context.Context) (*dto.GetSubscribeClientResponse, error) {
	return s.public.GetClient(ctx)
}

func (s *service) Heartbeat(ctx context.Context) (*dto.HeartbeatResponse, error) {
	return s.public.Heartbeat(ctx)
}

func (s *service) GetSystemLog(ctx context.Context) (*dto.LogResponse, error) {
	return s.tools.GetSystemLog(ctx)
}

func (s *service) GetVersion(ctx context.Context) (*dto.VersionResponse, error) {
	return s.tools.GetVersion(ctx)
}

func (s *service) QueryIPLocation(ctx context.Context, req *dto.QueryIPLocationRequest) (*dto.QueryIPLocationResponse, error) {
	return s.tools.QueryIPLocation(ctx, req)
}

func (s *service) RestartSystem(ctx context.Context) error {
	return s.tools.RestartSystem(ctx)
}

// Store is the persistence the public-info reads need: the system settings.
// It excludes unrelated repositories and application-wide transactions.
type Store interface {
	System() kernel.SystemRepo
}

// publicSettings reads the public settings from the store's system settings.
// A facade built without a store, as the route tests build it, has none.
func publicSettings(store Store) publicinfo.SettingsReader {
	if store == nil {
		return nil
	}
	return store.System()
}

func (s *service) CleanupLogs(ctx context.Context) error {
	return s.logs.CleanupLogs(ctx)
}
