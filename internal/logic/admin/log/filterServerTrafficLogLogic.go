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

type FilterServerTrafficLogLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewFilterServerTrafficLogLogic Filter server traffic log
func NewFilterServerTrafficLogLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FilterServerTrafficLogLogic {
	return &FilterServerTrafficLogLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}
func (l *FilterServerTrafficLogLogic) FilterServerTrafficLog(req *dto.FilterServerTrafficLogRequest) (resp *dto.FilterServerTrafficLogResponse, err error) {
	today := timeutil.Now().Format("2006-01-02")
	var list []dto.ServerTrafficLog
	var total int64

	if req.Date == today || req.Date == "" {
		now := timeutil.Now()
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, timeutil.Location())
		end := start.Add(24 * time.Hour)

		serverTraffic, err := l.svcCtx.Store.TrafficLog().QueryServerTrafficRanking(l.ctx, start, end)
		if err != nil {
			l.Errorw("[FilterServerTrafficLog] Query Database Error", logger.Field("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "today traffic query error: %s", err.Error())
		}

		for _, v := range serverTraffic {
			list = append(list, dto.ServerTrafficLog{
				ServerId: v.ServerId,
				Upload:   v.Upload,
				Download: v.Download,
				Total:    v.Total,
				Date:     today,
				Details:  true,
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
			return &dto.FilterServerTrafficLogResponse{
				List:  pageData,
				Total: int64(todayTotal),
			}, nil
		}

		need := endIdx - todayTotal
		historyPage := (need + req.Size - 1) / req.Size // 算出需要的历史页数
		historyData, historyTotal, err := l.svcCtx.Store.Log().FilterSystemLog(l.ctx, &log.FilterParams{
			Page: historyPage,
			Size: need,
			Type: log.TypeServerTraffic.Uint8(),
		})
		if err != nil {
			l.Errorw("[FilterServerTrafficLog] Query History Error", logger.Field("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "history query error: %s", err.Error())
		}

		for _, item := range historyData {
			var content log.ServerTraffic
			if err = content.Unmarshal([]byte(item.Content)); err != nil {
				l.Errorw("[FilterServerTrafficLog] Unmarshal Error", logger.Field("error", err.Error()), logger.Field("content", item.Content))
				continue
			}

			hasDetails := true
			if l.svcCtx.Config.Log.AutoClear {
				last := now.AddDate(0, 0, int(-l.svcCtx.Config.Log.ClearDays))
				dataTime, err := time.Parse(time.DateOnly, item.Date)
				if err != nil {
					l.Errorw("[FilterServerTrafficLog] Parse Date Error", logger.Field("error", err.Error()), logger.Field("date", item.Date))
				} else {
					if dataTime.Before(last) {
						hasDetails = false
					} else {
						hasDetails = true
					}
				}
			}

			list = append(list, dto.ServerTrafficLog{
				ServerId: item.ObjectID,
				Upload:   content.Upload,
				Download: content.Download,
				Total:    content.Total,
				Date:     item.Date,
				Details:  hasDetails,
			})
		}

		// 返回最终分页数据
		if endIdx > len(list) {
			endIdx = len(list)
		}
		pageData := list[startIdx:endIdx]

		return &dto.FilterServerTrafficLogResponse{
			List:  pageData,
			Total: int64(todayTotal) + historyTotal,
		}, nil
	}

	data, total, err := l.svcCtx.Store.Log().FilterSystemLog(l.ctx, &log.FilterParams{
		Page: req.Page,
		Size: req.Size,
		Type: log.TypeServerTraffic.Uint8(),
	})
	if err != nil {
		l.Errorw("[FilterServerTrafficLog] Query Database Error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "history query error: %s", err.Error())
	}

	for _, item := range data {
		var content log.ServerTraffic
		if err = content.Unmarshal([]byte(item.Content)); err != nil {
			l.Errorw("[FilterServerTrafficLog] Unmarshal Error", logger.Field("error", err.Error()), logger.Field("content", item.Content))
			continue
		}
		list = append(list, dto.ServerTrafficLog{
			ServerId: item.ObjectID,
			Upload:   content.Upload,
			Download: content.Download,
			Total:    content.Total,
			Date:     item.Date,
			Details:  false,
		})
	}

	return &dto.FilterServerTrafficLogResponse{
		List:  list,
		Total: total,
	}, nil
}
