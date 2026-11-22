package app

import (
	"context"
	"strings"

	"github.com/perfect-panel/backend/internal/module/identity"
	"github.com/perfect-panel/backend/internal/module/notification"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
)

// newIdentityModule wires the identity module against the shared store;
// device kicking is a closure over the application's device manager.
// The trial settings are not passed: the subscription module grants trials
// when it consumes the registration event.
func newIdentityModule(store repository.Store, srv *Application) identity.Service {
	return identity.New(identity.Deps{
		Users:     store.User(),
		UserAuths: store.UserAuth(),
		Devices:   store.UserDevice(),
		Cache:     store.UserCache(),
		Logs:      store.Log(),
		Store:     store,
		KickDevice: func(userID int64, identifier string) {
			if srv.DeviceManager != nil {
				srv.DeviceManager.KickDevice(userID, identifier)
			}
		},
		// Deleted and disabled accounts lose their cached access at once.
		SubscriptionCaches: srv.Subscription,
		ServerCaches:       identityServerCaches{srv},

		// Billing is constructed before identity, so its facade is bound
		// directly.
		Wallet: srv.Billing,
		Auths:  store.Auth(),
		Redis:  srv.Redis,
		EmailDomains: func() (string, bool) {
			current := srv.Runtime.Config().Email
			return current.DomainSuffixList, current.EnableDomainSuffix
		},
		TelegramBotName: func() string { return srv.Runtime.Config().Telegram.BotName },
		NotifyTelegramUnbind: func(ctx context.Context, userID, chatID int64) error {
			return srv.Notification.NotifyTelegramUnbind(ctx, userID, chatID)
		},
		NotifyPasswordChanged: func(ctx context.Context, userID int64, bindings []string) error {
			return notifyPasswordChanged(ctx, srv.Notification, userID, bindings)
		},
		AuthConfig: func() identity.AuthSnapshot {
			c := srv.Runtime.Config()
			return identity.AuthSnapshot{
				JWTAccessSecret: c.JwtAuth.AccessSecret,
				JWTAccessExpire: c.JwtAuth.AccessExpire,

				EmailEnabled:            c.Email.Enable,
				EmailVerifyEnabled:      c.Email.EnableVerify,
				EmailDomainSuffixList:   c.Email.DomainSuffixList,
				EmailEnableDomainSuffix: c.Email.EnableDomainSuffix,
				MobileEnabled:           c.Mobile.Enable,
				DeviceEnabled:           c.Device.Enable,
				DeviceOnlyReal:          c.Device.OnlyRealDevice,

				InviteForced:      c.Invite.ForcedInvite,
				OnlyFirstPurchase: c.Invite.OnlyFirstPurchase,

				StopRegister:            c.Register.StopRegister,
				RegisterVerify:          c.Verify.RegisterVerify,
				LoginVerify:             c.Verify.LoginVerify,
				ResetPasswordVerify:     c.Verify.ResetPasswordVerify,
				TurnstileSecret:         c.Verify.TurnstileSecret,
				EnableIpRegisterLimit:   c.Register.EnableIpRegisterLimit,
				IpRegisterLimit:         c.Register.IpRegisterLimit,
				IpRegisterLimitDuration: c.Register.IpRegisterLimitDuration,

				SiteHost: c.Site.Host,
			}
		},
		VerifyQueue: srv.Queue,
		SenderConfig: func() identity.SenderSnapshot {
			c := srv.Runtime.Config()
			return identity.SenderSnapshot{
				EmailPlatform:        c.Email.Platform,
				EmailPlatformConfig:  c.Email.PlatformConfig,
				MobilePlatform:       c.Mobile.Platform,
				MobilePlatformConfig: c.Mobile.PlatformConfig,
				SiteName:             c.Site.SiteName,
			}
		},
		Reinitialize: srv.Runtime.Reinitialize,
		VerifyCodeConfig: func() identity.VerifyCodeSnapshot {
			c := srv.Runtime.Config()
			return identity.VerifyCodeSnapshot{
				DomainSuffixList:       c.Email.DomainSuffixList,
				EnableDomainSuffix:     c.Email.EnableDomainSuffix,
				VerifyCodeInterval:     c.VerifyCode.Interval,
				VerifyCodeLimit:        c.VerifyCode.Limit,
				VerifyCodeExpire:       c.VerifyCode.ExpireTime,
				MobileWhitelistEnabled: c.Mobile.EnableWhitelist,
				MobileWhitelist:        c.Mobile.Whitelist,
				SiteLogo:               c.Site.SiteLogo,
				SiteName:               c.Site.SiteName,
			}
		},
	})
}

// passwordChangedNotice is the Telegram message an account gets when its
// password changed; the bindings are data and escaped by the renderer.
//
//nolint:gosec // G101: a notification template, not a credential
const passwordChangedNotice = `🔐 *您的账户密码已更改*

如果这不是您本人的操作，请立即重置密码并检查账户绑定。
仍绑定的第三方登录方式: {{.Bindings}}`

// notifyPasswordChanged tells the account, through its Telegram binding if it
// has one, that its password changed and which third-party sign-in methods
// stay bound. An account without a binding reports nothing to deliver.
func notifyPasswordChanged(ctx context.Context, notifier notification.Service, userID int64, bindings []string) error {
	if notifier == nil {
		return nil
	}
	list := "无"
	if len(bindings) > 0 {
		list = strings.Join(bindings, ", ")
	}
	text, err := notification.RenderTelegramMarkdown(passwordChangedNotice, map[string]string{"Bindings": list})
	if err != nil {
		return err
	}
	return notifier.NotifyTelegramUser(ctx, userID, text)
}

// startupChecks are the identity module's start-up warnings beyond
// IdentityStartup: the facade provides them, and they only log.
type startupChecks interface {
	WarnUnpinnedOAuthRedirects(ctx context.Context) error
	ReportLegacyAdministratorPasswords(ctx context.Context) error
}

// normalizeIdentityData runs the identity module's idempotent startup data
// fix-ups once the schema is current; the module logs what they changed.
// They repair stored identifiers, so a failure is logged and the server still
// starts. The start-up warnings run here too: the sign-in methods whose
// redirects need a site host, and the administrators on a legacy password
// hash.
func normalizeIdentityData(ctx context.Context, accounts IdentityStartup) {
	if err := accounts.NormalizePhoneNumbers(ctx); err != nil {
		logger.Errorw("[Identity] normalize stored phone numbers failed", logger.Field("error", err.Error()))
	}
	checks, ok := accounts.(startupChecks)
	if !ok {
		return
	}
	if err := checks.WarnUnpinnedOAuthRedirects(ctx); err != nil {
		logger.Errorw("[Identity] check the OAuth redirect pins failed", logger.Field("error", err.Error()))
	}
	if err := checks.ReportLegacyAdministratorPasswords(ctx); err != nil {
		logger.Errorw("[Identity] check the administrators' password hashes failed", logger.Field("error", err.Error()))
	}
}
