package adminuser

import (
	"context"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	"github.com/perfect-panel/backend/internal/infra/mapping"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetUserList pages the accounts, newest first, with their wallets. Phone
// numbers are shown in the international format.
func (s *Service) GetUserList(ctx context.Context, req *dto.GetUserListRequest) (*dto.GetUserListResponse, error) {
	list, total, err := s.deps.Users.QueryPageList(ctx, req.Page, req.Size, &user.UserFilterParams{
		UserId:             req.UserId,
		Search:             req.Search,
		Unscoped:           req.Unscoped,
		SubscribeId:        req.SubscribeId,
		UserSubscribeId:    req.UserSubscribeId,
		UserSubscribeToken: req.UserSubscribeToken,
		Order:              "DESC",
	})
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "GetUserList failed: %v", err.Error())
	}

	// Wallet values come from the billing-owned table (batch read);
	// accounts without a wallet row read as zero.
	ids := make([]int64, 0, len(list))
	for _, item := range list {
		ids = append(ids, item.Id)
	}
	wallets, err := s.deps.Wallet.FindWallets(ctx, ids)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "GetUserList load wallets failed: %v", err.Error())
	}

	userRespList := make([]dto.User, 0, len(list))
	for _, item := range list {
		var u dto.User
		if err := mapping.Copy(&u, item); err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "map user %d", item.Id)
		}
		if w, ok := wallets[item.Id]; ok {
			u.Balance = w.Balance
			u.GiftAmount = w.GiftAmount
			u.Commission = w.Commission
		}
		if item.DeletedAt.Valid {
			u.DeletedAt = item.DeletedAt.Time.UnixMilli()
		}

		authMethods := make([]dto.UserAuthMethod, len(u.AuthMethods))
		for i, method := range u.AuthMethods {
			authMethods[i] = method
			if method.AuthType == "mobile" {
				authMethods[i].AuthIdentifier = identifier.FormatToInternational(method.AuthIdentifier)
			}
		}
		u.AuthMethods = authMethods

		userRespList = append(userRespList, u)
	}

	return &dto.GetUserListResponse{
		Total: total,
		List:  userRespList,
	}, nil
}
