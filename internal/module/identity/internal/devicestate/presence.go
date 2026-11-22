package devicestate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"gorm.io/gorm"
)

// MarkOnline shows the device identifier online once it connected to the
// device WebSocket. A device removed meanwhile is skipped, and a disabled one
// stays offline: the repository sets only enabled devices online.
func MarkOnline(ctx context.Context, devices repository.UserDeviceRepo, identifier string) error {
	device, found, err := findDevice(ctx, devices, identifier)
	if err != nil || !found {
		return err
	}
	if err := devices.SetDeviceOnline(ctx, device.Id, true); err != nil {
		return fmt.Errorf("mark device %s online: %w", identifier, err)
	}
	return nil
}

// MarkOffline shows the device identifier offline once its connection,
// opened at connectedAt, closed, and records the connection's online time for
// the account userID. The connection ended whether or not the flag could be
// cleared, so its online time is recorded either way. A device removed
// meanwhile is skipped.
func MarkOffline(ctx context.Context, devices repository.UserDeviceRepo, userID int64, identifier string, connectedAt time.Time) error {
	return markOffline(ctx, devices, userID, identifier, connectedAt, timeutil.Now())
}

// markOffline is MarkOffline at the instant now, which decides the day the
// connection is recorded on; tests pin it.
func markOffline(ctx context.Context, devices repository.UserDeviceRepo, userID int64, identifier string, connectedAt, now time.Time) error {
	device, found, err := findDevice(ctx, devices, identifier)
	if err != nil || !found {
		return err
	}
	var offlineErr error
	if err := devices.SetDeviceOnline(ctx, device.Id, false); err != nil {
		offlineErr = fmt.Errorf("mark device %s offline: %w", identifier, err)
	}

	record := user.DeviceOnlineRecord{
		UserId:        userID,
		Identifier:    identifier,
		OnlineTime:    connectedAt,
		OfflineTime:   now,
		OnlineSeconds: int64(now.Sub(connectedAt).Seconds()),
		DurationDays:  1,
	}
	// The streak of consecutive online days continues from the account's
	// records of the previous day: every connection of a day carries the
	// previous day's streak plus one, or starts a streak of one day when the
	// account was not online the day before.
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterday := today.AddDate(0, 0, -1)
	var streakErr error
	previous, err := devices.FindDeviceOnlineRecord(ctx, userID, yesterday.Format(time.DateTime), today.Format(time.DateTime))
	switch {
	case err == nil:
		record.DurationDays = previous.DurationDays + 1
	case !errors.Is(err, gorm.ErrRecordNotFound):
		// The connection's online time is still recorded, as a new streak.
		streakErr = fmt.Errorf("find the previous day's online record of user %d: %w", userID, err)
	}
	if err := devices.InsertDeviceOnlineRecord(ctx, &record); err != nil {
		return errors.Join(offlineErr, streakErr, fmt.Errorf("record the online time of device %s: %w", identifier, err))
	}
	return errors.Join(offlineErr, streakErr)
}

// findDevice looks the device up by its identifier. A device that no longer
// exists has no presence to keep, so it is reported as not found rather than
// as an error.
func findDevice(ctx context.Context, devices repository.UserDeviceRepo, identifier string) (*user.Device, bool, error) {
	device, err := devices.FindOneDeviceByIdentifier(ctx, identifier)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("find device %s: %w", identifier, err)
	}
	return device, true, nil
}
