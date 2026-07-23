package subscription

import (
	"context"
	"encoding/json"

	queue "github.com/perfect-panel/server/queue/types"

	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
)

type CheckSubscriptionLogic struct {
	svc *svc.ServiceContext
}

func NewCheckSubscriptionLogic(svc *svc.ServiceContext) *CheckSubscriptionLogic {
	return &CheckSubscriptionLogic{
		svc: svc,
	}
}

func (l *CheckSubscriptionLogic) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	logger.Infof("[CheckSubscription] Start check subscription: %s", timeutil.Now().Format("2006-01-02 15:04:05"))
	// Check subscription traffic
	err := l.svc.Store.InTx(ctx, func(store repository.Store) error {
		list, err := store.UserSubscription().FindTrafficExceededSubscribes(ctx)
		if err != nil {
			logger.Errorw("[Check Subscription Traffic] Query subscribe failed", logger.Field("error", err.Error()))
			return err
		}
		var ids []int64
		for _, item := range list {
			ids = append(ids, item.Id)
		}
		if len(ids) > 0 {
			if err = store.UserSubscription().MarkSubscribesFinished(ctx, ids, 2, timeutil.Now()); err != nil {
				logger.Errorw("[Check Subscription Traffic] Update subscribe status failed", logger.Field("error", err.Error()))
				return nil
			}
			err = l.sendTrafficNotify(ctx, ids)
			if err != nil {
				logger.Errorw("[Check Subscription Traffic] Send email failed", logger.Field("error", err.Error()))
				return nil
			}

			if len(list) > 0 {
				if err = store.UserCache().ClearSubscribeCache(ctx, list...); err != nil {
					logger.Errorw("[Check Subscription Traffic] Clear subscribe cache failed", logger.Field("error", err.Error()))
					return err
				}
			}
			l.clearServerCache(ctx, list...)
			logger.Infow("[Check Subscription Traffic] Update subscribe status", logger.Field("user_ids", ids), logger.Field("count", int64(len(ids))))

		} else {
			logger.Info("[Check Subscription Traffic] No subscribe need to update")
		}

		return nil
	})
	if err != nil {
		logger.Error("[CheckSubscription] Transaction failed", logger.Field("error", err.Error()))
	}
	// Check subscription expire
	err = l.svc.Store.InTx(ctx, func(store repository.Store) error {
		list, err := store.UserSubscription().FindExpiredSubscribes(ctx, timeutil.Now())
		if err != nil {
			logger.Error("[Check Subscription] Find subscribe failed", logger.Field("error", err.Error()))
			return err
		}
		var ids []int64
		for _, item := range list {
			ids = append(ids, item.Id)
		}
		if len(ids) > 0 {
			if err = store.UserSubscription().MarkSubscribesFinished(ctx, ids, 3, timeutil.Now()); err != nil {
				logger.Error("[Check Subscription Expire] Update subscribe status failed", logger.Field("error", err.Error()))
				return err
			}
			err = l.sendExpiredNotify(ctx, ids)
			if err != nil {
				logger.Error("[Check Subscription Expire] Send email failed", logger.Field("error", err.Error()))
				return nil
			}
			if err = store.UserCache().ClearSubscribeCache(ctx, list...); err != nil {
				logger.Errorw("[Check Subscription Traffic] Clear subscribe cache failed", logger.Field("error", err.Error()))
				return err
			}
			l.clearServerCache(ctx, list...)

			logger.Info("[Check Subscription Expire] Update subscribe status", logger.Field("user_ids", ids), logger.Field("count", int64(len(ids))))
		} else {
			logger.Info("[Check Subscription Expire] No subscribe need to update")
		}
		return nil
	})
	if err != nil {
		logger.Info("[CheckSubscription] Transaction failed", logger.Field("error", err.Error()))
	}
	return nil
}

func (l *CheckSubscriptionLogic) sendExpiredNotify(ctx context.Context, subs []int64) error {
	for _, id := range subs {
		sub, err := l.svc.Store.UserSubscription().FindOneUserSubscribe(ctx, id)
		if err != nil {
			logger.Errorw("[CheckSubscription] FindOneUserSubscribe failed", logger.Field("error", err.Error()))
			continue
		}
		method, err := l.svc.Store.UserAuth().FindUserAuthMethodByUserId(ctx, "email", sub.UserId)
		if err != nil {
			logger.Errorw("[CheckSubscription] FindUserAuthMethodByUserId failed", logger.Field("error", err.Error()), logger.Field("user_id", sub.UserId))
			continue
		}
		var taskPayload queue.SendEmailPayload
		taskPayload.Type = queue.EmailTypeExpiration
		taskPayload.Email = method.AuthIdentifier
		taskPayload.Subject = "Subscription Expired"
		taskPayload.Content = map[string]interface{}{
			"SiteLogo":   l.svc.Config.Site.SiteLogo,
			"SiteName":   l.svc.Config.Site.SiteName,
			"ExpireDate": sub.ExpireTime.Format("2006-01-02 15:04:05"),
		}
		payloadBuy, err := json.Marshal(taskPayload)
		if err != nil {
			logger.Errorw("[CheckSubscription] Marshal payload failed", logger.Field("error", err.Error()))
			continue
		}
		task := asynq.NewTask(queue.ForthwithSendEmail, payloadBuy, asynq.MaxRetry(3))
		taskInfo, err := l.svc.Queue.Enqueue(task)
		if err != nil {
			logger.Errorw("[CheckSubscription] Enqueue task failed", logger.Field("error", err.Error()), logger.Field("payload", string(payloadBuy)))
			continue
		}
		logger.Infow("[CheckSubscription] Send email success",
			logger.Field("taskID", taskInfo.ID), logger.Field("User", sub.UserId),
			logger.Field("Email", method.AuthIdentifier),
		)
	}
	return nil
}

func (l *CheckSubscriptionLogic) sendTrafficNotify(ctx context.Context, subs []int64) error {
	for _, id := range subs {
		sub, err := l.svc.Store.UserSubscription().FindOneUserSubscribe(ctx, id)
		if err != nil {
			logger.Errorw("[CheckSubscription] FindOneUserSubscribe failed", logger.Field("error", err.Error()))
			continue
		}
		method, err := l.svc.Store.UserAuth().FindUserAuthMethodByUserId(ctx, "email", sub.UserId)
		if err != nil {
			logger.Errorw("[CheckSubscription] FindUserAuthMethodByUserId failed", logger.Field("error", err.Error()), logger.Field("user_id", sub.UserId))
			continue
		}
		var taskPayload queue.SendEmailPayload
		taskPayload.Type = queue.EmailTypeTrafficExceed
		taskPayload.Email = method.AuthIdentifier
		taskPayload.Subject = "Subscription Traffic Exceed"
		taskPayload.Content = map[string]interface{}{
			"SiteLogo": l.svc.Config.Site.SiteLogo,
			"SiteName": l.svc.Config.Site.SiteName,
		}
		payloadBuy, err := json.Marshal(taskPayload)
		if err != nil {
			logger.Errorw("[CheckSubscription] Marshal payload failed", logger.Field("error", err.Error()))
			continue
		}
		task := asynq.NewTask(queue.ForthwithSendEmail, payloadBuy, asynq.MaxRetry(3))
		taskInfo, err := l.svc.Queue.Enqueue(task)
		if err != nil {
			logger.Errorw("[CheckSubscription] Enqueue task failed", logger.Field("error", err.Error()), logger.Field("payload", string(payloadBuy)))
			continue
		}
		logger.Infow("[CheckSubscription] Send email success",
			logger.Field("taskID", taskInfo.ID), logger.Field("User", sub.UserId),
			logger.Field("Email", method.AuthIdentifier),
		)
	}
	return nil
}

func (l *CheckSubscriptionLogic) clearServerCache(ctx context.Context, userSubs ...*user.Subscribe) {
	subs := make(map[int64]bool)
	for _, sub := range userSubs {
		if _, ok := subs[sub.SubscribeId]; !ok {
			subs[sub.SubscribeId] = true
		}
	}

	for sub, _ := range subs {
		if err := l.svc.Store.Subscribe().ClearCache(ctx, sub); err != nil {
			logger.Errorw("[CheckSubscription] ClearCache failed", logger.Field("error", err.Error()), logger.Field("subscribe_id", sub))
		}
	}
}
