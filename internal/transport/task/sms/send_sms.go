package sms

import (
	"context"
	"encoding/json"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/infra/sms"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	"github.com/perfect-panel/backend/internal/module/identity/entity/auth"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// SendSmsHandler sends the verification code a task carries through the
// configured provider. The attempt is written to the message log before the
// provider call and finalized after it; once the provider was called the task
// is not retried, even on an error, because a failed call may still have
// delivered the code.
type SendSmsHandler struct {
	deps Dependencies
	// senders keeps the provider client between messages; it is rebuilt
	// when the mobile configuration changes.
	senders sms.Senders
}

// newSMSMessageLog is the message-log record of one send. The recipient and
// the code stay out of it; the request metadata of the producing request is
// kept for risk review.
func newSMSMessageLog(platform string, messageType uint8, metadata ...requestmeta.Metadata) *log.Message {
	var requestMetadata requestmeta.Metadata
	if len(metadata) > 0 {
		requestMetadata = metadata[0]
	}
	return &log.Message{
		Metadata: requestMetadata,
		Platform: platform,
		To:       logger.RedactedValue,
		Subject:  auth.ParseVerifyType(messageType).String(),
		Content:  map[string]any{"redacted": true},
	}
}

// NewSendSmsHandler builds the handler over the message log and the runtime
// mobile settings.
func NewSendSmsHandler(deps Dependencies) *SendSmsHandler {
	return &SendSmsHandler{
		deps: deps,
	}
}

func (h *SendSmsHandler) ProcessTask(ctx context.Context, task *asynq.Task) error {
	var payload taskqueue.SendSmsPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.WithContext(ctx).Error("[SendSms] Unmarshal payload failed",
			logger.Field("error", err.Error()),
			logger.Field("payload", task.Payload()),
		)
		return nil
	}
	ctx = requestmeta.With(ctx, payload.Metadata)
	ctx = logger.ContextWithRequestMetadata(ctx, payload.Metadata)
	client, err := h.senders.Get(h.deps.Mobile().Platform, h.deps.Mobile().PlatformConfig)
	if err != nil {
		logger.WithContext(ctx).Error("[SendSms] New send sms client failed", logger.Field("error", err.Error()), logger.Field("payload", payload))
		return err
	}
	createSms := newSMSMessageLog(h.deps.Mobile().Platform, payload.Type, payload.Metadata)
	content, marshalErr := createSms.Marshal()
	if marshalErr != nil {
		return marshalErr
	}
	audit := &log.SystemLog{
		Type:     log.TypeMobileMessage.Uint8(),
		Date:     timeutil.Now().Format("2006-01-02"),
		ObjectID: 0,
		Content:  string(content),
	}
	// Record the attempt before contacting the provider so a storage failure
	// cannot produce a successful but unaudited SMS delivery.
	if err = h.deps.Logs.Insert(ctx, audit); err != nil {
		logger.WithContext(ctx).Error("[SendSms] Insert sms log failed", logger.Field("error", err.Error()))
		return err
	}
	err = client.Send(ctx, sms.CodeMessage(payload.TelephoneArea, payload.Telephone, payload.Content))

	if err != nil {
		logger.WithContext(ctx).Error("[SendSms] Send sms failed", logger.Field("error", err.Error()), logger.Field("payload", payload))
		createSms.Status = 2
	} else {
		createSms.Status = 1
	}
	logger.WithContext(ctx).Info("[SendSms] Send sms", logger.Field("telephone", payload.Telephone), logger.Field("content", createSms.Content))

	content, marshalErr = createSms.Marshal()
	if marshalErr != nil {
		// The message went out: retrying the task would send it again, so the
		// audit row keeps the attempt's content.
		logger.WithContext(ctx).Error("[SendSms] Encode sms log failed", logger.Field("error", marshalErr.Error()), logger.Field("log_id", audit.Id))
		return nil
	}
	audit.Content = string(content)
	if updateErr := h.deps.Logs.Update(ctx, audit); updateErr != nil {
		logger.WithContext(ctx).Error("[SendSms] Finalize sms log failed", logger.Field("error", updateErr.Error()), logger.Field("log_id", audit.Id))
	}
	return nil
}
