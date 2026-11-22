package authmethodadmin

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetAuthMethodList returns every stored method with its configuration.
func (s *Service) GetAuthMethodList(ctx context.Context) (*dto.GetAuthMethodListResponse, error) {
	methods, err := s.deps.Auths.FindAll(ctx)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "list auth methods")
	}
	var list []dto.AuthMethodConfig
	for _, method := range methods {
		item, err := methodConfig(method)
		if err != nil {
			return nil, err
		}
		list = append(list, *item)
	}
	return &dto.GetAuthMethodListResponse{List: list}, nil
}
