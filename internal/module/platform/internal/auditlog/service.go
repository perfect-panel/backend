// Package auditlog implements the audit/message log subdomain of the
// platform module: filtered views over the system log, message log listing
// and the log retention settings. Only the module facade may reach it.
package auditlog

import (
	"context"
	"time"

	"github.com/perfect-panel/backend/internal/module/platform/internal/readmodel"
	"github.com/perfect-panel/backend/internal/repository/kernel"
)

// TrafficReader is the subdomain's port onto the network domain's traffic
// statistics, typed with the platform's read models; the composition root
// adapts the network facade to it.
type TrafficReader interface {
	QueryServerTrafficRanking(ctx context.Context, start, end time.Time) ([]readmodel.ServerTrafficRanking, error)
	QueryUserTrafficRanking(ctx context.Context, start, end time.Time) ([]readmodel.UserTrafficRanking, error)
	QueryTrafficLogDetails(ctx context.Context, filter *readmodel.TrafficLogDetailsFilter) ([]*readmodel.TrafficLog, int64, error)
}

// PlatformTransactor mirrors the store's platform-scoped transaction.
type PlatformTransactor interface {
	InPlatformTx(ctx context.Context, fn func(kernel.PlatformStore) error) error
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Logs    kernel.LogRepo
	System  kernel.SystemRepo
	Traffic TrafficReader
	// TrafficRetention prunes the network's raw traffic log along with the
	// system log.
	TrafficRetention TrafficLogPruner
	Store            PlatformTransactor
	// OnLogSettingChanged propagates a committed retention change to the
	// running configuration.
	OnLogSettingChanged func(autoClear bool, clearDays int64)
	// LogRetention reads the current (mutable) retention configuration.
	LogRetention func() (autoClear bool, clearDays int64)
}

func (d Deps) logRetention() (bool, int64) {
	if d.LogRetention == nil {
		return false, 0
	}
	return d.LogRetention()
}

// Service serves the audit and message logs for the platform facade.
type Service struct {
	deps Deps
}

// NewService builds the audit log service.
func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}
