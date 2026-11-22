package ads

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	entity "github.com/perfect-panel/backend/internal/module/support/entity/ads"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// GetPublicAds lists the ads for the public site: enabled and inside their
// schedule, so a scheduled ad does not show early and an expired one stops.
// The request's device and position are not filtered on yet.
func (s *Service) GetPublicAds(ctx context.Context, _ *dto.GetAdsRequest) (*dto.GetAdsResponse, error) {
	status := 1
	// The schedule the admin API writes (time.UnixMilli) is on the same
	// clock: the process zone is the application's.
	now := timeutil.Now()
	// One page of the largest size the repository serves.
	_, data, err := s.repo.GetAdsListByPage(ctx, 1, repository.MaxPageSize, entity.Filter{
		Status:   &status,
		ActiveAt: &now,
	})
	if err != nil {
		return nil, err
	}
	return &dto.GetAdsResponse{List: adsViews(data)}, nil
}
