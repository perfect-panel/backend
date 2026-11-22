package profile

import (
	"context"
	"encoding/json"

	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateUserRules stores the calling account's rules; an empty list leaves
// the stored ones.
func (s *Service) UpdateUserRules(ctx context.Context, req *dto.UpdateUserRulesRequest) error {
	u, err := currentUser(ctx)
	if err != nil {
		return err
	}
	if len(req.Rules) > 0 {
		bytes, err := json.Marshal(req.Rules)
		if err != nil {
			return xerr.Wrapf(err, xerr.ERROR, "marshal rules")
		}
		if err := s.deps.Users.UpdateColumns(ctx, u.Id, map[string]any{"rules": string(bytes)}); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update rules of user %d", u.Id)
		}
	}
	return nil
}
