package auth

import (
	"context"

	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/authmethod"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

type BindDeviceLogic struct {
	logger.Logger
	ctx  context.Context
	deps BindDeviceDependencies
}

func NewBindDeviceLogic(ctx context.Context, deps BindDeviceDependencies) *BindDeviceLogic {
	return &BindDeviceLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

// BindDeviceToUser binds a device to a user
// If the device is already bound to another user, it will disable that user and bind the device to the current user
func (l *BindDeviceLogic) BindDeviceToUser(identifier, ip, userAgent string, currentUserId int64) error {
	if identifier == "" {
		// No device identifier provided, skip binding
		return nil
	}

	l.Infow("binding device to user",
		logger.Field("identifier", identifier),
		logger.Field("user_id", currentUserId),
		logger.Field("ip", ip),
	)

	// Check if device exists
	deviceInfo, err := l.deps.Store.UserDevice().FindOneDeviceByIdentifier(l.ctx, identifier)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Device not found, create new device record
			return l.createDeviceForUser(identifier, ip, userAgent, currentUserId)
		}
		l.Errorw("failed to query device",
			logger.Field("identifier", identifier),
			logger.Field("error", err.Error()),
		)
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query device failed: %v", err.Error())
	}

	// Device exists, check if it's bound to current user
	if deviceInfo.UserId == currentUserId {
		// Already bound to current user, just update IP and UserAgent
		l.Infow("device already bound to current user, updating info",
			logger.Field("identifier", identifier),
			logger.Field("user_id", currentUserId),
		)
		deviceInfo.Ip = ip
		deviceInfo.UserAgent = userAgent
		if err := l.deps.Store.UserDevice().UpdateDevice(l.ctx, deviceInfo); err != nil {
			l.Errorw("failed to update device",
				logger.Field("identifier", identifier),
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update device failed: %v", err.Error())
		}
		return nil
	}

	// Device is bound to another user, need to disable old user and rebind
	l.Infow("device bound to another user, rebinding",
		logger.Field("identifier", identifier),
		logger.Field("old_user_id", deviceInfo.UserId),
		logger.Field("new_user_id", currentUserId),
	)

	return l.rebindDeviceToNewUser(deviceInfo, ip, userAgent, currentUserId)
}

func (l *BindDeviceLogic) createDeviceForUser(identifier, ip, userAgent string, userId int64) error {
	l.Infow("creating new device for user",
		logger.Field("identifier", identifier),
		logger.Field("user_id", userId),
	)

	err := l.deps.Store.InTx(l.ctx, func(store repository.Store) error {
		// Create device auth method
		authMethod := &user.AuthMethods{
			UserId:         userId,
			AuthType:       authmethod.Device,
			AuthIdentifier: identifier,
			Verified:       true,
		}
		if err := store.UserAuth().InsertUserAuthMethods(l.ctx, authMethod); err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				// Concurrent request already created this auth method;
				// propagate so the outer handler can retry gracefully.
				return err
			}
			l.Errorw("failed to create device auth method",
				logger.Field("user_id", userId),
				logger.Field("identifier", identifier),
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create device auth method failed: %v", err)
		}

		// Create device record
		deviceInfo := &user.Device{
			Ip:         ip,
			UserId:     userId,
			UserAgent:  userAgent,
			Identifier: identifier,
			Enabled:    true,
			Online:     false,
		}
		if err := store.UserDevice().InsertDevice(l.ctx, deviceInfo); err != nil {
			l.Errorw("failed to create device",
				logger.Field("user_id", userId),
				logger.Field("identifier", identifier),
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create device failed: %v", err)
		}

		return nil
	})

	// Handle duplicate key from concurrent device creation.
	// The transaction is rolled back, and another request has already committed
	// the device record — re-read it and route to the appropriate path.
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		l.Infow("device concurrently created, retrying as existing device",
			logger.Field("identifier", identifier),
			logger.Field("user_id", userId),
		)

		deviceInfo, findErr := l.deps.Store.UserDevice().FindOneDeviceByIdentifier(l.ctx, identifier)
		if findErr != nil {
			l.Errorw("failed to find device after concurrent creation",
				logger.Field("identifier", identifier),
				logger.Field("error", findErr.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find device after concurrent creation failed: %v", findErr)
		}

		// Already bound to current user — just update IP / UserAgent
		if deviceInfo.UserId == userId {
			deviceInfo.Ip = ip
			deviceInfo.UserAgent = userAgent
			if err := l.deps.Store.UserDevice().UpdateDevice(l.ctx, deviceInfo); err != nil {
				l.Errorw("failed to update device",
					logger.Field("identifier", identifier),
					logger.Field("error", err.Error()),
				)
				return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update device failed: %v", err)
			}
			return nil
		}

		// Bound to another user — rebind
		return l.rebindDeviceToNewUser(deviceInfo, ip, userAgent, userId)
	}

	if err != nil {
		l.Errorw("device creation failed",
			logger.Field("identifier", identifier),
			logger.Field("user_id", userId),
			logger.Field("error", err.Error()),
		)
		return err
	}

	l.Infow("device created successfully",
		logger.Field("identifier", identifier),
		logger.Field("user_id", userId),
	)

	return nil
}

func (l *BindDeviceLogic) rebindDeviceToNewUser(deviceInfo *user.Device, ip, userAgent string, newUserId int64) error {
	oldUserId := deviceInfo.UserId

	err := l.deps.Store.InTx(l.ctx, func(store repository.Store) error {
		// Check if old user has other auth methods besides device
		authMethods, err := store.UserAuth().FindUserAuthMethods(l.ctx, oldUserId)
		if err != nil {
			l.Errorw("failed to query auth methods for old user",
				logger.Field("old_user_id", oldUserId),
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query auth methods failed: %v", err)
		}

		// Count non-device auth methods
		nonDeviceAuthCount := 0
		for _, auth := range authMethods {
			if auth.AuthType != authmethod.Device {
				nonDeviceAuthCount++
			}
		}

		// Only disable old user if they have no other auth methods
		if nonDeviceAuthCount == 0 {
			falseVal := false
			oldUser, err := store.User().FindOne(l.ctx, oldUserId)
			if err != nil {
				l.Errorw("failed to find old user",
					logger.Field("old_user_id", oldUserId),
					logger.Field("error", err.Error()),
				)
				return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find old user failed: %v", err)
			}
			oldUser.Enable = &falseVal
			if err := store.User().Update(l.ctx, oldUser); err != nil {
				l.Errorw("failed to disable old user",
					logger.Field("old_user_id", oldUserId),
					logger.Field("error", err.Error()),
				)
				return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "disable old user failed: %v", err)
			}

			l.Infow("disabled old user (no other auth methods)",
				logger.Field("old_user_id", oldUserId),
			)
		} else {
			l.Infow("old user has other auth methods, not disabling",
				logger.Field("old_user_id", oldUserId),
				logger.Field("non_device_auth_count", nonDeviceAuthCount),
			)
		}

		// Update device auth method to new user
		if err := store.UserAuth().UpdateUserAuthMethodOwner(l.ctx, authmethod.Device, deviceInfo.Identifier, newUserId); err != nil {
			l.Errorw("failed to update device auth method",
				logger.Field("identifier", deviceInfo.Identifier),
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update device auth method failed: %v", err)
		}

		// Update device record
		deviceInfo.UserId = newUserId
		deviceInfo.Ip = ip
		deviceInfo.UserAgent = userAgent
		deviceInfo.Enabled = true

		if err := store.UserDevice().UpdateDevice(l.ctx, deviceInfo); err != nil {
			l.Errorw("failed to update device",
				logger.Field("identifier", deviceInfo.Identifier),
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update device failed: %v", err)
		}

		return nil
	})

	if err != nil {
		l.Errorw("device rebinding failed",
			logger.Field("identifier", deviceInfo.Identifier),
			logger.Field("old_user_id", oldUserId),
			logger.Field("new_user_id", newUserId),
			logger.Field("error", err.Error()),
		)
		return err
	}

	l.Infow("device rebound successfully",
		logger.Field("identifier", deviceInfo.Identifier),
		logger.Field("old_user_id", oldUserId),
		logger.Field("new_user_id", newUserId),
	)

	return nil
}
