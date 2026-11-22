package systemsetting

import (
	"context"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// GetVerifyConfig returns the stored verification settings, the Turnstile
// secret masked. It only reads: the running configuration is re-initialized
// when the settings are updated.
func (s *Service) GetVerifyConfig(ctx context.Context) (*dto.VerifyConfig, error) {
	resp, err := s.storedVerifyConfig(ctx)
	if err != nil {
		return nil, err
	}
	maskVerifySecrets(resp)
	return resp, nil
}

// storedVerifyConfig reads the verification settings as stored, secrets in
// clear.
func (s *Service) storedVerifyConfig(ctx context.Context) (*dto.VerifyConfig, error) {
	rows, err := s.deps.System.GetVerifyConfig(ctx)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "get verify config: %v", err)
	}
	resp := &dto.VerifyConfig{}
	config.SystemConfigSliceReflectToStruct(rows, resp)
	return resp, nil
}
