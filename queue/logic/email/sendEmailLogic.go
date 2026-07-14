package emailLogic

import (
	"bytes"
	"context"
	"encoding/json"
	"text/template"

	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/email"
	"github.com/perfect-panel/server/queue/types"
)

type SendEmailLogic struct {
	svcCtx *svc.ServiceContext
}

func emailLogContent(emailType string, content map[string]interface{}) map[string]interface{} {
	if emailType == types.EmailTypeVerify {
		return map[string]interface{}{"redacted": true}
	}
	return content
}

func NewSendEmailLogic(svcCtx *svc.ServiceContext) *SendEmailLogic {
	return &SendEmailLogic{
		svcCtx: svcCtx,
	}
}
func (l *SendEmailLogic) ProcessTask(ctx context.Context, task *asynq.Task) error {
	var payload types.SendEmailPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.WithContext(ctx).Error("[SendEmailLogic] Unmarshal payload failed",
			logger.Field("error", err.Error()),
		)
		return nil
	}
	messageLog := log.Message{
		Platform: l.svcCtx.Config.Email.Platform,
		To:       payload.Email,
		Subject:  payload.Subject,
		Content:  emailLogContent(payload.Type, payload.Content),
	}
	sender, err := email.NewSender(l.svcCtx.Config.Email.Platform, l.svcCtx.Config.Email.PlatformConfig, l.svcCtx.Config.Site.SiteName)
	if err != nil {
		logger.WithContext(ctx).Error("[SendEmailLogic] NewSender failed", logger.Field("error", err.Error()))
		return nil
	}
	var content string
	switch payload.Type {
	case types.EmailTypeVerify:
		tpl, _ := template.New("verify").Parse(l.svcCtx.Config.Email.VerifyEmailTemplate)
		var result bytes.Buffer

		payload.Content["Type"] = uint8(payload.Content["Type"].(float64))

		err = tpl.Execute(&result, payload.Content)
		if err != nil {
			logger.WithContext(ctx).Error("[SendEmailLogic] Execute template failed",
				logger.Field("error", err.Error()),
			)
			return nil
		}
		content = result.String()
	case types.EmailTypeMaintenance:
		tpl, _ := template.New("maintenance").Parse(l.svcCtx.Config.Email.MaintenanceEmailTemplate)
		var result bytes.Buffer
		err = tpl.Execute(&result, payload.Content)
		if err != nil {
			logger.WithContext(ctx).Error("[SendEmailLogic] Execute template failed",
				logger.Field("error", err.Error()),
				logger.Field("template", l.svcCtx.Config.Email.MaintenanceEmailTemplate),
			)
			return nil
		}
		content = result.String()
	case types.EmailTypeExpiration:
		tpl, _ := template.New("expiration").Parse(l.svcCtx.Config.Email.ExpirationEmailTemplate)
		var result bytes.Buffer
		err = tpl.Execute(&result, payload.Content)
		if err != nil {
			logger.WithContext(ctx).Error("[SendEmailLogic] Execute template failed",
				logger.Field("error", err.Error()),
				logger.Field("template", l.svcCtx.Config.Email.ExpirationEmailTemplate),
			)
			return nil
		}
		content = result.String()
	case types.EmailTypeTrafficExceed:
		tpl, _ := template.New("traffic_exceed").Parse(l.svcCtx.Config.Email.TrafficExceedEmailTemplate)
		var result bytes.Buffer
		err = tpl.Execute(&result, payload.Content)
		if err != nil {
			logger.WithContext(ctx).Error("[SendEmailLogic] Execute template failed",
				logger.Field("error", err.Error()),
				logger.Field("template", l.svcCtx.Config.Email.TrafficExceedEmailTemplate),
			)
			return nil
		}
		content = result.String()
	case types.EmailTypeCustom:
		if payload.Content == nil {
			logger.WithContext(ctx).Error("[SendEmailLogic] Custom email content is empty")
			return nil
		}
		if tpl, ok := payload.Content["content"].(string); !ok {
			logger.WithContext(ctx).Error("[SendEmailLogic] Custom email content is not a string")
			return nil
		} else {
			content = tpl
		}
	default:
		logger.WithContext(ctx).Error("[SendEmailLogic] Unsupported email type",
			logger.Field("type", payload.Type),
		)
		return nil
	}

	err = sender.Send([]string{payload.Email}, payload.Subject, content)
	if err != nil {
		logger.WithContext(ctx).Error("[SendEmailLogic] Send email failed", logger.Field("error", err.Error()))
		return nil
	}
	messageLog.Status = 1
	emailLog, err := messageLog.Marshal()
	if err != nil {
		logger.WithContext(ctx).Error("[SendEmailLogic] Marshal message log failed",
			logger.Field("error", err.Error()),
		)
		return nil
	}

	if err = l.svcCtx.Store.Log().Insert(ctx, &log.SystemLog{
		Type:     log.TypeEmailMessage.Uint8(),
		Date:     timeutil.Now().Format("2006-01-02"),
		ObjectID: 0,
		Content:  string(emailLog),
	}); err != nil {
		logger.WithContext(ctx).Error("[SendEmailLogic] Insert email log failed",
			logger.Field("error", err.Error()),
		)
		return nil
	}
	return nil
}
