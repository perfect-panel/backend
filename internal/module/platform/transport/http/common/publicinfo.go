package common

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/platform"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
)

// PublicInfo is the part of the platform facade the site-level handlers
// call.
type PublicInfo interface {
	GetGlobalConfig(ctx context.Context) (*dto.GetGlobalConfigResponse, error)
	GetTos(ctx context.Context) (*dto.GetTosResponse, error)
	GetPrivacyPolicy(ctx context.Context) (*dto.PrivacyPolicyConfig, error)
	GetStat(ctx context.Context) (*dto.GetStatResponse, error)
	GetClient(ctx context.Context) (*dto.GetSubscribeClientResponse, error)
	Heartbeat(ctx context.Context) (*dto.HeartbeatResponse, error)
}

// The platform facade serves the site-level handlers.
var _ PublicInfo = platform.Service(nil)
