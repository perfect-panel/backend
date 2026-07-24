package auditlog

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/traffic"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type FilterTrafficLogDetailsLogic struct {
	logger.Logger
	ctx  context.Context
	deps Deps
}

// NewFilterTrafficLogDetailsLogic Filter traffic log details
func newFilterTrafficLogDetailsLogic(ctx context.Context, deps Deps) *FilterTrafficLogDetailsLogic {
	return &FilterTrafficLogDetailsLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *FilterTrafficLogDetailsLogic) FilterTrafficLogDetails(req *dto.FilterTrafficLogDetailsRequest) (resp *dto.FilterTrafficLogDetailsResponse, err error) {
	var start, end time.Time
	if req.Date != "" {
		day, err := time.ParseInLocation("2006-01-02", req.Date, timeutil.Location())
		if err != nil {
			l.Errorw("[FilterTrafficLogDetails] Date Parse Error", logger.Field("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), " date parse error: %s", err.Error())
		}
		start = day
		end = day.Add(24 * time.Hour)
	} else {
		// query today
		now := timeutil.Now()
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		end = start.Add(24 * time.Hour)
	}
	data, total, err := l.deps.Traffic.QueryTrafficLogDetails(l.ctx, &traffic.TrafficLogDetailsFilter{
		ServerId:    req.ServerId,
		UserId:      req.UserId,
		SubscribeId: req.SubscribeId,
		Start:       start,
		End:         end,
		Page:        req.Page,
		Size:        req.Size,
	})
	if err != nil {
		l.Errorw("[FilterTrafficLogDetails] Query Database Error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), " database query error: %s", err.Error())
	}

	var logs []dto.TrafficLogDetails
	for _, v := range data {
		logs = append(logs, dto.TrafficLogDetails{
			Id:          v.Id,
			UserId:      v.UserId,
			ServerId:    v.ServerId,
			SubscribeId: v.SubscribeId,
			Download:    v.Download,
			Upload:      v.Upload,
			Timestamp:   v.Timestamp.UnixMilli(),
		})
	}

	return &dto.FilterTrafficLogDetailsResponse{
		List:  logs,
		Total: total,
	}, nil
}
