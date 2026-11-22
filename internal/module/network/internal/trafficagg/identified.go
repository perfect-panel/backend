package trafficagg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
	"github.com/perfect-panel/backend/internal/module/platform/entity/inbox"
)

type ReportStore interface {
	AdmitTrafficReport(context.Context, *inbox.TrafficReport) error
	PendingTrafficReports(context.Context, int64, time.Time, int) ([]inbox.TrafficReport, error)
}

var ErrInvalidReportID = errors.New("invalid traffic report ID")

var reportIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// AdmitIdentified persists the actual charged deltas before acknowledging.
// The repository's unique key is independent of encoding, panel replica,
// wall-clock minute and Redis retention.
func (a *Aggregator) AdmitIdentified(ctx context.Context, server *node.Server, protocol, id string, logs []UserTraffic, now time.Time) error {
	if !reportIDPattern.MatchString(id) {
		return ErrInvalidReportID
	}
	if a == nil || a.deps.Reports == nil || server == nil || server.Id <= 0 {
		return errors.New("traffic report storage unavailable")
	}
	key := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s", server.Id, strings.ToLower(strings.TrimSpace(protocol)), id)))
	raw, err := json.Marshal(logs)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	deltas, err := a.admittedDeltas(ctx, server, protocol, logs, now)
	if err != nil {
		return err
	}
	// Freeze account ownership along with the rate. A deferred log retry
	// must still attribute already-billed traffic after a subscription is removed.
	deltas, err = a.attribute(ctx, deltas)
	if err != nil {
		return err
	}
	receipt := &inbox.TrafficReport{Key: hex.EncodeToString(key[:]), Digest: hex.EncodeToString(digest[:]), ReceivedAt: now, Completed: len(deltas) == 0}
	if !receipt.Completed {
		receipt.Payload, err = json.Marshal(deltas)
		if err != nil {
			return err
		}
	}
	// Heartbeat failure cannot accidentally charge a retried batch twice.
	if err := a.RecordServerReport(ctx, server.Id, now); err != nil {
		return err
	}
	return a.deps.Reports.AdmitTrafficReport(ctx, receipt)
}

func (a *Aggregator) flushIdentified(ctx context.Context, through time.Time) error {
	if a.deps.Reports == nil {
		return nil
	}
	var firstFailure error
	failed := 0
	var after int64
	for {
		const page = 64
		rows, err := a.deps.Reports.PendingTrafficReports(ctx, after, through, page)
		if err != nil {
			return errors.Join(firstFailure, err)
		}
		for _, r := range rows {
			after = r.ID
			var deltas []trafficDelta
			err := json.Unmarshal(r.Payload, &deltas)
			if err == nil {
				err = a.persistDeltas(ctx, inbox.NodeTrafficReportPrefix+r.Key, bucketMinute(r.ReceivedAt), deltas)
			}
			if err != nil {
				failed++
				if firstFailure == nil {
					firstFailure = fmt.Errorf("traffic report %d: %w", r.ID, err)
				}
			}
		}
		if len(rows) < page {
			if failed != 0 {
				return fmt.Errorf("%d pending traffic reports failed: %w", failed, firstFailure)
			}
			return nil
		}
	}
}
