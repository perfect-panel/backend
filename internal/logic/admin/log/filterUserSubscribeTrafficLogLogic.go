package log

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/log"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type FilterUserSubscribeTrafficLogLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewFilterUserSubscribeTrafficLogLogic Filter user subscribe traffic log
func NewFilterUserSubscribeTrafficLogLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FilterUserSubscribeTrafficLogLogic {
	return &FilterUserSubscribeTrafficLogLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *FilterUserSubscribeTrafficLogLogic) FilterUserSubscribeTrafficLog(req *dto.FilterSubscribeTrafficRequest) (resp *dto.FilterSubscribeTrafficResponse, err error) {
	if req.Size <= 0 {
		req.Size = 10
	}
	if req.Page <= 0 {
		req.Page = 1
	}

	today := timeutil.Now().Format("2006-01-02")
	var list []dto.UserSubscribeTrafficLog
	var total int64

	if req.Date == today || req.Date == "" {
		now := timeutil.Now()
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, timeutil.Location())
		end := start.Add(24 * time.Hour)

		userTraffic, err := l.svcCtx.Store.TrafficLog().QueryUserTrafficRanking(l.ctx, start, end)
		if err != nil {
			l.Errorw("[FilterUserSubscribeTrafficLog] Query Database Error", logger.Field("error", err.Error()))
			return nil, err
		}

		for _, v := range userTraffic {
			list = append(list, dto.UserSubscribeTrafficLog{
				UserId:      v.UserId,
				SubscribeId: v.SubscribeId,
				Upload:      v.Upload,
				Download:    v.Download,
				Total:       v.Total,
				Date:        today,
				Details:     true,
			})
		}
		todayTotal := len(list)

		startIdx := (req.Page - 1) * req.Size
		endIdx := startIdx + req.Size
		if startIdx < todayTotal {
			if endIdx > todayTotal {
				endIdx = todayTotal
			}
			pageData := list[startIdx:endIdx]
			return &dto.FilterSubscribeTrafficResponse{
				List:  pageData,
				Total: int64(todayTotal),
			}, nil
		}

		need := endIdx - todayTotal
		historyPage := (need + req.Size - 1) / req.Size // 算出需要的历史页数
		historyData, historyTotal, err := l.svcCtx.Store.Log().FilterSystemLog(l.ctx, &log.FilterParams{
			Page: historyPage,
			Size: need,
			Type: log.TypeSubscribeTraffic.Uint8(),
		})

		if err != nil {
			l.Errorw("[FilterUserSubscribeTrafficLog] Query Database Error", logger.Field("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "[FilterUserSubscribeTrafficLog] Query Database Error")
		}

		for _, datum := range historyData {
			var item log.UserTraffic
			err = item.Unmarshal([]byte(datum.Content))
			if err != nil {
				l.Errorw("[FilterUserSubscribeTrafficLog] Unmarshal Content Error", logger.Field("error", err.Error()))
				continue
			}
			list = append(list, dto.UserSubscribeTrafficLog{
				UserId:      item.UserId,
				SubscribeId: item.SubscribeId,
				Upload:      item.Upload,
				Download:    item.Download,
				Total:       item.Total,
				Date:        datum.Date,
				Details:     false,
			})
		}
		// 返回最终分页数据
		if endIdx > len(list) {
			endIdx = len(list)
		}
		pageData := list[startIdx:endIdx]

		return &dto.FilterSubscribeTrafficResponse{
			List:  pageData,
			Total: int64(todayTotal) + historyTotal,
		}, nil
	}
	var data []*log.SystemLog
	data, total, err = l.svcCtx.Store.Log().FilterSystemLog(l.ctx, &log.FilterParams{
		Page: req.Page,
		Size: req.Size,
		Type: log.TypeSubscribeTraffic.Uint8(),
		Data: req.Date,
	})
	if err != nil {
		l.Errorw("[FilterUserSubscribeTrafficLog] Query Database Error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "[FilterUserSubscribeTrafficLog] Query Database Error")
	}
	for _, datum := range data {
		var item log.UserTraffic
		err = item.Unmarshal([]byte(datum.Content))
		if err != nil {
			l.Errorw("[FilterUserSubscribeTrafficLog] Unmarshal Content Error", logger.Field("error", err.Error()))
			continue
		}
		list = append(list, dto.UserSubscribeTrafficLog{
			UserId:      item.UserId,
			SubscribeId: item.SubscribeId,
			Upload:      item.Upload,
			Download:    item.Download,
			Total:       item.Total,
			Date:        datum.Date,
			Details:     false,
		})
	}
	return &dto.FilterSubscribeTrafficResponse{
		List:  list,
		Total: total,
	}, nil
}
