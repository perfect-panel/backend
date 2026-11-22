package usersub

import (
	"context"

	"github.com/perfect-panel/backend/internal/infra/mapping"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetUserSubscribeById returns a subscription with its plan and its owner's
// account.
func (s *Service) GetUserSubscribeById(ctx context.Context, req *dto.GetUserSubscribeByIdRequest) (*dto.UserSubscribeDetail, error) {
	log := logger.WithContext(ctx)
	sub, err := s.deps.UserSubs.FindOneSubscribeDetailsById(ctx, req.Id)
	if err != nil {
		log.Errorw("[GetUserSubscribeByIdLogic] FindOneSubscribeDetailsById error", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "FindOneSubscribeDetailsById error: %v", err.Error())
	}
	var subscribeDetails dto.UserSubscribeDetail
	if err := mapping.Copy(&subscribeDetails, sub); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "map subscription %d", sub.Id)
	}
	// The identity row is composed at the module layer instead of a
	// cross-domain preload (ADR-001 step 5).
	owner, err := s.deps.Users.FindOne(ctx, sub.UserId)
	if err != nil {
		log.Errorw("[GetUserSubscribeByIdLogic] load subscription owner failed",
			logger.Field("error", err.Error()), logger.Field("user_id", sub.UserId))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "load subscription owner error: %v", err.Error())
	}
	if err := mapping.Copy(&subscribeDetails.User, owner); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "map the owner of subscription %d", sub.Id)
	}
	return &subscribeDetails, nil
}
