// Package selfsub implements the user self-service subscription management
// of the subscription module: viewing, token reset, notes, and the two-phase
// cancellation with its billing refund. Only the module facade may reach it.
package selfsub

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/repository"
)

type Deps struct {
	UserSubs repository.UserSubscriptionRepo
	Plans    repository.SubscribeRepo
	// Users/Orders/Logs are read (and, for the refund, wallet) ports onto the
	// identity, billing and platform domains.
	Users  repository.UserRepo
	Orders repository.OrderRepo
	Cache  repository.UserCacheRepo
	Logs   repository.LogRepo
	Inbox  repository.InboxRepo
	Store  repository.Store
	// SingleModel forbids holding more than one blocking subscription.
	SingleModel bool
}

type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

func (s *Service) QueryUserSubscribe(ctx context.Context) (*dto.QueryUserSubscribeListResponse, error) {
	return newQueryUserSubscribeLogic(ctx, s.deps).QueryUserSubscribe()
}

func (s *Service) ResetUserSubscribeToken(ctx context.Context, req *dto.ResetUserSubscribeTokenRequest) error {
	return newResetUserSubscribeTokenLogic(ctx, s.deps).ResetUserSubscribeToken(req)
}

func (s *Service) GetSubscribeLog(ctx context.Context, req *dto.GetSubscribeLogRequest) (*dto.GetSubscribeLogResponse, error) {
	return newGetSubscribeLogLogic(ctx, s.deps).GetSubscribeLog(req)
}

func (s *Service) UpdateUserSubscribeNote(ctx context.Context, req *dto.UpdateUserSubscribeNoteRequest) error {
	return newUpdateUserSubscribeNoteLogic(ctx, s.deps).UpdateUserSubscribeNote(req)
}

func (s *Service) PreUnsubscribe(ctx context.Context, req *dto.PreUnsubscribeRequest) (*dto.PreUnsubscribeResponse, error) {
	return newPreUnsubscribeLogic(ctx, s.deps).PreUnsubscribe(req)
}

func (s *Service) Unsubscribe(ctx context.Context, req *dto.UnsubscribeRequest) error {
	return newUnsubscribeLogic(ctx, s.deps).Unsubscribe(req)
}
