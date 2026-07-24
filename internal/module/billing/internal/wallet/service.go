// Package wallet implements the user-facing wallet subdomain of the billing
// module: commission withdrawal, balance/commission statements and the
// affiliate earnings overview. Only the module facade may reach it.
package wallet

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
)

// Transactor mirrors the facade's billing-scoped transaction port.
type Transactor interface {
	InBillingTx(ctx context.Context, fn func(repository.BillingStore) error) error
}

// AffiliateReader is the read-only port onto the identity domain's referral
// relations; the legacy user repository satisfies it structurally. The
// commission amounts belong to billing, the referral tree stays with
// identity.
type AffiliateReader interface {
	CountAffiliates(ctx context.Context, refererId int64) (int64, error)
	QueryAffiliateList(ctx context.Context, refererId int64, page, size int) ([]*user.User, int64, error)
}

// AuthMethodReader is the read-only identity port used to render an
// affiliate's masked login identifier.
type AuthMethodReader interface {
	FindUserAuthMethods(ctx context.Context, userId int64) ([]*user.AuthMethods, error)
}

// Deps declares the subdomain's dependencies; the module facade forwards
// them from the composition root.
type Deps struct {
	Logs        repository.LogRepo
	Cache       repository.UserCacheRepo
	Affiliates  AffiliateReader
	AuthMethods AuthMethodReader
	Tx          Transactor
}

// Service is the wallet subdomain entry point used by the billing facade.
type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

func (s *Service) CommissionWithdraw(ctx context.Context, req *dto.CommissionWithdrawRequest) (*dto.WithdrawalLog, error) {
	return newCommissionWithdrawLogic(ctx, s.deps).CommissionWithdraw(req)
}

func (s *Service) QueryUserBalanceLog(ctx context.Context) (*dto.QueryUserBalanceLogListResponse, error) {
	return newQueryUserBalanceLogLogic(ctx, s.deps).QueryUserBalanceLog()
}

func (s *Service) QueryUserCommissionLog(ctx context.Context, req *dto.QueryUserCommissionLogListRequest) (*dto.QueryUserCommissionLogListResponse, error) {
	return newQueryUserCommissionLogLogic(ctx, s.deps).QueryUserCommissionLog(req)
}

func (s *Service) QueryWithdrawalLog(ctx context.Context, req *dto.QueryWithdrawalLogListRequest) (*dto.QueryWithdrawalLogListResponse, error) {
	return newQueryWithdrawalLogLogic(ctx, s.deps).QueryWithdrawalLog(req)
}

func (s *Service) QueryUserAffiliate(ctx context.Context) (*dto.QueryUserAffiliateCountResponse, error) {
	return newQueryUserAffiliateLogic(ctx, s.deps).QueryUserAffiliate()
}

func (s *Service) QueryUserAffiliateList(ctx context.Context, req *dto.QueryUserAffiliateListRequest) (*dto.QueryUserAffiliateListResponse, error) {
	return newQueryUserAffiliateListLogic(ctx, s.deps).QueryUserAffiliateList(req)
}
