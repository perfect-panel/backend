package email

import (
	"bytes"
	"context"
	"encoding/json"
	"text/template"

	"github.com/pars-aria-labs/asynq"
	"github.com/perfect-panel/backend/internal/infra/mail"
	"github.com/perfect-panel/backend/internal/infra/taskqueue"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

// SendEmailHandler sends one notification email. The body and subject come
// from the operator-configured templates of the notification type. The
// attempt is written to the message log before the provider call and
// finalized after it; once the provider was called the task is not retried,
// even on an error, because a failed call may still have delivered the mail.
type SendEmailHandler struct {
	deps Dependencies
	// senders keeps the provider client between messages; it is rebuilt
	// when the email configuration changes.
	senders mail.Senders
}

// emailLogContent is the message-log content of a notification: its type
// only, since the template data may hold names, addresses or one-time codes.
func emailLogContent(emailType string, _ map[string]any) map[string]any {
	return map[string]any{"redacted": true, "email_type": emailType}
}

func renderEmailTemplate(name, text string, data map[string]any) (string, error) {
	tpl, err := template.New(name).Parse(text)
	if err != nil {
		return "", err
	}
	var result bytes.Buffer
	if err := tpl.Execute(&result, data); err != nil {
		return "", err
	}
	return result.String(), nil
}

// resolveSubject prefers the operator-configured subject over the fallback
// literal the producer queued. The configured subject renders with the same
// data as the body; if it fails to render it is still sent as raw text,
// because a localized subject with a template typo beats silently reverting
// to English.
func resolveSubject(ctx context.Context, configured, fallback string, data map[string]any) string {
	if configured == "" {
		return fallback
	}
	rendered, err := renderEmailTemplate("subject", configured, data)
	if err != nil {
		logger.WithContext(ctx).Error("[SendEmail] Execute subject template failed",
			logger.Field("error", err.Error()),
			logger.Field("subject", configured),
		)
		return configured
	}
	return rendered
}

// NewSendEmailHandler builds the handler over the message log and the runtime
// email settings.
func NewSendEmailHandler(deps Dependencies) *SendEmailHandler {
	return &SendEmailHandler{
		deps: deps,
	}
}

func (h *SendEmailHandler) ProcessTask(ctx context.Context, task *asynq.Task) error {
	var payload taskqueue.SendEmailPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.WithContext(ctx).Error("[SendEmail] Unmarshal payload failed",
			logger.Field("error", err.Error()),
		)
		return nil
	}
	ctx = requestmeta.With(ctx, payload.Metadata)
	ctx = logger.ContextWithRequestMetadata(ctx, payload.Metadata)
	emailConfig := h.deps.Email()
	sender, err := h.senders.Get(emailConfig.Platform, emailConfig.PlatformConfig, h.deps.SiteName())
	if err != nil {
		logger.WithContext(ctx).Error("[SendEmail] NewSender failed", logger.Field("error", err.Error()))
		return nil
	}
	// The operator-configured subject of a typed notification wins over the
	// literal queued by the producer; it renders with the same data as the
	// body so subjects can interpolate {{.SiteName}} and friends.
	var content, bodyTemplate, subjectTemplate string
	switch payload.Type {
	case taskqueue.EmailTypeVerify:
		// The template compares the code's purpose with integer constants;
		// JSON decoded it as a number. A payload without it cannot render
		// and is dropped, like any other malformed payload.
		purpose, ok := payload.Content["Type"].(float64)
		if !ok {
			logger.WithContext(ctx).Error("[SendEmail] Verification email without its code purpose",
				logger.Field("type", payload.Content["Type"]),
			)
			return nil
		}
		payload.Content["Type"] = uint8(purpose)
		bodyTemplate = emailConfig.VerifyEmailTemplate
		subjectTemplate = emailConfig.VerifyEmailSubject
	case taskqueue.EmailTypeMaintenance:
		bodyTemplate = emailConfig.MaintenanceEmailTemplate
		subjectTemplate = emailConfig.MaintenanceEmailSubject
	case taskqueue.EmailTypeExpiration:
		bodyTemplate = emailConfig.ExpirationEmailTemplate
		subjectTemplate = emailConfig.ExpirationEmailSubject
	case taskqueue.EmailTypeTrafficExceed:
		bodyTemplate = emailConfig.TrafficExceedEmailTemplate
		subjectTemplate = emailConfig.TrafficExceedEmailSubject
	case taskqueue.EmailTypeCustom:
		if payload.Content == nil {
			logger.WithContext(ctx).Error("[SendEmail] Custom email content is empty")
			return nil
		}
		if tpl, ok := payload.Content["content"].(string); !ok {
			logger.WithContext(ctx).Error("[SendEmail] Custom email content is not a string")
			return nil
		} else {
			content = tpl
		}
	default:
		logger.WithContext(ctx).Error("[SendEmail] Unsupported email type",
			logger.Field("type", payload.Type),
		)
		return nil
	}
	if bodyTemplate != "" {
		content, err = renderEmailTemplate(payload.Type, bodyTemplate, payload.Content)
		if err != nil {
			logger.WithContext(ctx).Error("[SendEmail] Execute template failed",
				logger.Field("error", err.Error()),
				logger.Field("template", bodyTemplate),
			)
			return nil
		}
	}
	subject := resolveSubject(ctx, subjectTemplate, payload.Subject, payload.Content)
	messageLog := log.Message{
		Metadata: payload.Metadata,
		Platform: emailConfig.Platform,
		To:       logger.RedactedValue,
		// Subjects are operator-controlled templates and may interpolate names,
		// addresses or one-time credentials. Keep only the notification type in
		// the audit record; the actual subject is used solely for delivery.
		Subject: payload.Type,
		Content: emailLogContent(payload.Type, payload.Content),
		Status:  0, // attempted; finalized after the provider call
	}
	emailLog, err := messageLog.Marshal()
	if err != nil {
		logger.WithContext(ctx).Error("[SendEmail] Marshal message log failed", logger.Field("error", err.Error()))
		return err
	}
	audit := &log.SystemLog{
		Type:     log.TypeEmailMessage.Uint8(),
		Date:     timeutil.Now().Format("2006-01-02"),
		ObjectID: 0,
		Content:  string(emailLog),
	}
	// Persist the attempt before contacting the provider. A database failure is
	// safe to retry here because no email has been sent yet.
	if err = h.deps.Logs.Insert(ctx, audit); err != nil {
		logger.WithContext(ctx).Error("[SendEmail] Insert email log failed", logger.Field("error", err.Error()))
		return err
	}

	err = sender.SendContext(ctx, []string{payload.Email}, subject, content)
	if err != nil {
		messageLog.Status = 2
		logger.WithContext(ctx).Error("[SendEmail] Send email failed", logger.Field("error", err.Error()))
	} else {
		messageLog.Status = 1
	}
	emailLog, marshalErr := messageLog.Marshal()
	if marshalErr != nil {
		logger.WithContext(ctx).Error("[SendEmail] Marshal finalized message log failed", logger.Field("error", marshalErr.Error()))
		return nil
	}
	audit.Content = string(emailLog)
	if updateErr := h.deps.Logs.Update(ctx, audit); updateErr != nil {
		// The pre-created attempt row remains, so the delivery is never missing
		// from the audit trail. Retrying after a provider call could duplicate the
		// email, therefore record the update failure without failing the task.
		logger.WithContext(ctx).Error("[SendEmail] Finalize email log failed", logger.Field("error", updateErr.Error()), logger.Field("log_id", audit.Id))
	}
	return nil
}
