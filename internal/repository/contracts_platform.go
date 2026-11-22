package repository

import "github.com/perfect-panel/backend/internal/repository/kernel"

// The shared kernel's contracts live in the kernel package, which depends
// on no module, so the platform module can use them without depending on the
// other modules' contracts in this package; they are re-exported here.
type (
	SystemRepo = kernel.SystemRepo
	LogRepo    = kernel.LogRepo
	TaskRepo   = kernel.TaskRepo
	InboxRepo  = kernel.InboxRepo
	OutboxRepo = kernel.OutboxRepo
)
