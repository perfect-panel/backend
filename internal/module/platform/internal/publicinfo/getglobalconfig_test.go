package publicinfo

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/platform/entity/system"
	"github.com/perfect-panel/backend/internal/module/platform/internal/readmodel"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
)

// globalConfigSettings has no stored currency or verification code settings
// and the web ads switched off; currencyErr fails the currency read.
type globalConfigSettings struct {
	currencyErr   error
	currencyCalls int
}

var _ SettingsReader = (*globalConfigSettings)(nil)

func (s *globalConfigSettings) GetCurrencyConfig(context.Context) ([]*system.System, error) {
	s.currencyCalls++
	return nil, s.currencyErr
}

func (s *globalConfigSettings) GetVerifyCodeConfig(context.Context) ([]*system.System, error) {
	return nil, nil
}

func (s *globalConfigSettings) GetTosConfig(context.Context) ([]*system.System, error) {
	return nil, nil
}

func (s *globalConfigSettings) FindOneByKey(context.Context, string) (*system.System, error) {
	return &system.System{Value: "false"}, nil
}

// publicConfigAccounts has no authentication method configured.
type publicConfigAccounts struct{}

var _ AccountStats = publicConfigAccounts{}

func (publicConfigAccounts) ListAuthMethods(context.Context) ([]readmodel.AuthMethod, error) {
	return nil, nil
}

func (publicConfigAccounts) CountEnabledUsers(context.Context) (int64, error) { return 0, nil }

func globalConfigService(settings SettingsReader, running GlobalConfigSnapshot) *Service {
	return NewService(Deps{
		Settings: settings,
		Accounts: publicConfigAccounts{},
		Config:   func() GlobalConfigSnapshot { return running },
	})
}

func TestGetGlobalConfigUsesInjectedSettings(t *testing.T) {
	logtest.Discard(t)
	failure := errors.New("currency config unavailable")
	settings := &globalConfigSettings{currencyErr: failure}

	_, err := globalConfigService(settings, GlobalConfigSnapshot{}).GetGlobalConfig(context.Background())
	if !errors.Is(err, failure) {
		t.Fatalf("GetGlobalConfig error = %v, want the currency config error", err)
	}
	if settings.currencyCalls != 1 {
		t.Fatalf("GetCurrencyConfig calls = %d, want 1", settings.currencyCalls)
	}
}

func TestGetGlobalConfigPreservesSubscribePath(t *testing.T) {
	for _, path := range []string{"", "/subscribe", "/custom/subscription", "/sub"} {
		t.Run(path, func(t *testing.T) {
			resp, err := globalConfigService(&globalConfigSettings{}, GlobalConfigSnapshot{
				Subscribe: config.SubscribeConfig{SubscribePath: path},
			}).GetGlobalConfig(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got := resp.Subscribe.SubscribePath; got != path {
				t.Fatalf("SubscribePath = %q, want %q", got, path)
			}
		})
	}
}

// Every part of the running configuration the public site reads reaches the
// response.
func TestGetGlobalConfigCopiesTheRunningConfiguration(t *testing.T) {
	resp, err := globalConfigService(&globalConfigSettings{}, GlobalConfigSnapshot{
		Site:     config.SiteConfig{SiteName: "Panel"},
		Email:    config.EmailConfig{EnableVerify: true, DomainSuffixList: "example.com"},
		Mobile:   config.MobileConfig{EnableWhitelist: true, Whitelist: []string{"86"}},
		Register: config.RegisterConfig{StopRegister: true, IpRegisterLimit: 3},
		Verify:   config.Verify{TurnstileSiteKey: "site-key", TurnstileSecret: "secret", LoginVerify: true},
		Invite:   config.InviteConfig{ForcedInvite: true, ReferralPercentage: 20},
	}).GetGlobalConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case resp.Site.SiteName != "Panel":
		t.Fatalf("site = %+v", resp.Site)
	case !resp.Auth.Email.EnableVerify || resp.Auth.Email.DomainSuffixList != "example.com":
		t.Fatalf("email = %+v", resp.Auth.Email)
	case !resp.Auth.Mobile.EnableWhitelist || len(resp.Auth.Mobile.Whitelist) != 1:
		t.Fatalf("mobile = %+v", resp.Auth.Mobile)
	case !resp.Auth.Register.StopRegister || resp.Auth.Register.IpRegisterLimit != 3:
		t.Fatalf("register = %+v", resp.Auth.Register)
	case resp.Verify.TurnstileSiteKey != "site-key" || !resp.Verify.EnableLoginVerify:
		t.Fatalf("verify = %+v", resp.Verify)
	case !resp.Invite.ForcedInvite || resp.Invite.ReferralPercentage != 20:
		t.Fatalf("invite = %+v", resp.Invite)
	case resp.WebAd:
		t.Fatal("web ads are on, want off")
	}
}
