package auditlog

import (
	"context"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/internal/readmodel"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// FilterTrafficLogDetails pages the raw traffic log entries of a date range
// (end date inclusive), of a single legacy date, or of today when neither is
// given.
func (s *Service) FilterTrafficLogDetails(ctx context.Context, req *dto.FilterTrafficLogDetailsRequest) (*dto.FilterTrafficLogDetailsResponse, error) {
	var (
		start, end time.Time
		err        error
	)
	if req.StartDate != "" || req.EndDate != "" {
		if req.StartDate != "" {
			start, err = time.ParseInLocation(time.DateOnly, req.StartDate, timeutil.Location())
			if err != nil {
				return nil, xerr.Errorf(xerr.InvalidParams, "invalid start_date")
			}
		}
		if req.EndDate != "" {
			end, err = time.ParseInLocation(time.DateOnly, req.EndDate, timeutil.Location())
			if err != nil {
				return nil, xerr.Errorf(xerr.InvalidParams, "invalid end_date")
			}
			end = end.AddDate(0, 0, 1)
		}
	} else if req.Date != "" {
		day, err := time.ParseInLocation("2006-01-02", req.Date, timeutil.Location())
		if err != nil {
			logger.WithContext(ctx).Errorw("[FilterTrafficLogDetails] Date Parse Error", logger.Field("error", err.Error()))
			return nil, xerr.Wrapf(err, xerr.InvalidParams, " date parse error: %s", err.Error())
		}
		start = day
		end = day.AddDate(0, 0, 1)
	} else {
		now := timeutil.Now()
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		end = start.AddDate(0, 0, 1)
	}
	data, total, err := s.deps.Traffic.QueryTrafficLogDetails(ctx, &readmodel.TrafficLogDetailsFilter{
		ServerId:    req.ServerId,
		UserId:      req.UserId,
		SubscribeId: req.SubscribeId,
		Start:       start,
		End:         end,
		Page:        req.Page,
		Size:        req.Size,
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("[FilterTrafficLogDetails] Query Database Error", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, " database query error: %s", err.Error())
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
