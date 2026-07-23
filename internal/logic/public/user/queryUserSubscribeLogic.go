package user

import (
	"context"
	"encoding/json"
	"time"

	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/timeutil"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type QueryUserSubscribeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Query User Subscribe
func NewQueryUserSubscribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryUserSubscribeLogic {
	return &QueryUserSubscribeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryUserSubscribeLogic) QueryUserSubscribe() (resp *dto.QueryUserSubscribeListResponse, err error) {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	data, err := l.svcCtx.Store.UserSubscription().QueryUserSubscribe(l.ctx, u.Id, 0, 1, 2, 3)
	if err != nil {
		l.Errorw("[QueryUserSubscribeLogic] Query User Subscribe Error:", logger.Field("err", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Query User Subscribe Error")
	}

	resp = &dto.QueryUserSubscribeListResponse{
		List:  make([]dto.UserSubscribe, 0),
		Total: int64(len(data)),
	}

	for _, item := range data {
		var sub dto.UserSubscribe
		tool.DeepCopy(&sub, item)

		// 解析Discount字段 避免在续订时只能续订一个月
		if item.Subscribe != nil && item.Subscribe.Discount != "" {
			var discounts []dto.SubscribeDiscount
			if err := json.Unmarshal([]byte(item.Subscribe.Discount), &discounts); err == nil {
				sub.Subscribe.Discount = discounts
			}
		}

		short, _ := tool.FixedUniqueString(item.Token, 8, "")
		sub.Short = short
		sub.ResetTime = calculateNextResetTime(&sub)
		resp.List = append(resp.List, sub)
	}
	return
}

// 计算下次重置时间
func calculateNextResetTime(sub *dto.UserSubscribe) int64 {
	now := timeutil.Now()
	return calculateNextResetTimeAt(sub, now)
}

func calculateNextResetTimeAt(sub *dto.UserSubscribe, now time.Time) int64 {
	switch sub.Subscribe.ResetCycle {
	case 0:
		return 0
	case 1:
		return time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location()).UnixMilli()
	case 2:
		startTime := resetBaseTime(sub)
		target := validResetDate(now.Year(), now.Month(), startTime.Day(), now.Location())
		if !target.After(now) {
			target = validResetDate(now.Year(), now.Month()+1, startTime.Day(), now.Location())
		}
		return target.UnixMilli()
	case 3:
		startTime := resetBaseTime(sub)
		targetTime := validResetDate(now.Year(), startTime.Month(), startTime.Day(), now.Location())
		if !targetTime.After(now) {
			targetTime = validResetDate(now.Year()+1, startTime.Month(), startTime.Day(), now.Location())
		}
		return targetTime.UnixMilli()
	default:
		return 0
	}
}

func resetBaseTime(sub *dto.UserSubscribe) time.Time {
	if sub.StartTime > 0 {
		return time.UnixMilli(sub.StartTime)
	}
	return time.UnixMilli(sub.ExpireTime)
}

func validResetDate(year int, month time.Month, day int, loc *time.Location) time.Time {
	firstOfMonth := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	lastDay := firstOfMonth.AddDate(0, 1, -1).Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}
