package system

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/platform"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
)

// Settings is the part of the platform facade the system setting handlers
// call.
type Settings interface {
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
}

// The platform facade serves the system setting handlers.
var _ Settings = platform.Service(nil)
