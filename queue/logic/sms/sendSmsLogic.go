package smslogic

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/sms"
	"github.com/perfect-panel/server/queue/types"
)

type SmsSendCount struct {
	Count    int   `json:"count"`
	CreateAt int64 `json:"create_at"`
}

type SendSmsLogic struct {
	svcCtx *svc.ServiceContext
}

func NewSendSmsLogic(svcCtx *svc.ServiceContext) *SendSmsLogic {
	return &SendSmsLogic{
		svcCtx: svcCtx,
	}
}
func (l *SendSmsLogic) ProcessTask(ctx context.Context, task *asynq.Task) error {
	var payload types.SendSmsPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.WithContext(ctx).Error("[SendSmsLogic] Unmarshal payload failed",
			logger.Field("error", err.Error()),
			logger.Field("payload", task.Payload()),
		)
		return nil
	}
	client, err := sms.NewSender(l.svcCtx.Config.Mobile.Platform, l.svcCtx.Config.Mobile.PlatformConfig)
	if err != nil {
		logger.WithContext(ctx).Error("[SendSmsLogic] New send sms client failed", logger.Field("error", err.Error()), logger.Field("payload", payload))
		return err
	}
	createSms := &log.Message{
		Platform: l.svcCtx.Config.Mobile.Platform,
		To:       fmt.Sprintf("+%s%s", payload.TelephoneArea, payload.Telephone),
		Subject:  constant.ParseVerifyType(payload.Type).String(),
		Content: map[string]interface{}{
			"content": client.GetSendCodeContent(payload.Content),
		},
	}
	err = client.SendCode(payload.TelephoneArea, payload.Telephone, payload.Content)

	if err != nil {
		logger.WithContext(ctx).Error("[SendSmsLogic] Send sms failed", logger.Field("error", err.Error()), logger.Field("payload", payload))
		if l.svcCtx.Config.Model != constant.DevMode {
			createSms.Status = 2
		} else {
			return nil
		}
	}
	createSms.Status = 1
	logger.WithContext(ctx).Info("[SendSmsLogic] Send sms", logger.Field("telephone", payload.Telephone), logger.Field("content", createSms.Content))

	content, _ := createSms.Marshal()
	err = l.svcCtx.Store.Log().Insert(ctx, &log.SystemLog{
		Type:     log.TypeMobileMessage.Uint8(),
		Date:     timeutil.Now().Format("2006-01-02"),
		ObjectID: 0,
		Content:  string(content),
	})
	if err != nil {
		logger.WithContext(ctx).Error("[SendSmsLogic] Send sms failed", logger.Field("error", err.Error()), logger.Field("payload", payload))
		return nil
	}
	return nil
}
