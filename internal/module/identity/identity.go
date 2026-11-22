// Package identity is the facade of the identity module: accounts, their
// sign-in identities and devices, the authentication and OAuth flows, the
// verification codes and the admin management of accounts and
// authentication methods. See docs/design/adr-001-modular-monolith.md.
package identity

import (
	"context"
	"sync"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/identity/internal/account"
	"github.com/perfect-panel/backend/internal/module/identity/internal/adminuser"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authmethodadmin"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authn"
	"github.com/perfect-panel/backend/internal/module/identity/internal/authn/oauth"
	"github.com/perfect-panel/backend/internal/module/identity/internal/devicestate"
	"github.com/perfect-panel/backend/internal/module/identity/internal/oauthflow"
	"github.com/perfect-panel/backend/internal/module/identity/internal/profile"
	"github.com/perfect-panel/backend/internal/module/identity/internal/repo"
	"github.com/perfect-panel/backend/internal/module/identity/internal/startup"
	"github.com/perfect-panel/backend/internal/module/identity/internal/verifycode"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/cache"
	"github.com/redis/go-redis/v9"
)

// Service is the only surface other code may depend on; the implementation
// lives under internal/ where the compiler seals it off.
type Service interface {
	// Accounts serves the other modules' account reads and writes.
	Accounts

	CreateUser(ctx context.Context, req *dto.CreateUserRequest) error
	DeleteUser(ctx context.Context, req *dto.GetDetailRequest) error
	BatchDeleteUser(ctx context.Context, req *dto.BatchDeleteUserRequest) error
	GetUserDetail(ctx context.Context, req *dto.GetDetailRequest) (*dto.User, error)
	GetUserList(ctx context.Context, req *dto.GetUserListRequest) (*dto.GetUserListResponse, error)
	CurrentUser(ctx context.Context) (*dto.User, error)
	CreateUserAuthMethod(ctx context.Context, req *dto.CreateUserAuthMethodRequest) error
	DeleteUserAuthMethod(ctx context.Context, req *dto.DeleteUserAuthMethodRequest) error
	GetUserAuthMethod(ctx context.Context, req *dto.GetUserAuthMethodRequest) (*dto.GetUserAuthMethodResponse, error)
	UpdateUserAuthMethod(ctx context.Context, req *dto.UpdateUserAuthMethodRequest) error
	DeleteUserDevice(ctx context.Context, req *dto.DeleteUserDeviceRequest) error
	UpdateUserDevice(ctx context.Context, req *dto.UserDevice) error
	KickOfflineByUserDevice(ctx context.Context, req *dto.KickOfflineRequest) error
	GetUserLoginLogs(ctx context.Context, req *dto.GetUserLoginLogsRequest) (*dto.GetUserLoginLogsResponse, error)
	UpdateUserBasicInfo(ctx context.Context, req *dto.UpdateUserBasicInfoRequest) error
	UpdateUserNotifySetting(ctx context.Context, req *dto.UpdateUserNotifySettingRequest) error

	// The profile flows resolve the current user from the request context:
	// account info, credentials, third-party bindings, devices and
	// notification preferences.
	QueryUserInfo(ctx context.Context) (*dto.User, error)
	// UpdateUserPassword changes the password and reports the third-party
	// sign-in methods still bound to the account.
	UpdateUserPassword(ctx context.Context, req *dto.UpdateUserPasswordRequest) (*dto.UpdateUserPasswordResponse, error)
	// Logout ends the calling session.
	Logout(ctx context.Context) error
	UpdateUserNotify(ctx context.Context, req *dto.UpdateUserNotifyRequest) error
	UpdateUserRules(ctx context.Context, req *dto.UpdateUserRulesRequest) error
	GetLoginLog(ctx context.Context, req *dto.GetLoginLogRequest) (*dto.GetLoginLogResponse, error)
	GetDeviceList(ctx context.Context) (*dto.GetDeviceListResponse, error)
	UnbindDevice(ctx context.Context, req *dto.UnbindDeviceRequest) error
	GetOAuthMethods(ctx context.Context) (*dto.GetOAuthMethodsResponse, error)
	BindOAuth(ctx context.Context, req *dto.BindOAuthRequest) (*dto.BindOAuthResponse, error)
	BindOAuthCallback(ctx context.Context, req *dto.BindOAuthCallbackRequest) error
	UnbindOAuth(ctx context.Context, req *dto.UnbindOAuthRequest) error
	BindTelegram(ctx context.Context) (*dto.BindTelegramResponse, error)
	UnbindTelegram(ctx context.Context) error
	UpdateBindEmail(ctx context.Context, req *dto.UpdateBindEmailRequest) error
	VerifyEmail(ctx context.Context, req *dto.VerifyEmailRequest) error
	UpdateBindMobile(ctx context.Context, req *dto.UpdateBindMobileRequest) error

	// The authentication flows: existence checks, credential/telephone/device
	// login and registration, password resets and the OAuth handshakes. They
	// read the client address and user agent from the request metadata and
	// apply the configured Turnstile checks themselves.
	CheckUser(ctx context.Context, req *dto.CheckUserRequest) (*dto.CheckUserResponse, error)
	CheckUserTelephone(ctx context.Context, req *dto.TelephoneCheckUserRequest) (*dto.TelephoneCheckUserResponse, error)
	UserLogin(ctx context.Context, req *dto.UserLoginRequest) (*dto.LoginResponse, error)
	UserRegister(ctx context.Context, req *dto.UserRegisterRequest) (*dto.LoginResponse, error)
	TelephoneLogin(ctx context.Context, req *dto.TelephoneLoginRequest) (*dto.LoginResponse, error)
	TelephoneUserRegister(ctx context.Context, req *dto.TelephoneRegisterRequest) (*dto.LoginResponse, error)
	ResetPassword(ctx context.Context, req *dto.ResetPasswordRequest) (*dto.LoginResponse, error)
	TelephoneResetPassword(ctx context.Context, req *dto.TelephoneResetPasswordRequest) (*dto.LoginResponse, error)
	DeviceLogin(ctx context.Context, req *dto.DeviceLoginRequest) (*dto.LoginResponse, error)
	OAuthLogin(ctx context.Context, req *dto.OAuthLoginRequest) (*dto.OAuthLoginResponse, error)
	OAuthLoginGetToken(ctx context.Context, req *dto.OAuthLoginGetTokenRequest) (*dto.LoginResponse, error)
	AppleLoginCallback(ctx context.Context, req *dto.AppleLoginCallbackRequest) (*AppleLoginRedirect, error)

	// The admin-side authentication-method management: configuration,
	// sender platforms and test sends.
	GetAuthMethodList(ctx context.Context) (*dto.GetAuthMethodListResponse, error)
	GetAuthMethodConfig(ctx context.Context, req *dto.GetAuthMethodConfigRequest) (*dto.AuthMethodConfig, error)
	UpdateAuthMethodConfig(ctx context.Context, req *dto.UpdateAuthMethodConfigRequest) (*dto.AuthMethodConfig, error)
	GetEmailPlatform(ctx context.Context) (*dto.AuthPlatformResponse, error)
	GetSmsPlatform(ctx context.Context) (*dto.AuthPlatformResponse, error)
	TestEmailSend(ctx context.Context, req *dto.TestEmailSendRequest) error
	TestSmsSend(ctx context.Context, req *dto.TestSmsSendRequest) error

	// The verification-code flows issue and pre-check the email/SMS codes
	// gating registration and account mutations.
	SendEmailCode(ctx context.Context, req *dto.SendCodeRequest) (*dto.SendCodeResponse, error)
	SendSmsCode(ctx context.Context, req *dto.SendSmsCodeRequest) (*dto.SendCodeResponse, error)
	CheckVerificationCode(ctx context.Context, req *dto.CheckVerificationCodeRequest) (*dto.CheckVerificationCodeResponse, error)

	// The presence the device WebSocket reports, which the composition
	// root's socket callbacks record. Both skip a device that no longer
	// exists.
	//
	// MarkDeviceOnline shows the connected device identifier online; a
	// disabled device stays offline.
	MarkDeviceOnline(ctx context.Context, identifier string) error
	// MarkDeviceOffline shows the device offline again and records, for the
	// account userID, the online time of its connection opened at
	// connectedAt; the time is recorded even when the flag cannot be cleared.
	MarkDeviceOffline(ctx context.Context, userID int64, identifier string, connectedAt time.Time) error

	// The startup work of the runtime bootstrap and the server start.
	//
	// CreateInitialAdministrator creates the first administrator, signing in
	// with the verified email address and password, when the database holds
	// no account yet, and reports whether it created one.
	CreateInitialAdministrator(ctx context.Context, email, password string) (bool, error)
	// FindAdministratorsWithPassword returns the administrators, with their
	// auth methods, who sign in with password.
	FindAdministratorsWithPassword(ctx context.Context, password string) ([]*user.User, error)
	// ValidateEmailIdentities fails with EmailIdentityAmbiguous when email
	// sign-in cannot tell two email bindings apart.
	ValidateEmailIdentities(ctx context.Context) error
	// NormalizePhoneNumbers rewrites the phone numbers stored in a legacy
	// form to E.164 and logs what it changed; it is idempotent.
	NormalizePhoneNumbers(ctx context.Context) error
	// WarnUnpinnedOAuthRedirects logs an error when Apple or Telegram
	// sign-in is enabled while no site host pins their redirects.
	WarnUnpinnedOAuthRedirects(ctx context.Context) error
	// ReportLegacyAdministratorPasswords logs an error naming the ids of
	// the administrators whose password hash is still a legacy format.
	ReportLegacyAdministratorPasswords(ctx context.Context) error
}

// UserRegisteredTopic is the integration event every self-service
// registration emits, keyed by the new account's id; subscribers reference
// this constant rather than spelling the topic.
const UserRegisteredTopic = account.RegisteredTopic

// AuthSnapshot re-exports the authentication subdomain's per-request view of
// the runtime-mutable settings; the composition root supplies the snapshot
// closure.
type AuthSnapshot = authn.Snapshot

// AppleLoginRedirect re-exports the Apple form-post callback's redirect
// result for the transport handler.
type AppleLoginRedirect = oauth.AppleLoginRedirect

// Deps declares everything the module needs; the composition root
// (internal/app) provides them.
type Deps struct {
	Users     repository.UserRepo
	UserAuths repository.UserAuthRepo
	Devices   repository.UserDeviceRepo
	Cache     repository.UserCacheRepo
	Logs      repository.LogRepo
	Store     Store
	// KickDevice force-disconnects a bound device.
	KickDevice func(userID int64, identifier string)
	// SubscriptionCaches and ServerCaches drop the subscription tokens and
	// node user lists that keep serving an account after its deletion or
	// disabling (the subscription and network facades).
	SubscriptionCaches adminuser.SubscriptionCaches
	ServerCaches       adminuser.ServerCaches

	// Wallet is the port onto the billing module's wallets: the admin and
	// self-service account views read them, and the admin's money edits run
	// in billing's own transaction after the identity one.
	Wallet Wallets

	// Profile-specific dependencies.
	Auths repository.AuthRepo
	Redis *redis.Client
	// EmailDomains snapshots the runtime-mutable email domain-suffix policy.
	EmailDomains func() (domainList string, restrict bool)
	// TelegramBotName snapshots the runtime-mutable Telegram bot name.
	TelegramBotName func() string
	// NotifyTelegramUnbind sends the best-effort unbind notice.
	NotifyTelegramUnbind func(ctx context.Context, userID, chatID int64) error
	// NotifyPasswordChanged tells the account, best effort, that its
	// password was changed or reset and which third-party sign-in methods
	// (by type) are still bound to it; optional.
	NotifyPasswordChanged func(ctx context.Context, userID int64, bindings []string) error
	// AuthConfig snapshots the runtime-mutable settings consumed by the
	// authentication flows per request.
	AuthConfig func() AuthSnapshot
	// VerifyQueue publishes verification-code delivery tasks; the asynq
	// client satisfies it structurally.
	VerifyQueue VerificationTaskQueue
	// VerifyCodeConfig snapshots the runtime-mutable settings consumed by
	// the verification-code flows per request.
	VerifyCodeConfig func() VerifyCodeSnapshot
	// SenderConfig snapshots the runtime-mutable sender platform settings
	// per request, and Reinitialize re-runs a sender subsystem's
	// initialization after its configuration changed.
	SenderConfig func() SenderSnapshot
	Reinitialize func(subsystem string) error
}

// Wallets re-exports the admin account subdomain's billing port, which also
// covers the self-service account view's wallet read; the billing facade
// provides it.
type Wallets = adminuser.Wallets

// SenderSnapshot re-exports the auth-method subdomain's sender settings view
// for the composition root.
type SenderSnapshot = authmethodadmin.Snapshot

// VerificationTaskQueue and VerifyCodeSnapshot re-export the
// verification-code subdomain's ports for the composition root.
type (
	VerificationTaskQueue = verifycode.VerificationTaskQueue
	VerifyCodeSnapshot    = verifycode.Snapshot
)

// NewRepoBuilder exports the module-owned repository implementations for
// store assembly (ADR-001 step-6 preparation). The builder runs once per
// connection and once per transaction; the retrier that redoes failed cache
// invalidations of the account rows outlives them all, so it is created
// once, on the first run.
func NewRepoBuilder() repository.IdentityBuilder {
	var (
		once    sync.Once
		retrier *cache.InvalidationRetrier
	)
	return func(c repository.ModuleConn, bridges repository.IdentityBridges) repository.IdentityRepos {
		once.Do(func() { retrier = cache.NewInvalidationRetrier(c.Redis) })
		conn := c.Conn()
		u := repo.NewUserRepo(conn, bridges, repo.WithInvalidationRetrier(retrier))
		return repository.IdentityRepos{
			Users:     u,
			UserAuths: u,
			Devices:   u,
			UserCache: u,
			Auths:     repo.NewAuthRepo(conn),
		}
	}
}

// New builds the module over the dependencies the composition root
// provides.
func New(deps Deps) Service {
	// Sign-in and account binding share the OAuth round trip.
	oauthFlow := oauthflow.New(oauthflow.Deps{
		Auths:    deps.Auths,
		Redis:    deps.Redis,
		SiteHost: func() string { return deps.AuthConfig().SiteHost },
	})
	authSvc := authn.NewService(authn.Deps{
		Store:                 deps.Store,
		Redis:                 deps.Redis,
		Config:                deps.AuthConfig,
		OAuth:                 oauthFlow,
		NotifyPasswordChanged: deps.NotifyPasswordChanged,
	})
	return &service{
		accounts: newAccounts(deps),
		authn:    authSvc,
		adminUsers: adminuser.NewService(adminuser.Deps{
			Wallet:     deps.Wallet,
			Users:      deps.Users,
			UserAuths:  deps.UserAuths,
			Devices:    deps.Devices,
			Cache:      deps.Cache,
			Logs:       deps.Logs,
			Store:      deps.Store,
			KickDevice: deps.KickDevice,
			Redis:      deps.Redis,
			// The access-cache cascade of deleted and disabled accounts.
			SubscriptionCaches: deps.SubscriptionCaches,
			ServerCaches:       deps.ServerCaches,
		}),
		methods: authmethodadmin.NewService(authmethodadmin.Deps{
			Auths:        deps.Auths,
			Config:       deps.SenderConfig,
			Reinitialize: deps.Reinitialize,
		}),
		verify: verifycode.NewService(verifycode.Deps{
			Store:  deps.Store,
			Redis:  deps.Redis,
			Queue:  deps.VerifyQueue,
			Policy: authSvc.Policy(),
			Config: deps.VerifyCodeConfig,
		}),
		profile: profile.NewService(profile.Deps{
			Wallet:                deps.Wallet,
			Users:                 deps.Users,
			UserAuth:              deps.UserAuths,
			Auth:                  deps.Auths,
			Devices:               deps.Devices,
			UserCache:             deps.Cache,
			Logs:                  deps.Logs,
			Redis:                 deps.Redis,
			Store:                 deps.Store,
			Policy:                authSvc.Policy(),
			OAuth:                 oauthFlow,
			EmailDomains:          deps.EmailDomains,
			TelegramBotName:       deps.TelegramBotName,
			NotifyUnbind:          deps.NotifyTelegramUnbind,
			NotifyPasswordChanged: deps.NotifyPasswordChanged,
			KickDevice:            deps.KickDevice,
		}),
		startup: startup.NewService(startup.Deps{
			Users:     deps.Users,
			UserAuths: deps.UserAuths,
			Auths:     deps.Auths,
			SiteHost:  siteHost(deps.AuthConfig),
			Store:     deps.Store,
		}),
	}
}

// siteHost snapshots the configured site host from the authentication
// settings; it reads as empty when the settings are not wired.
func siteHost(config func() AuthSnapshot) func() string {
	return func() string {
		if config == nil {
			return ""
		}
		return config().SiteHost
	}
}

type service struct {
	accounts
	adminUsers *adminuser.Service
	profile    *profile.Service
	authn      *authn.Service
	verify     *verifycode.Service
	methods    *authmethodadmin.Service
	startup    *startup.Service
}

func (s *service) CreateUser(ctx context.Context, req *dto.CreateUserRequest) error {
	return s.adminUsers.CreateUser(ctx, req)
}

func (s *service) DeleteUser(ctx context.Context, req *dto.GetDetailRequest) error {
	return s.adminUsers.DeleteUser(ctx, req)
}

func (s *service) BatchDeleteUser(ctx context.Context, req *dto.BatchDeleteUserRequest) error {
	return s.adminUsers.BatchDeleteUser(ctx, req)
}

func (s *service) GetUserDetail(ctx context.Context, req *dto.GetDetailRequest) (*dto.User, error) {
	return s.adminUsers.GetUserDetail(ctx, req)
}

func (s *service) GetUserList(ctx context.Context, req *dto.GetUserListRequest) (*dto.GetUserListResponse, error) {
	return s.adminUsers.GetUserList(ctx, req)
}

func (s *service) CurrentUser(ctx context.Context) (*dto.User, error) {
	return s.adminUsers.CurrentUser(ctx)
}

func (s *service) CreateUserAuthMethod(ctx context.Context, req *dto.CreateUserAuthMethodRequest) error {
	return s.adminUsers.CreateUserAuthMethod(ctx, req)
}

func (s *service) DeleteUserAuthMethod(ctx context.Context, req *dto.DeleteUserAuthMethodRequest) error {
	return s.adminUsers.DeleteUserAuthMethod(ctx, req)
}

func (s *service) GetUserAuthMethod(ctx context.Context, req *dto.GetUserAuthMethodRequest) (*dto.GetUserAuthMethodResponse, error) {
	return s.adminUsers.GetUserAuthMethod(ctx, req)
}

func (s *service) UpdateUserAuthMethod(ctx context.Context, req *dto.UpdateUserAuthMethodRequest) error {
	return s.adminUsers.UpdateUserAuthMethod(ctx, req)
}

func (s *service) DeleteUserDevice(ctx context.Context, req *dto.DeleteUserDeviceRequest) error {
	return s.adminUsers.DeleteUserDevice(ctx, req)
}

func (s *service) UpdateUserDevice(ctx context.Context, req *dto.UserDevice) error {
	return s.adminUsers.UpdateUserDevice(ctx, req)
}

func (s *service) KickOfflineByUserDevice(ctx context.Context, req *dto.KickOfflineRequest) error {
	return s.adminUsers.KickOfflineByUserDevice(ctx, req)
}

func (s *service) GetUserLoginLogs(ctx context.Context, req *dto.GetUserLoginLogsRequest) (*dto.GetUserLoginLogsResponse, error) {
	return s.adminUsers.GetUserLoginLogs(ctx, req)
}

func (s *service) UpdateUserBasicInfo(ctx context.Context, req *dto.UpdateUserBasicInfoRequest) error {
	return s.adminUsers.UpdateUserBasicInfo(ctx, req)
}

func (s *service) UpdateUserNotifySetting(ctx context.Context, req *dto.UpdateUserNotifySettingRequest) error {
	return s.adminUsers.UpdateUserNotifySetting(ctx, req)
}

func (s *service) QueryUserInfo(ctx context.Context) (*dto.User, error) {
	return s.profile.QueryUserInfo(ctx)
}

func (s *service) UpdateUserPassword(ctx context.Context, req *dto.UpdateUserPasswordRequest) (*dto.UpdateUserPasswordResponse, error) {
	return s.profile.UpdateUserPassword(ctx, req)
}

func (s *service) Logout(ctx context.Context) error {
	return s.profile.Logout(ctx)
}

func (s *service) UpdateUserNotify(ctx context.Context, req *dto.UpdateUserNotifyRequest) error {
	return s.profile.UpdateUserNotify(ctx, req)
}

func (s *service) UpdateUserRules(ctx context.Context, req *dto.UpdateUserRulesRequest) error {
	return s.profile.UpdateUserRules(ctx, req)
}

func (s *service) GetLoginLog(ctx context.Context, req *dto.GetLoginLogRequest) (*dto.GetLoginLogResponse, error) {
	return s.profile.GetLoginLog(ctx, req)
}

func (s *service) GetDeviceList(ctx context.Context) (*dto.GetDeviceListResponse, error) {
	return s.profile.GetDeviceList(ctx)
}

func (s *service) UnbindDevice(ctx context.Context, req *dto.UnbindDeviceRequest) error {
	return s.profile.UnbindDevice(ctx, req)
}

func (s *service) GetOAuthMethods(ctx context.Context) (*dto.GetOAuthMethodsResponse, error) {
	return s.profile.GetOAuthMethods(ctx)
}

func (s *service) BindOAuth(ctx context.Context, req *dto.BindOAuthRequest) (*dto.BindOAuthResponse, error) {
	return s.profile.BindOAuth(ctx, req)
}

func (s *service) BindOAuthCallback(ctx context.Context, req *dto.BindOAuthCallbackRequest) error {
	return s.profile.BindOAuthCallback(ctx, req)
}

func (s *service) UnbindOAuth(ctx context.Context, req *dto.UnbindOAuthRequest) error {
	return s.profile.UnbindOAuth(ctx, req)
}

func (s *service) BindTelegram(ctx context.Context) (*dto.BindTelegramResponse, error) {
	return s.profile.BindTelegram(ctx)
}

func (s *service) UnbindTelegram(ctx context.Context) error {
	return s.profile.UnbindTelegram(ctx)
}

func (s *service) UpdateBindEmail(ctx context.Context, req *dto.UpdateBindEmailRequest) error {
	return s.profile.UpdateBindEmail(ctx, req)
}

func (s *service) VerifyEmail(ctx context.Context, req *dto.VerifyEmailRequest) error {
	return s.profile.VerifyEmail(ctx, req)
}

func (s *service) UpdateBindMobile(ctx context.Context, req *dto.UpdateBindMobileRequest) error {
	return s.profile.UpdateBindMobile(ctx, req)
}

func (s *service) CheckUser(ctx context.Context, req *dto.CheckUserRequest) (*dto.CheckUserResponse, error) {
	return s.authn.CheckUser(ctx, req)
}

func (s *service) CheckUserTelephone(ctx context.Context, req *dto.TelephoneCheckUserRequest) (*dto.TelephoneCheckUserResponse, error) {
	return s.authn.CheckUserTelephone(ctx, req)
}

func (s *service) UserLogin(ctx context.Context, req *dto.UserLoginRequest) (*dto.LoginResponse, error) {
	return s.authn.UserLogin(ctx, req)
}

func (s *service) UserRegister(ctx context.Context, req *dto.UserRegisterRequest) (*dto.LoginResponse, error) {
	return s.authn.UserRegister(ctx, req)
}

func (s *service) TelephoneLogin(ctx context.Context, req *dto.TelephoneLoginRequest) (*dto.LoginResponse, error) {
	return s.authn.TelephoneLogin(ctx, req)
}

func (s *service) TelephoneUserRegister(ctx context.Context, req *dto.TelephoneRegisterRequest) (*dto.LoginResponse, error) {
	return s.authn.TelephoneUserRegister(ctx, req)
}

func (s *service) ResetPassword(ctx context.Context, req *dto.ResetPasswordRequest) (*dto.LoginResponse, error) {
	return s.authn.ResetPassword(ctx, req)
}

func (s *service) TelephoneResetPassword(ctx context.Context, req *dto.TelephoneResetPasswordRequest) (*dto.LoginResponse, error) {
	return s.authn.TelephoneResetPassword(ctx, req)
}

func (s *service) DeviceLogin(ctx context.Context, req *dto.DeviceLoginRequest) (*dto.LoginResponse, error) {
	return s.authn.DeviceLogin(ctx, req)
}

func (s *service) OAuthLogin(ctx context.Context, req *dto.OAuthLoginRequest) (*dto.OAuthLoginResponse, error) {
	return s.authn.OAuthLogin(ctx, req)
}

func (s *service) OAuthLoginGetToken(ctx context.Context, req *dto.OAuthLoginGetTokenRequest) (*dto.LoginResponse, error) {
	return s.authn.OAuthLoginGetToken(ctx, req)
}

func (s *service) AppleLoginCallback(ctx context.Context, req *dto.AppleLoginCallbackRequest) (*AppleLoginRedirect, error) {
	return s.authn.AppleLoginCallback(ctx, req)
}

func (s *service) SendEmailCode(ctx context.Context, req *dto.SendCodeRequest) (*dto.SendCodeResponse, error) {
	return s.verify.SendEmailCode(ctx, req)
}

func (s *service) SendSmsCode(ctx context.Context, req *dto.SendSmsCodeRequest) (*dto.SendCodeResponse, error) {
	return s.verify.SendSmsCode(ctx, req)
}

func (s *service) CheckVerificationCode(ctx context.Context, req *dto.CheckVerificationCodeRequest) (*dto.CheckVerificationCodeResponse, error) {
	return s.verify.CheckVerificationCode(ctx, req)
}

func (s *service) GetAuthMethodList(ctx context.Context) (*dto.GetAuthMethodListResponse, error) {
	return s.methods.GetAuthMethodList(ctx)
}

func (s *service) GetAuthMethodConfig(ctx context.Context, req *dto.GetAuthMethodConfigRequest) (*dto.AuthMethodConfig, error) {
	return s.methods.GetAuthMethodConfig(ctx, req)
}

func (s *service) UpdateAuthMethodConfig(ctx context.Context, req *dto.UpdateAuthMethodConfigRequest) (*dto.AuthMethodConfig, error) {
	return s.methods.UpdateAuthMethodConfig(ctx, req)
}

func (s *service) GetEmailPlatform(ctx context.Context) (*dto.AuthPlatformResponse, error) {
	return s.methods.GetEmailPlatform(ctx)
}

func (s *service) GetSmsPlatform(ctx context.Context) (*dto.AuthPlatformResponse, error) {
	return s.methods.GetSmsPlatform(ctx)
}

func (s *service) TestEmailSend(ctx context.Context, req *dto.TestEmailSendRequest) error {
	return s.methods.TestEmailSend(ctx, req)
}

func (s *service) TestSmsSend(ctx context.Context, req *dto.TestSmsSendRequest) error {
	return s.methods.TestSmsSend(ctx, req)
}

func (s *service) MarkDeviceOnline(ctx context.Context, identifier string) error {
	return devicestate.MarkOnline(ctx, s.devices, identifier)
}

func (s *service) MarkDeviceOffline(ctx context.Context, userID int64, identifier string, connectedAt time.Time) error {
	return devicestate.MarkOffline(ctx, s.devices, userID, identifier, connectedAt)
}

func (s *service) CreateInitialAdministrator(ctx context.Context, email, password string) (bool, error) {
	return s.startup.CreateInitialAdministrator(ctx, email, password)
}

func (s *service) FindAdministratorsWithPassword(ctx context.Context, password string) ([]*user.User, error) {
	return s.startup.FindAdministratorsWithPassword(ctx, password)
}

func (s *service) ValidateEmailIdentities(ctx context.Context) error {
	return s.startup.ValidateEmailIdentities(ctx)
}

func (s *service) NormalizePhoneNumbers(ctx context.Context) error {
	return s.startup.NormalizePhoneNumbers(ctx)
}

func (s *service) WarnUnpinnedOAuthRedirects(ctx context.Context) error {
	return s.startup.WarnUnpinnedOAuthRedirects(ctx)
}

func (s *service) ReportLegacyAdministratorPasswords(ctx context.Context) error {
	return s.startup.ReportLegacyAdministratorPasswords(ctx)
}

// Store is the persistence capability required by this package. It excludes
// unrelated repositories and application-wide transactions.
type Store interface {
	adminuser.Store
	authn.Store
	profile.Store
	startup.Store
}
