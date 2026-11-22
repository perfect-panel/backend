package tool

import (
	"context"
	"encoding/json"
	"sort"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// systemLogTail is how many of the latest system log entries the tail
// shows.
const systemLogTail = 50

// GetSystemLog returns the latest system log entries, newest first. A line
// that is not a JSON entry is skipped, but a tail in which no line is one is
// an error: the log is not written in the format the tail shows.
func (s *Service) GetSystemLog(ctx context.Context) (*dto.LogResponse, error) {
	log := logger.WithContext(ctx)
	lines, err := logger.ReadLastNLogLines(s.deps.LogPath, systemLogTail)
	if err != nil {
		log.Error(err)
		return nil, xerr.Wrapf(err, xerr.ERROR, "get system log error: %v", err.Error())
	}
	list := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			log.Error(err)
			continue
		}
		list = append(list, entry)
	}
	if len(lines) > 0 && len(list) == 0 {
		return nil, xerr.Errorf(xerr.ERROR, "system logs are not JSON encoded")
	}
	sort.SliceStable(list, func(i, j int) bool {
		left, _ := list[i]["timestamp"].(string)
		right, _ := list[j]["timestamp"].(string)
		return left > right
	})
	if len(list) > systemLogTail {
		list = list[:systemLogTail]
	}
	return &dto.LogResponse{List: list}, nil
}
