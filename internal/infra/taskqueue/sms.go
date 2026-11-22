package taskqueue

import "github.com/perfect-panel/backend/pkg/requestmeta"

const (
	// ForthwithSendSms sends one text message (SendSmsPayload).
	ForthwithSendSms = "forthwith:sms:send"
)

type (
	// SendSmsPayload is one text message to send: Content is the
	// verification code the message carries.
	SendSmsPayload struct {
		requestmeta.Metadata
		Type          uint8  `json:"type"`
		Telephone     string `json:"telephone"`
		TelephoneArea string `json:"area"`
		Content       string `json:"content"`
	}
)
