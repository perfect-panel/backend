package adminuser

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetUserAuthMethod lists the account's identities as stored.
func (s *Service) GetUserAuthMethod(ctx context.Context, req *dto.GetUserAuthMethodRequest) (*dto.GetUserAuthMethodResponse, error) {
	methods, err := s.deps.UserAuths.FindUserAuthMethods(ctx, req.UserId)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetUserAuthMethod] get user auth methods failed", logger.Field("err", err.Error()))
		return nil, xerr.Errorf(xerr.DatabaseQueryError, "Get User Auth Method Error")
	}
	list := make([]dto.UserAuthMethod, 0, len(methods))
	for _, method := range methods {
		list = append(list, dto.UserAuthMethod{
			AuthType:       method.AuthType,
			AuthIdentifier: method.AuthIdentifier,
			Verified:       method.Verified,
		})
	}
	return &dto.GetUserAuthMethodResponse{
		AuthMethods: list,
	}, nil
}
