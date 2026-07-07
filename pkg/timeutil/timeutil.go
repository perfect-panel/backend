// Package timeutil provides centralized timezone handling for the application.
// Call LoadLocation once during initialization to set the canonical timezone,
// then use Now() and Location() throughout business logic instead of time.Now()
// and time.Local.
package timeutil

import (
	"sync"
	"time"
)

var (
	mu   sync.RWMutex
	loc  *time.Location
	name string
)

// LoadLocation loads the timezone by name (e.g., "Asia/Shanghai", "UTC").
// Must be called once at startup before any other function in this package.
func LoadLocation(tzName string) error {
	mu.Lock()
	defer mu.Unlock()

	l, err := time.LoadLocation(tzName)
	if err != nil {
		return err
	}
	loc = l
	name = tzName
	return nil
}

// Location returns the canonical application timezone.
// Falls back to time.Local if LoadLocation was never called.
func Location() *time.Location {
	mu.RLock()
	defer mu.RUnlock()
	if loc == nil {
		return time.Local
	}
	return loc
}

// LocationName returns the configured timezone name.
func LocationName() string {
	mu.RLock()
	defer mu.RUnlock()
	if name == "" {
		return "Local"
	}
	return name
}

// Now returns the current time in the application timezone.
// Falls back to time.Now() if LoadLocation was never called.
func Now() time.Time {
	return time.Now().In(Location())
}

// TodayStart returns the start of today (00:00:00) in the application timezone.
func TodayStart() time.Time {
	now := Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, Location())
}

// TodayEnd returns the end of today (24:00:00) in the application timezone.
func TodayEnd() time.Time {
	return TodayStart().Add(24 * time.Hour)
}

// FormatDate formats a time as "2006-01-02" in the application timezone.
func FormatDate(t time.Time) string {
	return t.In(Location()).Format("2006-01-02")
}

// ParseDate parses a "2006-01-02" string in the application timezone.
func ParseDate(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", s, Location())
}

// UnixMilli returns the UnixMilli of the given time (for timestamp fields).
func UnixMilli(t time.Time) int64 {
	return t.UnixMilli()
}

// InLoc converts a time to the application timezone.
func InLoc(t time.Time) time.Time {
	return t.In(Location())
}
