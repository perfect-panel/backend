// Package plan implements the subscription plan management subdomain of the
// subscription module: plan and group CRUD, ordering and token resets. Only
// the module facade may reach it.
package plan

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/repository"
)

// SubscriptionTransactor mirrors the store's subscription-scoped transaction.
type SubscriptionTransactor interface {
	InSubscriptionTx(ctx context.Context, fn func(repository.SubscriptionStore) error) error
}

type Deps struct {
	Plans    repository.SubscribeRepo
	UserSubs repository.UserSubscriptionRepo
	Store    SubscriptionTransactor
	// NotifyPlanChanged broadcasts a plan update to connected devices.
	NotifyPlanChanged func()
}

func (d Deps) notifyPlanChanged() {
	if d.NotifyPlanChanged != nil {
		d.NotifyPlanChanged()
	}
}

type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

func (s *Service) CreateSubscribe(ctx context.Context, req *dto.CreateSubscribeRequest) error {
	return newCreateSubscribeLogic(ctx, s.deps).CreateSubscribe(req)
}

func (s *Service) UpdateSubscribe(ctx context.Context, req *dto.UpdateSubscribeRequest) error {
	return newUpdateSubscribeLogic(ctx, s.deps).UpdateSubscribe(req)
}

func (s *Service) DeleteSubscribe(ctx context.Context, req *dto.DeleteSubscribeRequest) error {
	return newDeleteSubscribeLogic(ctx, s.deps).DeleteSubscribe(req)
}

func (s *Service) BatchDeleteSubscribe(ctx context.Context, req *dto.BatchDeleteSubscribeRequest) error {
	return newBatchDeleteSubscribeLogic(ctx, s.deps).BatchDeleteSubscribe(req)
}

func (s *Service) GetSubscribeList(ctx context.Context, req *dto.GetSubscribeListRequest) (*dto.GetSubscribeListResponse, error) {
	return newGetSubscribeListLogic(ctx, s.deps).GetSubscribeList(req)
}

func (s *Service) GetSubscribeDetails(ctx context.Context, req *dto.GetSubscribeDetailsRequest) (*dto.Subscribe, error) {
	return newGetSubscribeDetailsLogic(ctx, s.deps).GetSubscribeDetails(req)
}

func (s *Service) SubscribeSort(ctx context.Context, req *dto.SubscribeSortRequest) error {
	return newSubscribeSortLogic(ctx, s.deps).SubscribeSort(req)
}

func (s *Service) ResetAllSubscribeToken(ctx context.Context) (*dto.ResetAllSubscribeTokenResponse, error) {
	return newResetAllSubscribeTokenLogic(ctx, s.deps).ResetAllSubscribeToken()
}

func (s *Service) CreateSubscribeGroup(ctx context.Context, req *dto.CreateSubscribeGroupRequest) error {
	return newCreateSubscribeGroupLogic(ctx, s.deps).CreateSubscribeGroup(req)
}

func (s *Service) UpdateSubscribeGroup(ctx context.Context, req *dto.UpdateSubscribeGroupRequest) error {
	return newUpdateSubscribeGroupLogic(ctx, s.deps).UpdateSubscribeGroup(req)
}

func (s *Service) DeleteSubscribeGroup(ctx context.Context, req *dto.DeleteSubscribeGroupRequest) error {
	return newDeleteSubscribeGroupLogic(ctx, s.deps).DeleteSubscribeGroup(req)
}

func (s *Service) BatchDeleteSubscribeGroup(ctx context.Context, req *dto.BatchDeleteSubscribeGroupRequest) error {
	return newBatchDeleteSubscribeGroupLogic(ctx, s.deps).BatchDeleteSubscribeGroup(req)
}

func (s *Service) GetSubscribeGroupList(ctx context.Context) (*dto.GetSubscribeGroupListResponse, error) {
	return newGetSubscribeGroupListLogic(ctx, s.deps).GetSubscribeGroupList()
}
