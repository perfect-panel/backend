package wallet

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// QueryUserAffiliateList pages the accounts the current user referred, each
// shown by a masked login identifier.
func (s *Service) QueryUserAffiliateList(ctx context.Context, req *dto.QueryUserAffiliateListRequest) (*dto.QueryUserAffiliateListResponse, error) {
	u, ok := user.FromContext(ctx)
	if !ok {
		logger.WithContext(ctx).Error("current user is not found in context")
		return nil, xerr.Errorf(xerr.InvalidAccess, "Invalid Access")
	}
	data, total, err := s.deps.Affiliates.QueryAffiliateList(ctx, u.Id, req.Page, req.Size)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "Query User Affiliate List failed")
	}

	list := make([]dto.UserAffiliate, 0)
	for _, item := range data {
		list = append(list, dto.UserAffiliate{
			Identifier:   s.maskedAuthMethod(ctx, item).AuthIdentifier,
			Avatar:       item.Avatar,
			RegisteredAt: item.CreatedAt.UnixMilli(),
			Enable:       *item.Enable,
		})
	}
	return &dto.QueryUserAffiliateListResponse{
		Total: total,
		List:  list,
	}, nil
}

// maskedAuthMethod picks the login method an affiliate is shown by, auth
// types "6" and "7" first and otherwise the first one, and masks the middle
// third of its identifier. Methods the user row does not carry are read from
// the identity domain; a failed read leaves the affiliate unnamed.
func (s *Service) maskedAuthMethod(ctx context.Context, item *user.User) user.AuthMethods {
	authMethod := user.AuthMethods{}
	authMethods := item.AuthMethods
	if len(authMethods) == 0 {
		methods, err := s.deps.AuthMethods.FindUserAuthMethods(ctx, item.Id)
		if err == nil {
			for _, method := range methods {
				authMethods = append(authMethods, *method)
			}
		}
	}
	if len(authMethods) > 0 {
		for _, am := range authMethods {
			if am.AuthType == "6" || am.AuthType == "7" {
				authMethod = am
				break
			}
		}
		if authMethod.AuthIdentifier == "" {
			authMethod = authMethods[0]
		}

		hideTextLength := len(authMethod.AuthIdentifier) / 3
		if hideTextLength > 0 {
			authMethod.AuthIdentifier = authMethod.AuthIdentifier[0:hideTextLength] + "***" + authMethod.AuthIdentifier[hideTextLength*2:]
		}
	}
	return authMethod
}
