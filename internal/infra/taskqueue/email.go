package taskqueue

import "github.com/perfect-panel/backend/pkg/requestmeta"

const (
	// ForthwithSendEmail sends one email (SendEmailPayload).
	ForthwithSendEmail = "forthwith:email:send"
)

// The email types of SendEmailPayload: each selects the configured subject
// and template, except EmailTypeCustom, whose payload carries the text.
const (
	EmailTypeVerify        = "verify"
	EmailTypeMaintenance   = "maintenance"
	EmailTypeExpiration    = "expiration"
	EmailTypeTrafficExceed = "traffic_exceed"
	EmailTypeCustom        = "custom"
)

type (
	// SendEmailPayload is one email to send; Content fills the template of
	// its Type.
	SendEmailPayload struct {
		requestmeta.Metadata
		Type    string         `json:"type"`
		Email   string         `json:"to"`
		Subject string         `json:"subject"`
		Content map[string]any `json:"content"`
	}
)
