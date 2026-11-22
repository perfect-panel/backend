package authn

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// maxUserAgentLength matches the user_device.user_agent column (02149).
const maxUserAgentLength = 512

func truncateUserAgent(ua string) string {
	if len(ua) <= maxUserAgentLength {
		return ua
	}
	cut := ua[:maxUserAgentLength]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// deviceMetadata is the client address and user agent a device records, from
// the request metadata.
func deviceMetadata(ctx context.Context) (ip, userAgent string) {
	meta, _ := requestmeta.From(ctx)
	return meta.ClientIP, truncateUserAgent(meta.UserAgent)
}

// BindDeviceToUser creates a binding or refreshes the current owner's device.
// An identifier is not proof of ownership: never move another user's binding
// or disable that account. Anonymous users can add email/mobile credentials
// through authenticated profile binding without abandoning their account.
func (s *Service) BindDeviceToUser(ctx context.Context, deviceIdentifier string, userID int64) (*user.Device, error) {
	if deviceIdentifier == "" {
		return nil, nil
	}
	if userID <= 0 || len(deviceIdentifier) > 255 || strings.TrimSpace(deviceIdentifier) != deviceIdentifier {
		return nil, xerr.NewErrCode(xerr.InvalidParams)
	}
	ip, userAgent := deviceMetadata(ctx)
	devices := s.deps.Store.UserDevice()
	device, err := devices.FindOneDeviceByIdentifier(ctx, deviceIdentifier)
	if err == nil {
		return s.touchOwnDevice(ctx, device, userID, ip, userAgent)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find device")
	}

	device = &user.Device{UserId: userID, Identifier: deviceIdentifier, Ip: ip, UserAgent: userAgent, Enabled: true}
	err = s.deps.Store.InIdentityTx(ctx, func(store repository.IdentityStore) error {
		if err := store.UserAuth().InsertUserAuthMethods(ctx, &user.AuthMethods{
			UserId: userID, AuthType: identifier.Device, AuthIdentifier: deviceIdentifier, Verified: true,
		}); err != nil {
			return err
		}
		return store.UserDevice().InsertDevice(ctx, device)
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		// A concurrent creator may only be reused if it belongs to this user.
		existing, queryErr := devices.FindOneDeviceByIdentifier(ctx, deviceIdentifier)
		if queryErr != nil {
			return nil, xerr.Wrapf(queryErr, xerr.DatabaseQueryError, "find concurrently bound device")
		}
		return s.touchOwnDevice(ctx, existing, userID, ip, userAgent)
	}
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseInsertError, "bind device")
	}
	return device, nil
}

func (s *Service) touchOwnDevice(ctx context.Context, device *user.Device, userID int64, ip, userAgent string) (*user.Device, error) {
	if device == nil || device.UserId != userID {
		return nil, xerr.Errorf(xerr.InvalidAccess, "device belongs to another account")
	}
	if !device.Enabled {
		return nil, xerr.Errorf(xerr.InvalidAccess, "device is disabled")
	}
	updated, err := s.deps.Store.UserDevice().TouchDevice(ctx, device.Id, userID, ip, userAgent)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseUpdateError, "refresh device")
	}
	if !updated {
		return nil, xerr.Errorf(xerr.InvalidAccess, "device binding changed")
	}
	device.Ip, device.UserAgent = ip, userAgent
	return device, nil
}
