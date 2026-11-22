package auditlog

import (
	"context"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/repository/kernel"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// FilterUserSubscribeTrafficLog pages the subscriptions' daily traffic:
// today's live ranking first, then the archived days, narrowed to one user
// or one subscription when asked. The retention settings decide which
// archived days still have their details.
func (s *Service) FilterUserSubscribeTrafficLog(ctx context.Context, req *dto.FilterSubscribeTrafficRequest) (*dto.FilterSubscribeTrafficResponse, error) {
	now := timeutil.Now()
	today := now.Format(time.DateOnly)
	startDate, endDate := req.StartDate, req.EndDate
	if startDate == "" && endDate == "" && req.Date != "" {
		startDate, endDate = req.Date, req.Date
	}
	list := make([]dto.UserSubscribeTrafficLog, 0)
	if (startDate == "" || startDate <= today) && (endDate == "" || endDate >= today) {
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, timeutil.Location())
		traffic, err := s.deps.Traffic.QueryUserTrafficRanking(ctx, start, start.AddDate(0, 0, 1))
		if err != nil {
			return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "today traffic query error: %s", err)
		}
		for _, row := range traffic {
			if (req.UserId != 0 && row.UserId != req.UserId) || (req.UserSubscribeId != 0 && row.SubscribeId != req.UserSubscribeId) {
				continue
			}
			list = append(list, dto.UserSubscribeTrafficLog{UserId: row.UserId, SubscribeId: row.SubscribeId, Upload: row.Upload, Download: row.Download, Total: row.Total, Date: today, Details: true})
		}
	}
	// Today's ranking replaces persisted rows for today, avoiding duplicates.
	yesterday := now.AddDate(0, 0, -1).Format(time.DateOnly)
	if endDate == "" || endDate > yesterday {
		endDate = yesterday
	}
	page, size := kernel.NormalizePage(req.Page, req.Size)
	offset := (page - 1) * size
	todayTotal := len(list)
	historyOffset := max(0, offset-todayTotal)
	// An archived row's object is its subscription; its user is in the
	// content.
	params := &log.FilterParams{Page: historyOffset/size + 1, Size: size, Type: log.TypeSubscribeTraffic.Uint8(), StartDate: startDate, EndDate: endDate, Search: req.Search, ObjectID: req.UserSubscribeId}
	if req.UserId != 0 {
		params.ContentInt64 = map[string]int64{"user_id": req.UserId}
	}
	history, historyTotal, err := s.deps.Logs.FilterSystemLog(ctx, params)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "history query error: %s", err)
	}
	list = list[min(offset, todayTotal):min(offset+size, todayTotal)]
	// A mixed live/history page can begin partway through a database page.
	skip := historyOffset % size
	history = history[min(skip, len(history)):]
	if len(list)+len(history) < size && int64(historyOffset+len(history)) < historyTotal {
		params.Page++
		params.SkipCount = true
		next, _, err := s.deps.Logs.FilterSystemLog(ctx, params)
		if err != nil {
			return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "history query error: %s", err)
		}
		history = append(history, next...)
	}
	for _, item := range history {
		if len(list) == size {
			break
		}
		var content log.UserTraffic
		if err := content.Unmarshal([]byte(item.Content)); err != nil {
			return nil, xerr.Wrapf(err, xerr.ERROR, "corrupt subscription traffic log %d: %v", item.Id, err)
		}
		hasDetails := true
		if autoClear, clearDays := s.deps.logRetention(); autoClear {
			day, err := time.ParseInLocation(time.DateOnly, item.Date, timeutil.Location())
			hasDetails = err == nil && !day.Before(now.AddDate(0, 0, -int(clearDays)))
		}
		list = append(list, dto.UserSubscribeTrafficLog{UserId: content.UserId, SubscribeId: content.SubscribeId, Upload: content.Upload, Download: content.Download, Total: content.Total, Date: item.Date, Details: hasDetails})
	}
	return &dto.FilterSubscribeTrafficResponse{List: list, Total: int64(todayTotal) + historyTotal}, nil
}
