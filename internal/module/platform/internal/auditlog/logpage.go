package auditlog

import (
	"context"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// logFilter pages the system log.
type logFilter interface {
	FilterSystemLog(ctx context.Context, filter *log.FilterParams) ([]*log.SystemLog, int64, error)
}

// filterParams selects the rows of kind about objectID (any object when it
// is 0) on the requested page, dates and search text.
func filterParams(kind log.Type, objectID int64, req dto.FilterLogParams) *log.FilterParams {
	return &log.FilterParams{
		Page:      req.Page,
		Size:      req.Size,
		Type:      kind.Uint8(),
		ObjectID:  objectID,
		Data:      req.Date,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
		Search:    req.Search,
	}
}

// content is a system log row's decoded content.
type content[C any] interface {
	*C
	Unmarshal(data []byte) error
}

// logList pages the log of kind about objectID (any object when it is 0)
// under req, like logPage, but answers an empty list, never null.
func logList[C any, P content[C], V any](ctx context.Context, logs logFilter, name string, kind log.Type, objectID int64, req dto.FilterLogParams, view func(row *log.SystemLog, content *C) V) (int64, []V, error) {
	total, list, err := logPage[C, P](ctx, logs, name, filterParams(kind, objectID, req), view)
	if err != nil {
		return 0, nil, err
	}
	if list == nil {
		list = []V{}
	}
	return total, list, nil
}

// logPage pages the system log rows params selects, decodes each row's
// content into a C and shows it with view. A row whose content does not
// decode fails the page: a corrupt row is reported, not hidden. Without rows
// the list is nil. name names the log in the errors.
func logPage[C any, P content[C], V any](ctx context.Context, logs logFilter, name string, params *log.FilterParams, view func(row *log.SystemLog, content *C) V) (int64, []V, error) {
	rows, total, err := logs.FilterSystemLog(ctx, params)
	if err != nil {
		logger.WithContext(ctx).Errorw("[AuditLog] filter the "+name+" log failed", logger.Field("error", err.Error()))
		return 0, nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "filter the %s log", name)
	}
	var list []V
	for _, row := range rows {
		var decoded C
		if err := P(&decoded).Unmarshal([]byte(row.Content)); err != nil {
			logger.WithContext(ctx).Errorw("[AuditLog] corrupt "+name+" log", logger.Field("id", row.Id), logger.Field("error", err.Error()))
			return 0, nil, xerr.Wrapf(err, xerr.ERROR, "corrupt %s log %d", name, row.Id)
		}
		list = append(list, view(row, &decoded))
	}
	return total, list, nil
}
