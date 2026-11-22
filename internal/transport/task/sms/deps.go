// Package sms holds the queue handler that sends verification-code text
// messages through the configured provider and audits every attempt.
package sms

import (
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/repository"
)

// Dependencies are the SMS task's: the message log (the platform kernel's)
// and the runtime settings.
type Dependencies struct {
	Logs   repository.LogRepo
	Mobile func() config.MobileConfig
}
