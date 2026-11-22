package adminuser

import (
	"context"

	"github.com/perfect-panel/backend/internal/auth/password"
	"github.com/perfect-panel/backend/internal/auth/usersession"
	"github.com/perfect-panel/backend/internal/module/billing/entity/wallet"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// UpdateUserBasicInfo applies an administrator's edit of an account: its
// profile columns and, in a billing transaction of its own, its wallet.
func (s *Service) UpdateUserBasicInfo(ctx context.Context, req *dto.UpdateUserBasicInfoRequest) error {
	// The admin edit spans two domains by design — identity profile fields
	// and a billing money adjustment — so it runs as two sequential domain
	// transactions. The identity transaction goes first because it carries
	// the request validations (avatar, demo-mode password): a rejected edit
	// then leaves the money untouched. A failure after the profile commit
	// leaves the money unadjusted for the admin to retry — the same
	// partial-failure surface the flows will have as services.
	if err := validateReferralPercentage(req.ReferralPercentage); err != nil {
		return err
	}
	accessStateChanged := false
	passwordChanged := false
	err := s.deps.Store.InIdentityTx(ctx, func(store repository.IdentityStore) error {
		userInfo, err := store.User().FindOneForUpdate(ctx, req.UserId)
		if err != nil {
			return xerr.Wrapf(err, xerr.DatabaseQueryError, "find user %d", req.UserId)
		}
		if err := validateAvatarUpdate(userInfo.Avatar, req.Avatar); err != nil {
			return err
		}
		// The last enabled administrator keeps the panel administrable: it
		// is neither demoted nor disabled. The check runs in the transaction
		// that locked the row, so two edits cannot each see the other as
		// the remaining administrator.
		if isEnabledAdministrator(userInfo) && (!req.IsAdmin || !req.Enable) {
			if err := ensureAnotherAdministrator(ctx, store.User(), userInfo.Id); err != nil {
				return err
			}
		}
		accessStateChanged = userInfo.Enable == nil || *userInfo.Enable != req.Enable
		columns := map[string]any{
			"avatar":              req.Avatar,
			"refer_code":          req.ReferCode,
			"referer_id":          req.RefererId,
			"only_first_purchase": req.OnlyFirstPurchase,
			"referral_percentage": req.ReferralPercentage,
			"enable":              req.Enable,
			"is_admin":            req.IsAdmin,
		}
		passwordChanged = req.Password != "" && req.Password != "***"
		if passwordChanged {
			if userInfo.Id == demoAdminID && demoMode() {
				return demoRestricted("modify the admin user's password")
			}
			for column, value := range password.UserColumns(req.Password) {
				columns[column] = value
			}
		}
		// Only these profile columns are written: the billing-owned money
		// columns go through the admin's wallet adjustment in its own
		// billing transaction below.
		if err := store.User().UpdateColumns(ctx, userInfo.Id, columns); err != nil {
			return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update user %d", userInfo.Id)
		}
		if accessStateChanged {
			return s.rotateAccessEpoch(ctx, req.UserId, req.Enable)
		}
		return nil
	})
	if err != nil {
		// The generic code keeps the validation's own (an invalid avatar,
		// the demo-mode refusal).
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "update user %d", req.UserId)
	}
	// Account state changes must invalidate both subscription-token caches and
	// node-facing user lists. In particular, disabling a user takes effect at
	// the service plane immediately instead of waiting for the five-minute TTL.
	if accessStateChanged {
		clearUserAccessCaches(ctx, s.deps, []int64{req.UserId})
	}
	// Rotate after the password write is visible: a concurrent sign-in may
	// have checked the previous password until the transaction committed.
	if passwordChanged {
		if err := usersession.Revoke(ctx, s.deps.Redis, req.UserId); err != nil {
			return xerr.Wrapf(err, xerr.ERROR, "revoke sessions of user %d", req.UserId)
		}
	}

	// The money adjustment is billing's: it compares and writes the latest
	// values under the wallet lock, with their audit logs, in the billing
	// module's own transaction. Only the amounts the request carries are
	// forwarded; an omitted one stays as it is.
	err = s.deps.Wallet.AdjustWallet(ctx, wallet.Adjustment{
		UserId:     req.UserId,
		Balance:    req.Balance,
		GiftAmount: req.GiftAmount,
		Commission: req.Commission,
	})
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "adjust wallet of user %d", req.UserId)
	}
	return nil
}

// validateAvatarUpdate permits retaining or clearing an existing avatar. A new
// avatar must be a Base64 image no larger than 1024 KiB; OAuth providers may
// persist remote HTTPS avatar URLs, which must remain usable during unrelated
// profile updates.
func validateAvatarUpdate(currentAvatar, requestedAvatar string) error {
	if requestedAvatar == "" || requestedAvatar == currentAvatar {
		return nil
	}

	if !IsValidImageSize(requestedAvatar, 1024) {
		return xerr.Errorf(xerr.InvalidParams, "invalid avatar")
	}

	return nil
}
