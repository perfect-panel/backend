package bootstrap

import (
	"context"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/platform/entity/system"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Stored settings categories, as the system table and the admin settings
// handlers name them.
const (
	categorySite       = "site"
	categoryInvite     = "invite"
	categoryRegister   = "register"
	categorySubscribe  = "subscribe"
	categoryVerify     = "verify"
	categoryVerifyCode = "verify_code"
	categoryNode       = "server"
	categoryCurrency   = "currency"
)

// readSettings reads one settings category and decodes it into target. A
// read failure is returned; a stored value that cannot be applied is only
// logged with its key, because the silent decoder this replaced applied the
// remaining settings as well.
func readSettings(ctx context.Context, category string, read func(context.Context) ([]*system.System, error), target any) error {
	entries, err := read(ctx)
	if err != nil {
		return wrapf(err, xerr.DatabaseQueryError, "read %s settings", category)
	}
	decodeSettings(ctx, category, entries, target)
	return nil
}

func decodeSettings[T config.SystemConfigEntry](ctx context.Context, category string, entries []T, target any) {
	if err := config.DecodeSystemConfig(entries, target); err != nil {
		logger.WithContext(ctx).Errorw("[Settings] stored settings could not be applied, the affected fields keep their zero value",
			logger.Field("category", category), logger.Field("error", err.Error()))
	}
}

// Site loads the site settings: name, host, logo and the other branding.
func Site(ctx context.Context, deps *Dependencies) error {
	logger.Debug("initialize site config")
	var siteConfig config.SiteConfig
	if err := readSettings(ctx, categorySite, deps.Settings.GetSiteConfig, &siteConfig); err != nil {
		return err
	}
	deps.updateRuntime(func(current *config.Runtime) { current.Site = siteConfig })
	return nil
}

// Invite loads the referral settings.
func Invite(ctx context.Context, deps *Dependencies) error {
	logger.Debug("Invite config initialization")
	var inviteConfig config.InviteConfig
	if err := readSettings(ctx, categoryInvite, deps.Settings.GetInviteConfig, &inviteConfig); err != nil {
		return err
	}
	deps.updateRuntime(func(current *config.Runtime) { current.Invite = inviteConfig })
	return nil
}

// Register loads the registration settings, the trial plan among them.
func Register(ctx context.Context, deps *Dependencies) error {
	logger.Debug("Register config initialization")
	var registerConfig config.RegisterConfig
	if err := readSettings(ctx, categoryRegister, deps.Settings.GetRegisterConfig, &registerConfig); err != nil {
		return err
	}
	deps.updateRuntime(func(current *config.Runtime) { current.Register = registerConfig })
	return nil
}

// Subscribe loads the subscription delivery settings.
func Subscribe(ctx context.Context, deps *Dependencies) error {
	logger.Debug("Subscribe config initialization")
	var subscribeConfig config.SubscribeConfig
	if err := readSettings(ctx, categorySubscribe, deps.Settings.GetSubscribeConfig, &subscribeConfig); err != nil {
		return err
	}
	deps.updateRuntime(func(current *config.Runtime) { current.Subscribe = subscribeConfig })
	return nil
}

type verifyConfig struct {
	TurnstileSiteKey          string
	TurnstileSecret           string
	EnableLoginVerify         bool
	EnableRegisterVerify      bool
	EnableResetPasswordVerify bool
}

// Verify loads the captcha and the verification-code settings. Both are read
// before either is published, so a failed read keeps the previous pair.
func Verify(ctx context.Context, deps *Dependencies) error {
	logger.Debug("Verify config initialization")
	var verify verifyConfig
	if err := readSettings(ctx, categoryVerify, deps.Settings.GetVerifyConfig, &verify); err != nil {
		return err
	}
	verifyConfig := config.Verify{
		TurnstileSiteKey:    verify.TurnstileSiteKey,
		TurnstileSecret:     verify.TurnstileSecret,
		LoginVerify:         verify.EnableLoginVerify,
		RegisterVerify:      verify.EnableRegisterVerify,
		ResetPasswordVerify: verify.EnableResetPasswordVerify,
	}

	logger.Debug("Verify code config initialization")
	cfg, err := deps.Settings.GetVerifyCodeConfig(ctx)
	if err != nil {
		return wrapf(err, xerr.DatabaseQueryError, "read %s settings", categoryVerifyCode)
	}
	verifyCode := verifyCodeFromSettings(ctx, cfg)
	deps.updateRuntime(func(current *config.Runtime) {
		current.Verify = verifyConfig
		current.VerifyCode = verifyCode
	})
	return nil
}

// verifyCodeSettings mirrors the stored keys, which carry a VerifyCode prefix
// that the runtime config's field names do not; reflecting straight into
// config.VerifyCode matched nothing and silently ignored the admin settings.
type verifyCodeSettings struct {
	VerifyCodeExpireTime int64
	VerifyCodeLimit      int64
	VerifyCodeInterval   int64
}

func verifyCodeFromSettings[T config.SystemConfigEntry](ctx context.Context, settings []T) config.VerifyCode {
	var stored verifyCodeSettings
	decodeSettings(ctx, categoryVerifyCode, settings, &stored)
	return config.VerifyCode{
		ExpireTime: stored.VerifyCodeExpireTime,
		Limit:      stored.VerifyCodeLimit,
		Interval:   stored.VerifyCodeInterval,
	}
}
