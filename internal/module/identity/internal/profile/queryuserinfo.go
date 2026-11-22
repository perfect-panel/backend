package profile

import (
	"context"
	"sort"

	"github.com/perfect-panel/backend/internal/infra/mapping"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// QueryUserInfo returns the calling account with its wallet. Identifiers
// other than the email address are masked, the device identifiers included:
// they sign the devices in.
func (s *Service) QueryUserInfo(ctx context.Context) (*dto.User, error) {
	u, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	resp := &dto.User{}
	if err := mapping.Copy(resp, u); err != nil {
		return nil, xerr.Wrapf(err, xerr.ERROR, "map user %d", u.Id)
	}
	// Wallet values come from the billing-owned table; a read failure fails
	// the request rather than rendering zero balances (ADR-001 step 5).
	w, err := s.deps.Wallet.FindWallet(ctx, u.Id)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "load user wallet error: %v", err.Error())
	}
	if w != nil {
		resp.Balance = w.Balance
		resp.GiftAmount = w.GiftAmount
		resp.Commission = w.Commission
	}

	var userMethods []dto.UserAuthMethod
	for _, method := range resp.AuthMethods {
		userMethods = append(userMethods, maskAuthMethod(method))
	}

	// The email binding comes first and the mobile one second; the others
	// keep their order.
	sort.SliceStable(userMethods, func(i, j int) bool {
		return getAuthTypePriority(userMethods[i].AuthType) < getAuthTypePriority(userMethods[j].AuthType)
	})

	resp.AuthMethods = userMethods
	resp.UserDevices = maskDevices(resp.UserDevices)
	return resp, nil
}

// getAuthTypePriority is the sort rank of a binding in the account view:
// email 1, mobile 2, every other type 100.
func getAuthTypePriority(authType string) int {
	switch authType {
	case "email":
		return 1
	case "mobile":
		return 2
	default:
		return 100
	}
}
